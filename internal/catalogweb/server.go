// Package catalogweb provides the private, embedded catalog editor.
package catalogweb

import (
	"bot/internal/gacha"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed static/*
var files embed.FS

type Server struct {
	Store         *gacha.Store
	password      [32]byte
	signing       [32]byte
	secure        bool
	mu            sync.Mutex
	loginWindow   time.Time
	loginAttempts int
	imports       chan struct{}
	uploads       chan struct{}
	anilist       *gacha.AniList
}

func New(store *gacha.Store, password string, secureCookies bool) (*Server, error) {
	if len(password) < 16 {
		return nil, fmt.Errorf("CATALOG_ADMIN_PASSWORD must contain at least 16 characters")
	}
	s := &Server{Store: store, password: sha256.Sum256([]byte(password)), secure: secureCookies, imports: make(chan struct{}, 1), uploads: make(chan struct{}, 2), anilist: gacha.NewAniList()}
	if _, err := rand.Read(s.signing[:]); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	assets, _ := fs.Sub(files, "static")
	mux.Handle("GET /catalog/", http.StripPrefix("/catalog/", http.FileServer(http.FS(assets))))
	mux.HandleFunc("POST /catalog/api/login", s.login)
	mux.HandleFunc("POST /catalog/api/logout", s.protect(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, s.cookie("", -1))
		respond(w, 200, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("GET /catalog/api/session", s.protect(func(w http.ResponseWriter, r *http.Request) {
		respond(w, 200, map[string]string{"user": "Catalog administrator"})
	}))
	mux.HandleFunc("GET /catalog/api/characters", s.protect(s.list))
	mux.HandleFunc("POST /catalog/api/characters", s.protect(s.save))
	mux.HandleFunc("GET /catalog/api/characters/{id}", s.protect(s.detail))
	mux.HandleFunc("PUT /catalog/api/characters/{id}", s.protect(s.save))
	mux.HandleFunc("PATCH /catalog/api/characters/{id}", s.protect(s.characterAction))
	mux.HandleFunc("POST /catalog/api/characters/{id}/assets", s.protect(s.upload))
	mux.HandleFunc("PATCH /catalog/api/assets/{id}", s.protect(s.assetAction))
	mux.HandleFunc("GET /catalog/api/assets/{id}/file", s.protect(s.assetFile))
	mux.HandleFunc("POST /catalog/api/import", s.protect(s.importCharacter))
	mux.HandleFunc("GET /catalog/api/stats", s.protect(s.stats))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' blob:; script-src 'self'; style-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		w.Header().Set("Cache-Control", "no-store")
		// Custom header forces a preflight for cross-origin writes. No CORS is granted.
		if r.Method != "GET" && r.Method != "HEAD" && (r.Header.Get("X-Catalog-Request") != "1" || r.Header.Get("Sec-Fetch-Site") == "cross-site") {
			failure(w, 403, "This request must come from the catalog editor.")
			return
		}
		mux.ServeHTTP(w, r)
	})
}
func respond(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func failure(w http.ResponseWriter, status int, msg string) {
	respond(w, status, map[string]string{"error": msg})
}
func internal(w http.ResponseWriter, err error) {
	log.Printf("catalog: %v", err)
	failure(w, 500, "The catalog could not complete this request. Please retry.")
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		failure(w, 400, "Invalid request fields.")
		return false
	}
	return true
}
func pathID(w http.ResponseWriter, r *http.Request) int64 {
	id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if e != nil || id < 1 {
		failure(w, 400, "A valid numeric ID is required.")
		return 0
	}
	return id
}
func (s *Server) cookie(v string, age int) *http.Cookie {
	return &http.Cookie{Name: "catalog_session", Value: v, Path: "/catalog/", MaxAge: age, HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteStrictMode}
}
func (s *Server) sign(payload string) string {
	h := hmac.New(sha256.New, s.signing[:])
	h.Write([]byte(payload))
	return hex.EncodeToString(h.Sum(nil))
}
func (s *Server) protect(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, e := r.Cookie("catalog_session")
		if e == nil {
			p := strings.Split(c.Value, ".")
			if len(p) == 2 {
				expiry, err := strconv.ParseInt(p[0], 10, 64)
				if err == nil && time.Now().Unix() < expiry && hmac.Equal([]byte(p[1]), []byte(s.sign(p[0]))) {
					next(w, r)
					return
				}
			}
		}
		failure(w, 401, "Sign in to manage the catalog.")
	}
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	if time.Since(s.loginWindow) > time.Minute {
		s.loginWindow = time.Now()
		s.loginAttempts = 0
	}
	s.loginAttempts++
	blocked := s.loginAttempts > 15
	s.mu.Unlock()
	if blocked {
		w.Header().Set("Retry-After", "60")
		failure(w, 429, "Too many sign-in attempts. Try again in a minute.")
		return
	}
	var in struct {
		Password string `json:"password"`
	}
	if !decode(w, r, &in) {
		return
	}
	sum := sha256.Sum256([]byte(in.Password))
	if subtle.ConstantTimeCompare(sum[:], s.password[:]) != 1 {
		failure(w, 401, "Incorrect password.")
		return
	}
	expiry := strconv.FormatInt(time.Now().Add(12*time.Hour).Unix(), 10)
	http.SetCookie(w, s.cookie(expiry+"."+s.sign(expiry), 43200))
	respond(w, 200, map[string]bool{"ok": true})
}
func (s *Server) importCharacter(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID int64 `json:"id"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.ID < 1 {
		failure(w, 400, "Enter an AniList character ID.")
		return
	}
	select {
	case s.imports <- struct{}{}:
		defer func() { <-s.imports }()
	default:
		failure(w, 409, "Another import is running. Please wait.")
		return
	}
	id, e := s.Store.ImportOne(r.Context(), s.anilist, in.ID, true)
	if e != nil {
		var upstream *gacha.AniListHTTPError
		if errors.As(e, &upstream) {
			failure(w, 502, fmt.Sprintf("AniList returned HTTP %d. Try again later.", upstream.Status))
		} else {
			log.Printf("catalog import: %v", e)
			failure(w, 502, "Import did not finish. Metadata may have been saved; search the catalog before retrying.")
		}
		return
	}
	respond(w, 200, map[string]int64{"id": id})
}

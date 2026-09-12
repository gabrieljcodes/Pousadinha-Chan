package catalogweb

import (
	"bot/internal/gacha"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
)

const testPassword = "catalog-test-password-1234"

func TestAuthentication(t *testing.T) {
	s, e := New(nil, testPassword, true)
	if e != nil {
		t.Fatal(e)
	}
	h := s.Handler()
	request := func(method, path, body string, header bool, cookie *http.Cookie) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if header {
			r.Header.Set("X-Catalog-Request", "1")
		}
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := request("GET", "/catalog/api/characters", "", true, nil); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if w := request("POST", "/catalog/api/login", `{"password":"`+testPassword+`"}`, false, nil); w.Code != 403 {
		t.Fatal(w.Code)
	}
	if w := request("POST", "/catalog/api/login", `{"password":"wrong"}`, true, nil); w.Code != 401 {
		t.Fatal(w.Code)
	}
	w := request("POST", "/catalog/api/login", `{"password":"`+testPassword+`"}`, true, nil)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	cookie := w.Result().Cookies()[0]
	if !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatal("unsafe session cookie")
	}
	if w = request("GET", "/catalog/api/session", "", true, cookie); w.Code != 200 {
		t.Fatal(w.Code)
	}
	cookie.Value += "tampered"
	if w = request("GET", "/catalog/api/session", "", true, cookie); w.Code != 401 {
		t.Fatal("tampered session accepted")
	}
	if w = request("GET", "/catalog/", "", false, nil); w.Code != 200 || !strings.Contains(w.Body.String(), "Character catalog") {
		t.Fatal("embedded UI unavailable")
	}
	for i := 0; i < 16; i++ {
		w = request("POST", "/catalog/api/login", `{"password":"wrong"}`, true, nil)
	}
	if w.Code != 429 {
		t.Fatal("login limit missing")
	}
}

func testCatalog(t *testing.T) (*Server, *sql.DB) {
	t.Helper()
	dsn := os.Getenv("GACHA_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set GACHA_TEST_DATABASE_URL to a disposable PostgreSQL database")
	}
	u, e := url.Parse(dsn)
	if e != nil || !strings.Contains(u.Path, "gacha_test") {
		t.Fatal("disposable gacha_test database required")
	}
	db, e := sql.Open("pgx", dsn)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	schema := "catalog_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, e = db.Exec("CREATE SCHEMA " + schema); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Exec("DROP SCHEMA " + schema + " CASCADE") })
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	scoped, e := sql.Open("pgx", u.String())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { scoped.Close() })
	if _, e = scoped.Exec(`CREATE TABLE users(id TEXT PRIMARY KEY,balance BIGINT DEFAULT 0)`); e != nil {
		t.Fatal(e)
	}
	store := &gacha.Store{DB: scoped, Config: gacha.Config{MediaDir: t.TempDir()}}
	if e = store.Migrate(context.Background()); e != nil {
		t.Fatal(e)
	}
	s, e := New(store, testPassword, false)
	if e != nil {
		t.Fatal(e)
	}
	return s, scoped
}
func TestCatalogLifecycle(t *testing.T) {
	s, db := testCatalog(t)
	h := s.Handler()
	var cookie *http.Cookie
	call := func(method, path string, body any, status int) map[string]any {
		t.Helper()
		b, _ := json.Marshal(body)
		r := httptest.NewRequest(method, "/catalog/api/"+path, bytes.NewReader(b))
		r.Header.Set("X-Catalog-Request", "1")
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		if c := w.Result().Cookies(); len(c) > 0 {
			cookie = c[0]
		}
		var result map[string]any
		json.Unmarshal(w.Body.Bytes(), &result)
		return result
	}
	call("POST", "login", map[string]string{"password": testPassword}, 200)
	input := map[string]any{"name": "Test <character>", "native_name": "テスト", "aliases": []string{"Alias"}, "gender": "Female", "description": "Description", "favourites": 10000, "new_work": map[string]any{"title": "Test Game", "kind": "game", "genres": []string{"Adventure"}, "studios": []string{"Studio"}}}
	c := call("POST", "characters", input, 200)
	id := int64(c["id"].(float64))
	path := "characters/" + jsonNumber(id)
	detail := call("GET", path, nil, 200)
	if detail["base_value"].(float64) != 150 {
		t.Fatal("base pricing mismatch")
	}
	call("PATCH", path, map[string]any{"action": "enable", "value": true}, 409)
	input["updated_at"] = detail["updated_at"]
	input["new_work"] = nil
	input["name"] = "Edited"
	call("PUT", path, input, 200)
	call("PUT", path, input, 409)
	call("PATCH", path, map[string]any{"action": "favorite", "value": true}, 200)
	list := call("GET", "characters?view=favorites&kind=game&q=Alias", nil, 200)
	if list["total"].(float64) != 1 {
		t.Fatal(list)
	}
	if _, e := db.Exec(`INSERT INTO users(id,balance) VALUES('owner',777)`); e != nil {
		t.Fatal(e)
	}
	if _, e := db.Exec(`INSERT INTO gacha_collection(guild_id,character_id,user_id,keys) VALUES('guild',$1,'owner',12)`, id); e != nil {
		t.Fatal(e)
	}
	var aid int64
	if e := db.QueryRow(`INSERT INTO gacha_assets(character_id,provider,external_id,source_url,sha256,path,media_type,status) VALUES($1,'manual','1','','hash','portrait.png','image/png','approved') RETURNING id`, id).Scan(&aid); e != nil {
		t.Fatal(e)
	}
	ap := "assets/" + jsonNumber(aid)
	call("PATCH", ap, map[string]any{"action": "primary"}, 200)
	call("PATCH", path, map[string]any{"action": "enable", "value": true}, 200)
	call("PATCH", path, map[string]any{"action": "archive", "value": true}, 200)
	call("PATCH", path, map[string]any{"action": "enable", "value": true}, 409)
	var keys, balance int
	if e := db.QueryRow(`SELECT keys,balance FROM gacha_collection JOIN users ON users.id=user_id WHERE character_id=$1`, id).Scan(&keys, &balance); e != nil || keys != 12 || balance != 777 {
		t.Fatal("archive changed ownership/economy", e)
	}
	if call("GET", "characters", nil, 200)["total"].(float64) != 0 {
		t.Fatal("archive visible in main catalog")
	}
	call("PATCH", path, map[string]any{"action": "archive", "value": false}, 200)
	call("PATCH", ap, map[string]any{"action": "archive", "value": true}, 200)
	call("PATCH", ap, map[string]any{"action": "review", "status": "approved"}, 409)
	call("PATCH", ap, map[string]any{"action": "archive", "value": false}, 200)
	call("PATCH", ap, map[string]any{"action": "review", "status": "approved"}, 200)
	call("GET", "stats", nil, 200)
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Fatal("ffmpeg is required for upload integration tests")
	}
	upload := func(replace bool, status int) {
		t.Helper()
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		part, _ := form.CreateFormFile("file", "portrait.png")
		img := image.NewRGBA(image.Rect(0, 0, 40, 60))
		img.Set(10, 10, color.RGBA{R: 255, A: 255})
		png.Encode(part, img)
		form.WriteField("approve", "true")
		if replace {
			form.WriteField("replace_id", jsonNumber(aid))
		}
		form.Close()
		r := httptest.NewRequest("POST", "/catalog/api/"+path+"/assets", &body)
		r.Header.Set("Content-Type", form.FormDataContentType())
		r.Header.Set("X-Catalog-Request", "1")
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("upload: %d %s", w.Code, w.Body.String())
		}
	}
	upload(true, 201)
	upload(false, 409)
	var archived bool
	if e := db.QueryRow(`SELECT archived_at IS NOT NULL FROM gacha_assets WHERE id=$1`, aid).Scan(&archived); e != nil || !archived {
		t.Fatal("replacement did not archive original", e)
	}
	// Authenticated previews must not escape the media root, including via symlinks.
	outside := filepath.Join(t.TempDir(), "private.txt")
	os.WriteFile(outside, []byte("secret"), 0600)
	os.Symlink(outside, filepath.Join(s.Store.Config.MediaDir, "escape.png"))
	db.Exec(`UPDATE gacha_assets SET path='escape.png' WHERE id=$1`, aid)
	call("GET", ap+"/file", nil, 404)
}
func jsonNumber(n int64) string { b, _ := json.Marshal(n); return string(b) }

func TestStrictCSRF(t *testing.T) {
	s, _ := New(nil, testPassword, false)
	r := httptest.NewRequest("POST", "/catalog/api/login", strings.NewReader(`{"password":"`+testPassword+`"}`))
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	r.Header.Set("X-Catalog-Request", "1")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
	if v := w.Header().Get("Access-Control-Allow-Origin"); v != "" {
		t.Fatal("admin leaked CORS", v)
	}
	io.Copy(io.Discard, w.Result().Body)
}

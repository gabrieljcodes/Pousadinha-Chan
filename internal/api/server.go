package api

import (
	"bot/internal/catalogweb"
	"bot/internal/database"
	"bot/internal/gacha"
	"bot/internal/webhook"
	"bot/pkg/config"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

type ErrorResponse struct {
	Error string `json:"error"`
}

type BalanceResponse struct {
	GuildID string `json:"guild_id,omitempty"`
	UserID  string `json:"user_id"`
	Balance int    `json:"balance"`
}

type TransferRequest struct {
	GuildID  string `json:"guild_id"`
	ToUserID string `json:"to_user_id"`
	Amount   int    `json:"amount"`
}

func getGuildID(r *http.Request) string {
	guildID := r.URL.Query().Get("guild_id")
	if guildID == "" {
		guildID = r.Header.Get("X-Guild-ID")
	}
	return guildID
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func CORSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-API-Key, X-Guild-ID")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func AuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("X-API-Key")
		if key == "" {
			writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "Missing API Key"})
			return
		}

		userID, err := database.GetUserByAPIKey(key)
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "Invalid API Key"})
			return
		}

		// Add UserID to header for next handler (simple context passing)
		r.Header.Set("X-User-ID", userID)
		next(w, r)
	}
}

func HandleMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "Method not allowed"})
		return
	}

	userID := r.Header.Get("X-User-ID")
	guildID := getGuildID(r)
	if guildID == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "Missing guild_id parameter or X-Guild-ID header"})
		return
	}

	balance := database.GetBalance(guildID, userID)

	writeJSON(w, http.StatusOK, BalanceResponse{
		GuildID: guildID,
		UserID:  userID,
		Balance: balance,
	})
}

func HandleTransfer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "Method not allowed"})
		return
	}

	userID := r.Header.Get("X-User-ID")

	var req TransferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "Invalid Request Body"})
		return
	}

	guildID := req.GuildID
	if guildID == "" {
		guildID = getGuildID(r)
	}
	if guildID == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "Missing guild_id parameter or body field"})
		return
	}

	if req.Amount <= 0 {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "Amount must be positive"})
		return
	}

	if req.ToUserID == userID {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "Cannot transfer to yourself"})
		return
	}

	err := database.TransferCoins(guildID, userID, req.ToUserID, req.Amount)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "Insufficient funds or transaction failed"})
		return
	}

	webhook.SendTransferNotification(userID, req.ToUserID, req.Amount)

	writeJSON(w, http.StatusOK, map[string]string{"status": "success"})
}

// CatalogStore is initialized at startup, independently of Discord gacha commands.
var CatalogStore *gacha.Store

func Start() {
	mux := http.NewServeMux()
	if gacha.Default != nil {
		mux.Handle("/", gacha.Default)
	}

	// The catalog is mounted outside public CORS and uses its own admin session.
	var catalog http.Handler
	if password := os.Getenv("CATALOG_ADMIN_PASSWORD"); password != "" {
		if CatalogStore == nil {
			log.Fatal("Catalog requires the catalog store to be initialized")
		}
		editor, err := catalogweb.New(CatalogStore, password, os.Getenv("CATALOG_SECURE_COOKIES") == "true")
		if err != nil {
			log.Fatal(err)
		}
		catalog = editor.Handler()
	}

	// User endpoints
	mux.HandleFunc("/api/v1/me", AuthMiddleware(HandleMe))
	mux.HandleFunc("/api/v1/transfer", AuthMiddleware(HandleTransfer))

	// Stock market endpoints
	mux.HandleFunc("/api/v1/stocks", HandleStocksList)
	mux.HandleFunc("/api/v1/stocks/portfolio", AuthMiddleware(HandlePortfolio))
	mux.HandleFunc("/api/v1/stocks/buy", AuthMiddleware(HandleBuyStock))
	mux.HandleFunc("/api/v1/stocks/sell", AuthMiddleware(HandleSellStock))

	// Cryptocurrency endpoints
	mux.HandleFunc("/api/v1/crypto", HandleCryptoList)
	mux.HandleFunc("/api/v1/crypto/portfolio", AuthMiddleware(HandleCryptoPortfolio))
	mux.HandleFunc("/api/v1/crypto/buy", AuthMiddleware(HandleBuyCrypto))
	mux.HandleFunc("/api/v1/crypto/sell", HandleSellCrypto)

	port := config.Bot.ApiPort
	if port == "" {
		port = ":8080"
	} else if !strings.Contains(port, ":") {
		port = ":" + port
	}

	publicHandler := CORSMiddleware(mux)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/catalog" {
			http.Redirect(w, r, "/catalog/", http.StatusTemporaryRedirect)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/catalog/") {
			if catalog == nil {
				http.NotFound(w, r)
				return
			}
			catalog.ServeHTTP(w, r)
			return
		}
		publicHandler.ServeHTTP(w, r)
	})
	server := &http.Server{
		Addr:              port,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      180 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	log.Printf("Starting API Server on %s", port)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal("API Server failed:", err)
	}
}

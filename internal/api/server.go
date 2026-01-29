package api

import (
	"encoding/json"
	"estudocoin/internal/database"
	"estudocoin/internal/webhook"
	"estudocoin/pkg/config"
	"log"
	"net/http"
)

type ErrorResponse struct {
	Error string `json:"error"`
}

type BalanceResponse struct {
	UserID  string `json:"user_id"`
	Balance int    `json:"balance"`
}

type TransferRequest struct {
	ToUserID string `json:"to_user_id"`
	Amount   int    `json:"amount"`
}

func AuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("X-API-Key")
		if key == "" {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(ErrorResponse{Error: "Missing API Key"})
			return
		}

		userID, err := database.GetUserByAPIKey(key)
		if err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(ErrorResponse{Error: "Invalid API Key"})
			return
		}

		// Add UserID to header for next handler (simple context passing)
		r.Header.Set("X-User-ID", userID)
		next(w, r)
	}
}

func HandleMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	userID := r.Header.Get("X-User-ID")
	balance := database.GetBalance(userID)

	json.NewEncoder(w).Encode(BalanceResponse{
		UserID:  userID,
		Balance: balance,
	})
}

func HandleTransfer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	userID := r.Header.Get("X-User-ID")
	
	var req TransferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Invalid Request Body"})
		return
	}

	if req.Amount <= 0 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Amount must be positive"})
		return
	}

	if req.ToUserID == userID {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Cannot transfer to yourself"})
		return
	}

	err := database.TransferCoins(userID, req.ToUserID, req.Amount)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Insufficient funds or transaction failed"})
		return
	}

	webhook.SendTransferNotification(userID, req.ToUserID, req.Amount)

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func Start() {
	mux := http.NewServeMux()
	
	// User endpoints
	mux.HandleFunc("/api/v1/me", AuthMiddleware(HandleMe))
	mux.HandleFunc("/api/v1/transfer", AuthMiddleware(HandleTransfer))
	
	// Stock market endpoints
	mux.HandleFunc("/api/v1/stocks", HandleStocksList)
	mux.HandleFunc("/api/v1/stocks/portfolio", AuthMiddleware(HandlePortfolio))
	mux.HandleFunc("/api/v1/stocks/buy", AuthMiddleware(HandleBuyStock))
	mux.HandleFunc("/api/v1/stocks/sell", AuthMiddleware(HandleSellStock))

	port := config.Bot.ApiPort
	if port == "" {
		port = ":8080"
	}

	log.Printf("Starting API Server on %s", port)
	if err := http.ListenAndServe(port, mux); err != nil {
		log.Fatal("API Server failed:", err)
	}
}

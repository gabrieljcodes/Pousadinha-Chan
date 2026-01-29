package api

import (
	"encoding/json"
	"estudocoin/internal/crypto"
	"estudocoin/internal/database"
	"estudocoin/pkg/config"
	"estudocoin/pkg/utils"
	"fmt"
	"net/http"
	"strings"
)

// CryptoInfo represents a cryptocurrency with its current price
type CryptoInfo struct {
	Symbol string  `json:"symbol"`
	Name   string  `json:"name"`
	Type   string  `json:"type"`
	Price  float64 `json:"price"`
}

// CryptoPortfolioItem represents a single crypto investment
type CryptoPortfolioItem struct {
	Symbol       string  `json:"symbol"`
	Name         string  `json:"name"`
	Type         string  `json:"type"`
	Coins        float64 `json:"coins"`
	CurrentPrice float64 `json:"current_price"`
	Value        int     `json:"value"`
}

// CryptoPortfolioResponse represents the user's crypto portfolio
type CryptoPortfolioResponse struct {
	Items      []CryptoPortfolioItem `json:"items"`
	TotalValue int                   `json:"total_value"`
}

// BuyCryptoRequest represents a crypto buy request
type BuyCryptoRequest struct {
	Symbol string `json:"symbol"`
	Amount int    `json:"amount"`
}

// BuyCryptoResponse represents a crypto buy response
type BuyCryptoResponse struct {
	Symbol     string  `json:"symbol"`
	Coins      float64 `json:"coins"`
	AmountPaid int     `json:"amount_paid"`
	Price      float64 `json:"price"`
	Balance    int     `json:"balance"`
}

// SellCryptoRequest represents a crypto sell request
type SellCryptoRequest struct {
	Symbol string  `json:"symbol"`
	Coins  float64 `json:"coins"`
}

// SellCryptoResponse represents a crypto sell response
type SellCryptoResponse struct {
	Symbol         string  `json:"symbol"`
	Coins          float64 `json:"coins"`
	AmountReceived int     `json:"amount_received"`
	Price          float64 `json:"price"`
	Balance        int     `json:"balance"`
}

// HandleCryptoList returns the list of available cryptocurrencies and their prices
func HandleCryptoList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	prices, err := crypto.GetCryptoPrices()
	if err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Could not fetch crypto prices"})
		return
	}

	var cryptos []CryptoInfo
	for _, c := range crypto.AvailableCryptos {
		price := prices[c.ID]
		if price > 0 {
			cryptos = append(cryptos, CryptoInfo{
				Symbol: c.Symbol,
				Name:   c.Name,
				Type:   c.Type,
				Price:  price,
			})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(cryptos)
}

// HandleCryptoPortfolio returns the user's crypto portfolio
func HandleCryptoPortfolio(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	userID := r.Header.Get("X-User-ID")

	investments, err := database.GetAllCryptoInvestmentsByUser(userID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Database error"})
		return
	}

	prices, err := crypto.GetCryptoPrices()
	if err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Could not fetch crypto prices"})
		return
	}

	var items []CryptoPortfolioItem
	totalValue := 0.0

	for _, inv := range investments {
		c := crypto.GetCryptoBySymbol(inv.Symbol)
		if c == nil {
			continue
		}

		price := prices[c.ID]
		if price <= 0 {
			continue
		}

		value := inv.Coins * price
		totalValue += value

		items = append(items, CryptoPortfolioItem{
			Symbol:       inv.Symbol,
			Name:         c.Name,
			Type:         c.Type,
			Coins:        inv.Coins,
			CurrentPrice: price,
			Value:        int(value),
		})
	}

	response := CryptoPortfolioResponse{
		Items:      items,
		TotalValue: int(totalValue),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// HandleBuyCrypto handles cryptocurrency purchase requests
func HandleBuyCrypto(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	userID := r.Header.Get("X-User-ID")

	var req BuyCryptoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Invalid request body"})
		return
	}

	// Validate symbol
	symbol := strings.ToUpper(req.Symbol)
	c := crypto.GetCryptoBySymbol(symbol)
	if c == nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Invalid cryptocurrency symbol"})
		return
	}

	// Validate amount
	if req.Amount <= 0 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Amount must be positive"})
		return
	}

	// Check balance
	balance := database.GetBalance(userID)
	if balance < req.Amount {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Insufficient funds"})
		return
	}

	// Get price
	price, err := crypto.GetSingleCryptoPrice(c.ID)
	if err != nil || price <= 0 {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Could not fetch crypto price"})
		return
	}

	coins := float64(req.Amount) / price

	// Transaction
	tx, err := database.DB.Begin()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Transaction failed"})
		return
	}
	defer tx.Rollback()

	// Remove coins
	if _, err := tx.Exec(PrepareQuery("UPDATE users SET balance = balance - ? WHERE id = ?"), req.Amount, userID); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Transaction failed"})
		return
	}

	// Add crypto shares
	var query string
	if config.DBType == "postgres" {
		query = `INSERT INTO crypto_investments (user_id, symbol, coins) VALUES ($1, $2, $3) 
				  ON CONFLICT(user_id, symbol) DO UPDATE SET coins = crypto_investments.coins + $3`
		_, err = tx.Exec(query, userID, symbol, coins)
	} else {
		query = "INSERT INTO crypto_investments (user_id, symbol, coins) VALUES (?, ?, ?) ON CONFLICT(user_id, symbol) DO UPDATE SET coins = coins + ?"
		_, err = tx.Exec(query, userID, symbol, coins, coins)
	}

	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Failed to add crypto"})
		return
	}

	if err := tx.Commit(); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Transaction commit failed"})
		return
	}

	// Send webhook notification
	go func() {
		message := fmt.Sprintf("🪙 **Crypto Purchase**\nYou bought **%.8f %s** for **%d %s** (at $%.6f/coin).",
			coins, symbol, req.Amount, config.Bot.CurrencyName, price)
		utils.SendWebhookNotification(userID, message)
	}()

	newBalance := database.GetBalance(userID)

	response := BuyCryptoResponse{
		Symbol:     symbol,
		Coins:      coins,
		AmountPaid: req.Amount,
		Price:      price,
		Balance:    newBalance,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// HandleSellCrypto handles cryptocurrency sale requests
func HandleSellCrypto(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	userID := r.Header.Get("X-User-ID")

	var req SellCryptoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Invalid request body"})
		return
	}

	// Validate symbol
	symbol := strings.ToUpper(req.Symbol)
	c := crypto.GetCryptoBySymbol(symbol)
	if c == nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Invalid cryptocurrency symbol"})
		return
	}

	// Validate coins
	if req.Coins <= 0 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Coins must be positive"})
		return
	}

	// Check owned coins
	ownedCoins, err := database.GetCryptoInvestment(userID, symbol)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Database error"})
		return
	}
	if ownedCoins <= 0 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "You don't own any of this cryptocurrency"})
		return
	}
	if req.Coins > ownedCoins {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: fmt.Sprintf("You only own %.8f %s", ownedCoins, symbol)})
		return
	}

	// Get price
	price, err := crypto.GetSingleCryptoPrice(c.ID)
	if err != nil || price <= 0 {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Could not fetch crypto price"})
		return
	}

	payout := int(req.Coins * price)

	// Transaction
	tx, err := database.DB.Begin()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Transaction failed"})
		return
	}
	defer tx.Rollback()

	// Remove crypto shares
	newAmount := ownedCoins - req.Coins
	var query string
	if newAmount <= 0.00000001 {
		query = PrepareQuery("DELETE FROM crypto_investments WHERE user_id = ? AND symbol = ?")
		_, err = tx.Exec(query, userID, symbol)
	} else {
		query = PrepareQuery("UPDATE crypto_investments SET coins = ? WHERE user_id = ? AND symbol = ?")
		_, err = tx.Exec(query, newAmount, userID, symbol)
	}
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Failed to remove crypto"})
		return
	}

	// Add coins
	query = PrepareQuery("UPDATE users SET balance = balance + ? WHERE id = ?")
	if _, err := tx.Exec(query, payout, userID); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Failed to add coins"})
		return
	}

	if err := tx.Commit(); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Transaction commit failed"})
		return
	}

	// Send webhook notification
	go func() {
		message := fmt.Sprintf("💰 **Crypto Sale**\nYou sold **%.8f %s** for **%d %s** (at $%.6f/coin).",
			req.Coins, symbol, payout, config.Bot.CurrencyName, price)
		utils.SendWebhookNotification(userID, message)
	}()

	newBalance := database.GetBalance(userID)

	response := SellCryptoResponse{
		Symbol:         symbol,
		Coins:          req.Coins,
		AmountReceived: payout,
		Price:          price,
		Balance:        newBalance,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

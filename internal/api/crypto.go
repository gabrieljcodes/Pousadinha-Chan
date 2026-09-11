package api

import (
	"encoding/json"
	"bot/internal/crypto"
	"bot/internal/database"
	"bot/internal/webhook"
	"fmt"
	"math"
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
	GuildID string `json:"guild_id"`
	Symbol  string `json:"symbol"`
	Amount  int    `json:"amount"`
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
	GuildID string  `json:"guild_id"`
	Symbol  string  `json:"symbol"`
	Coins   float64 `json:"coins"`
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
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "Method not allowed"})
		return
	}

	prices, err := crypto.GetCryptoPrices()
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "Could not fetch crypto prices"})
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

	writeJSON(w, http.StatusOK, cryptos)
}

// HandleCryptoPortfolio returns the user's crypto portfolio
func HandleCryptoPortfolio(w http.ResponseWriter, r *http.Request) {
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

	investments, err := database.GetAllCryptoInvestmentsByUser(guildID, userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "Database error"})
		return
	}

	prices, err := crypto.GetCryptoPrices()
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "Could not fetch crypto prices"})
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

	writeJSON(w, http.StatusOK, response)
}

// HandleBuyCrypto handles cryptocurrency purchase requests
func HandleBuyCrypto(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "Method not allowed"})
		return
	}

	userID := r.Header.Get("X-User-ID")

	var req BuyCryptoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "Invalid request body"})
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

	// Validate symbol
	symbol := strings.ToUpper(req.Symbol)
	c := crypto.GetCryptoBySymbol(symbol)
	if c == nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "Invalid cryptocurrency symbol"})
		return
	}

	// Validate amount
	if req.Amount <= 0 {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "Amount must be positive"})
		return
	}

	// Check balance
	balance := database.GetBalance(guildID, userID)
	if balance < req.Amount {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "Insufficient funds"})
		return
	}

	// Get price
	price, err := crypto.GetSingleCryptoPrice(c.ID)
	if err != nil || price <= 0 {
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "Could not fetch crypto price"})
		return
	}

	coins := float64(req.Amount) / price

	// Transaction
	tx, err := database.DB.Begin()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "Transaction failed"})
		return
	}
	defer tx.Rollback()

	// Atomically remove coins ensuring balance >= amount
	res, err := tx.Exec(`UPDATE guild_members SET balance = balance - $1 WHERE guild_id = $2 AND user_id = $3 AND balance >= $1`, req.Amount, guildID, userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "Transaction failed"})
		return
	}
	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "Insufficient funds"})
		return
	}

	// Add crypto shares
	query := `INSERT INTO crypto_investments (guild_id, user_id, symbol, coins, total_invested) VALUES ($1, $2, $3, $4, $5) 
			  ON CONFLICT(guild_id, user_id, symbol) DO UPDATE SET 
			    coins = crypto_investments.coins + $4,
			    total_invested = crypto_investments.total_invested + $5`
	_, err = tx.Exec(query, guildID, userID, symbol, coins, req.Amount)

	if err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "Failed to add crypto"})
		return
	}

	if err := tx.Commit(); err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "Transaction commit failed"})
		return
	}

	// Send webhook notification
	webhook.SendCryptoNotification(userID, true, symbol, coins, req.Amount, price)

	newBalance := database.GetBalance(guildID, userID)

	response := BuyCryptoResponse{
		Symbol:     symbol,
		Coins:      coins,
		AmountPaid: req.Amount,
		Price:      price,
		Balance:    newBalance,
	}

	writeJSON(w, http.StatusOK, response)
}

// HandleSellCrypto handles cryptocurrency sale requests
func HandleSellCrypto(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "Method not allowed"})
		return
	}

	userID := r.Header.Get("X-User-ID")

	var req SellCryptoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "Invalid request body"})
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

	// Validate symbol
	symbol := strings.ToUpper(req.Symbol)
	c := crypto.GetCryptoBySymbol(symbol)
	if c == nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "Invalid cryptocurrency symbol"})
		return
	}

	// Validate coins
	if req.Coins <= 0 {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "Coins must be positive"})
		return
	}

	// Check owned coins
	ownedCoins, err := database.GetCryptoInvestment(guildID, userID, symbol)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "Database error"})
		return
	}
	if ownedCoins <= 0 {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "You don't own any of this cryptocurrency"})
		return
	}
	if req.Coins > ownedCoins {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: fmt.Sprintf("You only own %.8f %s", ownedCoins, symbol)})
		return
	}

	// Get price
	price, err := crypto.GetSingleCryptoPrice(c.ID)
	if err != nil || price <= 0 {
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "Could not fetch crypto price"})
		return
	}

	payout := int(math.Round(req.Coins * price))

	// Transaction
	tx, err := database.DB.Begin()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "Transaction failed"})
		return
	}
	defer tx.Rollback()

	// Remove crypto shares and proportionally adjust total_invested
	res, err := tx.Exec(`UPDATE crypto_investments 
		SET total_invested = CASE WHEN coins <= $1 THEN 0 ELSE total_invested * (1 - ($1 / coins)) END,
		    coins = coins - $1 
		WHERE guild_id = $2 AND user_id = $3 AND symbol = $4 AND coins >= $1`, req.Coins, guildID, userID, symbol)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "Failed to remove crypto"})
		return
	}
	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "Not enough coins"})
		return
	}

	// Clean up dust
	_, _ = tx.Exec(`DELETE FROM crypto_investments WHERE guild_id = $1 AND user_id = $2 AND symbol = $3 AND coins <= 0.00000001`, guildID, userID, symbol)

	// Add coins to guild_members
	if _, err := tx.Exec(`INSERT INTO guild_members (guild_id, user_id, balance) VALUES ($1, $2, $3)
		ON CONFLICT (guild_id, user_id) DO UPDATE SET balance = guild_members.balance + $3`, guildID, userID, payout); err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "Failed to add coins"})
		return
	}

	if err := tx.Commit(); err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "Transaction commit failed"})
		return
	}

	// Send webhook notification
	webhook.SendCryptoNotification(userID, false, symbol, req.Coins, payout, price)

	newBalance := database.GetBalance(guildID, userID)

	response := SellCryptoResponse{
		Symbol:         symbol,
		Coins:          req.Coins,
		AmountReceived: payout,
		Price:          price,
		Balance:        newBalance,
	}

	writeJSON(w, http.StatusOK, response)
}

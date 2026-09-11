package api

import (
	"encoding/json"
	"bot/internal/database"
	"bot/internal/stockmarket"
	"bot/internal/webhook"
	"fmt"
	"math"
	"net/http"
	"strings"
)

// StockInfo represents a stock with its current price
type StockInfo struct {
	Ticker           string  `json:"ticker"`
	Name             string  `json:"name"`
	Price            float64 `json:"price"`
	ChangeAmount     float64 `json:"change_amount"`
	ChangePercentage float64 `json:"change_percentage"`
}

// PortfolioItem represents a single investment in the portfolio
type PortfolioItem struct {
	Ticker       string  `json:"ticker"`
	Name         string  `json:"name"`
	Shares       float64 `json:"shares"`
	CurrentPrice float64 `json:"current_price"`
	Value        int     `json:"value"`
}

// PortfolioResponse represents the user's portfolio
type PortfolioResponse struct {
	Items      []PortfolioItem `json:"items"`
	TotalValue int             `json:"total_value"`
}

// BuyStockRequest represents a buy request
type BuyStockRequest struct {
	GuildID string `json:"guild_id"`
	Ticker  string `json:"ticker"`
	Amount  int    `json:"amount"`
}

// BuyStockResponse represents a buy response
type BuyStockResponse struct {
	Ticker        string  `json:"ticker"`
	Shares        float64 `json:"shares"`
	AmountPaid    int     `json:"amount_paid"`
	PricePerShare float64 `json:"price_per_share"`
	Balance       int     `json:"balance"`
}

// SellStockRequest represents a sell request
type SellStockRequest struct {
	GuildID string  `json:"guild_id"`
	Ticker  string  `json:"ticker"`
	Shares  float64 `json:"shares"`
}

// SellStockResponse represents a sell response
type SellStockResponse struct {
	Ticker         string  `json:"ticker"`
	Shares         float64 `json:"shares"`
	AmountReceived int     `json:"amount_received"`
	PricePerShare  float64 `json:"price_per_share"`
	Balance        int     `json:"balance"`
}

// HandleStocksList returns the list of available stocks and their prices
func HandleStocksList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "Method not allowed"})
		return
	}

	var stocks []StockInfo

	for _, company := range stockmarket.Companies {
		price, _ := database.GetStockPriceDB(company.Ticker)

		// If no cached price, try to fetch live
		changeAmount := 0.0
		changePercentage := 0.0

		if price <= 0 {
			data, err := stockmarket.GetStockPrice(company.Ticker)
			if err == nil {
				price = data.Price
				changeAmount = data.ChangeAmount
				changePercentage = data.ChangePercentage
				database.SetStockPriceDB(company.Ticker, price)
			}
		} else {
			// Try to get live data for changes
			data, err := stockmarket.GetStockPrice(company.Ticker)
			if err == nil {
				changeAmount = data.ChangeAmount
				changePercentage = data.ChangePercentage
			}
		}

		stocks = append(stocks, StockInfo{
			Ticker:           company.Ticker,
			Name:             company.Name,
			Price:            price,
			ChangeAmount:     changeAmount,
			ChangePercentage: changePercentage,
		})
	}

	writeJSON(w, http.StatusOK, stocks)
}

// HandlePortfolio returns the user's stock portfolio
func HandlePortfolio(w http.ResponseWriter, r *http.Request) {
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

	investments, err := database.GetAllInvestmentsByUser(guildID, userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "Database error"})
		return
	}

	var items []PortfolioItem
	totalValue := 0.0

	for _, inv := range investments {
		if inv.Shares <= 0 {
			continue
		}

		price, _ := database.GetStockPriceDB(inv.Ticker)
		if price <= 0 {
			// Try to fetch live price
			data, err := stockmarket.GetStockPrice(inv.Ticker)
			if err == nil {
				price = data.Price
				database.SetStockPriceDB(inv.Ticker, price)
			}
		}

		// Find company name
		var name string
		for _, company := range stockmarket.Companies {
			if company.Ticker == inv.Ticker {
				name = company.Name
				break
			}
		}

		value := inv.Shares * price
		totalValue += value

		items = append(items, PortfolioItem{
			Ticker:       inv.Ticker,
			Name:         name,
			Shares:       inv.Shares,
			CurrentPrice: price,
			Value:        int(value),
		})
	}

	response := PortfolioResponse{
		Items:      items,
		TotalValue: int(totalValue),
	}

	writeJSON(w, http.StatusOK, response)
}

// HandleBuyStock handles stock purchase requests
func HandleBuyStock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "Method not allowed"})
		return
	}

	userID := r.Header.Get("X-User-ID")

	var req BuyStockRequest
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

	// Validate ticker
	ticker := strings.ToUpper(req.Ticker)
	valid := false
	for _, company := range stockmarket.Companies {
		if company.Ticker == ticker {
			valid = true
			break
		}
	}
	if !valid {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "Invalid ticker"})
		return
	}

	// Validate amount
	if req.Amount <= 0 {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "Amount must be positive"})
		return
	}

	// Initial balance check
	balance := database.GetBalance(guildID, userID)
	if balance < req.Amount {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "Insufficient funds"})
		return
	}

	// Get price
	price, err := database.GetStockPriceDB(ticker)
	if err != nil || price <= 0 {
		data, err := stockmarket.GetStockPrice(ticker)
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "Could not fetch stock price"})
			return
		}
		price = data.Price
		database.SetStockPriceDB(ticker, price)
	}

	shares := float64(req.Amount) / price

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

	// Add shares using upsert syntax
	query := `INSERT INTO stock_investments (guild_id, user_id, ticker, shares, total_invested) VALUES ($1, $2, $3, $4, $5) 
			  ON CONFLICT(guild_id, user_id, ticker) DO UPDATE SET 
			    shares = stock_investments.shares + $4,
			    total_invested = stock_investments.total_invested + $5`

	_, err = tx.Exec(query, guildID, userID, ticker, shares, req.Amount)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "Failed to add shares"})
		return
	}

	if err := tx.Commit(); err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "Transaction commit failed"})
		return
	}

	// Send webhook notification
	webhook.SendStockNotification(userID, true, ticker, shares, req.Amount, price)

	newBalance := database.GetBalance(guildID, userID)

	response := BuyStockResponse{
		Ticker:        ticker,
		Shares:        shares,
		AmountPaid:    req.Amount,
		PricePerShare: price,
		Balance:       newBalance,
	}

	writeJSON(w, http.StatusOK, response)
}

// HandleSellStock handles stock sale requests
func HandleSellStock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "Method not allowed"})
		return
	}

	userID := r.Header.Get("X-User-ID")

	var req SellStockRequest
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

	// Validate ticker
	ticker := strings.ToUpper(req.Ticker)
	valid := false
	for _, company := range stockmarket.Companies {
		if company.Ticker == ticker {
			valid = true
			break
		}
	}
	if !valid {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "Invalid ticker"})
		return
	}

	// Validate shares
	if req.Shares <= 0 {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "Shares must be positive"})
		return
	}

	// Check owned shares
	ownedShares, err := database.GetInvestment(guildID, userID, ticker)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "Database error"})
		return
	}
	if ownedShares <= 0 {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "You don't own any shares of this company"})
		return
	}
	if req.Shares > ownedShares {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: fmt.Sprintf("You only own %.4f shares", ownedShares)})
		return
	}

	// Get price
	price, err := database.GetStockPriceDB(ticker)
	if err != nil || price <= 0 {
		data, err := stockmarket.GetStockPrice(ticker)
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "Could not fetch stock price"})
			return
		}
		price = data.Price
		database.SetStockPriceDB(ticker, price)
	}

	payout := int(math.Round(req.Shares * price))

	// Transaction
	tx, err := database.DB.Begin()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "Transaction failed"})
		return
	}
	defer tx.Rollback()

	// Remove shares and proportionally adjust total_invested
	res, err := tx.Exec(`UPDATE stock_investments 
		SET total_invested = CASE WHEN shares <= $1 THEN 0 ELSE total_invested * (1 - ($1 / shares)) END,
		    shares = shares - $1 
		WHERE guild_id = $2 AND user_id = $3 AND ticker = $4 AND shares >= $1`, req.Shares, guildID, userID, ticker)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "Failed to remove shares"})
		return
	}
	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "Not enough shares"})
		return
	}

	// Clean up dust
	_, _ = tx.Exec(`DELETE FROM stock_investments WHERE guild_id = $1 AND user_id = $2 AND ticker = $3 AND shares <= 0.000001`, guildID, userID, ticker)

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
	webhook.SendStockNotification(userID, false, ticker, req.Shares, payout, price)

	newBalance := database.GetBalance(guildID, userID)

	response := SellStockResponse{
		Ticker:         ticker,
		Shares:         req.Shares,
		AmountReceived: payout,
		PricePerShare:  price,
		Balance:        newBalance,
	}

	writeJSON(w, http.StatusOK, response)
}

// PrepareQuery is a helper that converts placeholders to PostgreSQL format ($1, $2, etc.)
func PrepareQuery(query string) string {
	result := ""
	placeholderIndex := 1
	for i := 0; i < len(query); i++ {
		if query[i] == '?' {
			result += fmt.Sprintf("$%d", placeholderIndex)
			placeholderIndex++
		} else {
			result += string(query[i])
		}
	}
	return result
}

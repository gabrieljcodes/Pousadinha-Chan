package api

import (
	"encoding/json"
	"estudocoin/internal/database"
	"estudocoin/internal/stockmarket"
	"estudocoin/pkg/config"
	"estudocoin/pkg/utils"
	"fmt"
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
	Ticker      string  `json:"ticker"`
	Name        string  `json:"name"`
	Shares      float64 `json:"shares"`
	CurrentPrice float64 `json:"current_price"`
	Value       int     `json:"value"`
}

// PortfolioResponse represents the user's portfolio
type PortfolioResponse struct {
	Items      []PortfolioItem `json:"items"`
	TotalValue int             `json:"total_value"`
}

// BuyStockRequest represents a buy request
type BuyStockRequest struct {
	Ticker string `json:"ticker"`
	Amount int    `json:"amount"`
}

// BuyStockResponse represents a buy response
type BuyStockResponse struct {
	Ticker       string  `json:"ticker"`
	Shares       float64 `json:"shares"`
	AmountPaid   int     `json:"amount_paid"`
	PricePerShare float64 `json:"price_per_share"`
	Balance      int     `json:"balance"`
}

// SellStockRequest represents a sell request
type SellStockRequest struct {
	Ticker string  `json:"ticker"`
	Shares float64 `json:"shares"`
}

// SellStockResponse represents a sell response
type SellStockResponse struct {
	Ticker        string  `json:"ticker"`
	Shares        float64 `json:"shares"`
	AmountReceived int    `json:"amount_received"`
	PricePerShare float64 `json:"price_per_share"`
	Balance       int     `json:"balance"`
}

// HandleStocksList returns the list of available stocks and their prices
func HandleStocksList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stocks)
}

// HandlePortfolio returns the user's stock portfolio
func HandlePortfolio(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	userID := r.Header.Get("X-User-ID")

	investments, err := database.GetAllInvestmentsByUser(userID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Database error"})
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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// HandleBuyStock handles stock purchase requests
func HandleBuyStock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	userID := r.Header.Get("X-User-ID")

	var req BuyStockRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Invalid request body"})
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
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Invalid ticker"})
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
	price, err := database.GetStockPriceDB(ticker)
	if err != nil || price <= 0 {
		data, err := stockmarket.GetStockPrice(ticker)
		if err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(ErrorResponse{Error: "Could not fetch stock price"})
			return
		}
		price = data.Price
		database.SetStockPriceDB(ticker, price)
	}

	shares := float64(req.Amount) / price

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

	// Add shares using the appropriate upsert syntax
	var query string
	if config.DBType == "postgres" {
		query = `INSERT INTO stock_investments (user_id, ticker, shares) VALUES ($1, $2, $3) 
				  ON CONFLICT(user_id, ticker) DO UPDATE SET shares = stock_investments.shares + $3`
	} else {
		query = "INSERT INTO stock_investments (user_id, ticker, shares) VALUES (?, ?, ?) ON CONFLICT(user_id, ticker) DO UPDATE SET shares = shares + ?"
	}

	if config.DBType == "postgres" {
		_, err = tx.Exec(query, userID, ticker, shares)
	} else {
		_, err = tx.Exec(query, userID, ticker, shares, shares)
	}
	
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Failed to add shares"})
		return
	}

	if err := tx.Commit(); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Transaction commit failed"})
		return
	}

	// Send webhook notification
	go func() {
		message := fmt.Sprintf("📈 **Stock Purchase**\nYou bought **%.4f** shares of **%s** for **%d %s** (at $%.2f/share).",
			shares, ticker, req.Amount, config.Bot.CurrencyName, price)
		utils.SendWebhookNotification(userID, message)
	}()

	newBalance := database.GetBalance(userID)

	response := BuyStockResponse{
		Ticker:        ticker,
		Shares:        shares,
		AmountPaid:    req.Amount,
		PricePerShare: price,
		Balance:       newBalance,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// HandleSellStock handles stock sale requests
func HandleSellStock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	userID := r.Header.Get("X-User-ID")

	var req SellStockRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Invalid request body"})
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
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Invalid ticker"})
		return
	}

	// Validate shares
	if req.Shares <= 0 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Shares must be positive"})
		return
	}

	// Check owned shares
	ownedShares, err := database.GetInvestment(userID, ticker)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Database error"})
		return
	}
	if ownedShares <= 0 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "You don't own any shares of this company"})
		return
	}
	if req.Shares > ownedShares {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: fmt.Sprintf("You only own %.4f shares", ownedShares)})
		return
	}

	// Get price
	price, err := database.GetStockPriceDB(ticker)
	if err != nil || price <= 0 {
		data, err := stockmarket.GetStockPrice(ticker)
		if err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(ErrorResponse{Error: "Could not fetch stock price"})
			return
		}
		price = data.Price
		database.SetStockPriceDB(ticker, price)
	}

	payout := int(req.Shares * price)

	// Transaction
	tx, err := database.DB.Begin()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Transaction failed"})
		return
	}
	defer tx.Rollback()

	// Remove shares
	newAmount := ownedShares - req.Shares
	var query string
	if newAmount <= 0.000001 {
		query = PrepareQuery("DELETE FROM stock_investments WHERE user_id = ? AND ticker = ?")
		_, err = tx.Exec(query, userID, ticker)
	} else {
		query = PrepareQuery("UPDATE stock_investments SET shares = ? WHERE user_id = ? AND ticker = ?")
		_, err = tx.Exec(query, newAmount, userID, ticker)
	}
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Failed to remove shares"})
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
		message := fmt.Sprintf("📉 **Stock Sale**\nYou sold **%.4f** shares of **%s** for **%d %s** (at $%.2f/share).",
			req.Shares, ticker, payout, config.Bot.CurrencyName, price)
		utils.SendWebhookNotification(userID, message)
	}()

	newBalance := database.GetBalance(userID)

	response := SellStockResponse{
		Ticker:         ticker,
		Shares:         req.Shares,
		AmountReceived: payout,
		PricePerShare:  price,
		Balance:        newBalance,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// PrepareQuery é um helper que converte placeholders para o driver correto
func PrepareQuery(query string) string {
	if config.DBType == "postgres" {
		return convertPlaceholders(query)
	}
	return query
}

func convertPlaceholders(query string) string {
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

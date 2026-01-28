package database

import (
	"database/sql"
	"time"
)

type Investment struct {
	UserID string
	Ticker string
	Shares float64
}

func GetInvestment(userID, ticker string) (float64, error) {
	var shares float64
	err := DB.QueryRow("SELECT shares FROM stock_investments WHERE user_id = ? AND ticker = ?", userID, ticker).Scan(&shares)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, nil
		}
		return 0, err
	}
	return shares, nil
}

func AddShares(userID, ticker string, amount float64) error {
	_, err := DB.Exec("INSERT INTO stock_investments (user_id, ticker, shares) VALUES (?, ?, ?) ON CONFLICT(user_id, ticker) DO UPDATE SET shares = shares + ?", userID, ticker, amount, amount)
	return err
}

func RemoveShares(userID, ticker string, amount float64) error {
	current, err := GetInvestment(userID, ticker)
	if err != nil {
		return err
	}
	if current < amount {
		return sql.ErrNoRows
	}
	
	newAmount := current - amount
	if newAmount <= 0.000001 { // Float precision safety, effectively 0
		_, err = DB.Exec("DELETE FROM stock_investments WHERE user_id = ? AND ticker = ?", userID, ticker)
	} else {
		_, err = DB.Exec("UPDATE stock_investments SET shares = ? WHERE user_id = ? AND ticker = ?", newAmount, userID, ticker)
	}
	return err
}

func SetStockPriceDB(ticker string, price float64) error {
	_, err := DB.Exec("INSERT INTO stock_prices (ticker, last_price, updated_at) VALUES (?, ?, ?) ON CONFLICT(ticker) DO UPDATE SET last_price = ?, updated_at = ?", ticker, price, time.Now(), price, time.Now())
	return err
}

func GetStockPriceDB(ticker string) (float64, error) {
	var price float64
	err := DB.QueryRow("SELECT last_price FROM stock_prices WHERE ticker = ?", ticker).Scan(&price)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, nil
		}
		return 0, err
	}
	return price, nil
}

func GetAllInvestmentsByTicker(ticker string) ([]Investment, error) {
	rows, err := DB.Query("SELECT user_id, shares FROM stock_investments WHERE ticker = ?", ticker)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var investments []Investment
	for rows.Next() {
		var i Investment
		i.Ticker = ticker
		if err := rows.Scan(&i.UserID, &i.Shares); err != nil {
			continue
		}
		investments = append(investments, i)
	}
	return investments, nil
}

func GetAllInvestmentsByUser(userID string) ([]Investment, error) {
	rows, err := DB.Query("SELECT ticker, shares FROM stock_investments WHERE user_id = ?", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var investments []Investment
	for rows.Next() {
		var i Investment
		i.UserID = userID
		if err := rows.Scan(&i.Ticker, &i.Shares); err != nil {
			continue
		}
		investments = append(investments, i)
	}
	return investments, nil
}

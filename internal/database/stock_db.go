package database

import (
	"database/sql"
	"time"

	"estudocoin/pkg/config"
)

// GetInvestment retorna a quantidade de ações que um usuário tem de um ticker
func GetInvestment(userID, ticker string) (float64, error) {
	var shares float64
	query := prepareQuery("SELECT shares FROM stock_investments WHERE user_id = ? AND ticker = ?")
	err := DB.QueryRow(query, userID, ticker).Scan(&shares)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, nil
		}
		return 0, err
	}
	return shares, nil
}

// AddShares adiciona ações para um usuário
func AddShares(userID, ticker string, amount float64) error {
	if config.DBType == "postgres" {
		query := `INSERT INTO stock_investments (user_id, ticker, shares) VALUES ($1, $2, $3) 
				  ON CONFLICT(user_id, ticker) DO UPDATE SET shares = stock_investments.shares + $3`
		_, err := DB.Exec(query, userID, ticker, amount)
		return err
	}
	query := "INSERT INTO stock_investments (user_id, ticker, shares) VALUES (?, ?, ?) ON CONFLICT(user_id, ticker) DO UPDATE SET shares = shares + ?"
	_, err := DB.Exec(query, userID, ticker, amount, amount)
	return err
}

// RemoveShares remove ações de um usuário
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
		query := prepareQuery("DELETE FROM stock_investments WHERE user_id = ? AND ticker = ?")
		_, err = DB.Exec(query, userID, ticker)
	} else {
		query := prepareQuery("UPDATE stock_investments SET shares = ? WHERE user_id = ? AND ticker = ?")
		_, err = DB.Exec(query, newAmount, userID, ticker)
	}
	return err
}

// SetStockPriceDB define o preço de uma ação
func SetStockPriceDB(ticker string, price float64) error {
	now := time.Now()
	if config.DBType == "postgres" {
		query := `INSERT INTO stock_prices (ticker, last_price, updated_at) VALUES ($1, $2, $3) 
				  ON CONFLICT(ticker) DO UPDATE SET last_price = $2, updated_at = $3`
		_, err := DB.Exec(query, ticker, price, now)
		return err
	}
	query := "INSERT INTO stock_prices (ticker, last_price, updated_at) VALUES (?, ?, ?) ON CONFLICT(ticker) DO UPDATE SET last_price = ?, updated_at = ?"
	_, err := DB.Exec(query, ticker, price, now, price, now)
	return err
}

// GetStockPriceDB retorna o preço de uma ação
func GetStockPriceDB(ticker string) (float64, error) {
	var price float64
	query := prepareQuery("SELECT last_price FROM stock_prices WHERE ticker = ?")
	err := DB.QueryRow(query, ticker).Scan(&price)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, nil
		}
		return 0, err
	}
	return price, nil
}

// GetAllInvestmentsByTicker retorna todos os investimentos de um ticker específico
func GetAllInvestmentsByTicker(ticker string) ([]Investment, error) {
	query := prepareQuery("SELECT user_id, shares FROM stock_investments WHERE ticker = ?")
	rows, err := DB.Query(query, ticker)
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

// GetAllInvestmentsByUser retorna todos os investimentos de um usuário
func GetAllInvestmentsByUser(userID string) ([]Investment, error) {
	query := prepareQuery("SELECT ticker, shares FROM stock_investments WHERE user_id = ?")
	rows, err := DB.Query(query, userID)
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

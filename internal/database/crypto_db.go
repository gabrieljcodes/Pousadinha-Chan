package database

import (
	"database/sql"
)

// CryptoInvestment represents a cryptocurrency investment
type CryptoInvestment struct {
	UserID        string
	Symbol        string
	Coins         float64
	TotalInvested float64
}

// CreateCryptoTables creates the necessary tables for cryptocurrencies
func (p *PostgresDatabase) CreateCryptoTables() error {
	createCryptoInvestmentsSQL := `CREATE TABLE IF NOT EXISTS crypto_investments (
		"user_id" TEXT NOT NULL,
		"symbol" TEXT NOT NULL,
		"coins" NUMERIC(28, 12) DEFAULT 0,
		"total_invested" NUMERIC(28, 4) DEFAULT 0,
		PRIMARY KEY (user_id, symbol)
	);`
	if _, err := p.db.Exec(createCryptoInvestmentsSQL); err != nil {
		return err
	}
	return nil
}

// GetCryptoInvestment returns the amount of coins a user holds for a crypto
func GetCryptoInvestment(userID, symbol string) (float64, error) {
	var coins float64
	query := prepareQuery("SELECT coins FROM crypto_investments WHERE user_id = ? AND symbol = ?")
	err := DB.QueryRow(query, userID, symbol).Scan(&coins)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, nil
		}
		return 0, err
	}
	return coins, nil
}

func AddCryptoShares(userID, symbol string, coins float64, cost int) error {
	query := `INSERT INTO crypto_investments (user_id, symbol, coins, total_invested) VALUES ($1, $2, $3, $4) 
			  ON CONFLICT(user_id, symbol) DO UPDATE SET 
			    coins = crypto_investments.coins + $3,
			    total_invested = crypto_investments.total_invested + $4`
	_, err := DB.Exec(query, userID, symbol, coins, cost)
	return err
}

// RemoveCryptoShares removes coins from a user atomically and proportionally reduces cost basis
func RemoveCryptoShares(userID, symbol string, coins float64) error {
	if coins <= 0 {
		return nil
	}

	res, err := DB.Exec(`UPDATE crypto_investments 
		SET total_invested = CASE WHEN coins <= $1 THEN 0 ELSE total_invested * (1 - ($1 / coins)) END,
		    coins = coins - $1 
		WHERE user_id = $2 AND symbol = $3 AND coins >= $1`, coins, userID, symbol)
	if err != nil {
		return err
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}

	// Clean up dust or zero coins
	_, _ = DB.Exec(`DELETE FROM crypto_investments WHERE user_id = $1 AND symbol = $2 AND coins <= 0.00000001`, userID, symbol)
	return nil
}

// GetAllCryptoInvestmentsByUser returns all crypto investments for a user
func GetAllCryptoInvestmentsByUser(userID string) ([]CryptoInvestment, error) {
	query := `SELECT symbol, coins, COALESCE(total_invested, 0) FROM crypto_investments WHERE user_id = $1`
	rows, err := DB.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var investments []CryptoInvestment
	for rows.Next() {
		var i CryptoInvestment
		i.UserID = userID
		if err := rows.Scan(&i.Symbol, &i.Coins, &i.TotalInvested); err != nil {
			continue
		}
		if i.Coins > 0 {
			investments = append(investments, i)
		}
	}
	return investments, nil
}

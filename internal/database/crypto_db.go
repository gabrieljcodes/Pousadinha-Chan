package database

import (
	"database/sql"
	"time"
)

// CryptoInvestment represents a cryptocurrency investment
type CryptoInvestment struct {
	GuildID       string
	UserID        string
	Symbol        string
	Coins         float64
	TotalInvested float64
}

// CreateCryptoTables creates the necessary tables for cryptocurrencies
func (p *PostgresDatabase) CreateCryptoTables() error {
	createCryptoInvestmentsSQL := `CREATE TABLE IF NOT EXISTS crypto_investments (
		"guild_id" TEXT NOT NULL DEFAULT '',
		"user_id" TEXT NOT NULL,
		"symbol" TEXT NOT NULL,
		"coins" NUMERIC(28, 12) DEFAULT 0,
		"total_invested" NUMERIC(28, 4) DEFAULT 0,
		PRIMARY KEY (guild_id, user_id, symbol)
	);`
	if _, err := p.db.Exec(createCryptoInvestmentsSQL); err != nil {
		return err
	}

	createCryptoPricesSQL := `CREATE TABLE IF NOT EXISTS crypto_prices (
		"symbol" TEXT PRIMARY KEY,
		"last_price" NUMERIC(28, 6) DEFAULT 0,
		"updated_at" TIMESTAMPTZ DEFAULT NOW()
	);`
	if _, err := p.db.Exec(createCryptoPricesSQL); err != nil {
		return err
	}
	return nil
}

// GetCryptoInvestment returns the amount of coins a user holds for a crypto in a guild
func GetCryptoInvestment(guildID, userID, symbol string) (float64, error) {
	var coins float64
	query := prepareQuery("SELECT coins FROM crypto_investments WHERE guild_id = ? AND user_id = ? AND symbol = ?")
	err := DB.QueryRow(query, guildID, userID, symbol).Scan(&coins)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, nil
		}
		return 0, err
	}
	return coins, nil
}

func AddCryptoShares(guildID, userID, symbol string, coins float64, cost int) error {
	query := `INSERT INTO crypto_investments (guild_id, user_id, symbol, coins, total_invested) VALUES ($1, $2, $3, $4, $5) 
			  ON CONFLICT(guild_id, user_id, symbol) DO UPDATE SET 
			    coins = crypto_investments.coins + $4,
			    total_invested = crypto_investments.total_invested + $5`
	_, err := DB.Exec(query, guildID, userID, symbol, coins, cost)
	return err
}

// RemoveCryptoShares removes coins from a user atomically and proportionally reduces cost basis
func RemoveCryptoShares(guildID, userID, symbol string, coins float64) error {
	if coins <= 0 {
		return nil
	}

	res, err := DB.Exec(`UPDATE crypto_investments 
		SET total_invested = CASE WHEN coins <= $1 THEN 0 ELSE total_invested * (1 - ($1 / coins)) END,
		    coins = coins - $1 
		WHERE guild_id = $2 AND user_id = $3 AND symbol = $4 AND coins >= $1`, coins, guildID, userID, symbol)
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
	_, _ = DB.Exec(`DELETE FROM crypto_investments WHERE guild_id = $1 AND user_id = $2 AND symbol = $3 AND coins <= 0.00000001`, guildID, userID, symbol)
	return nil
}

// GetAllCryptoInvestmentsByUser returns all crypto investments for a user in a guild
func GetAllCryptoInvestmentsByUser(guildID, userID string) ([]CryptoInvestment, error) {
	query := `SELECT symbol, coins, COALESCE(total_invested, 0) FROM crypto_investments WHERE guild_id = $1 AND user_id = $2`
	rows, err := DB.Query(query, guildID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var investments []CryptoInvestment
	for rows.Next() {
		var i CryptoInvestment
		i.GuildID = guildID
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

// SetCryptoPriceDB sets the price of a crypto token in the database
func SetCryptoPriceDB(symbol string, price float64) error {
	if DB == nil {
		return nil
	}
	now := time.Now()
	query := `INSERT INTO crypto_prices (symbol, last_price, updated_at) VALUES ($1, $2, $3)
			  ON CONFLICT(symbol) DO UPDATE SET last_price = $2, updated_at = $3`
	_, err := DB.Exec(query, symbol, price, now)
	return err
}

// SetCryptoPricesBatchDB stores multiple crypto prices in the database
func SetCryptoPricesBatchDB(prices map[string]float64) error {
	if DB == nil || len(prices) == 0 {
		return nil
	}
	now := time.Now()
	for symbol, price := range prices {
		query := `INSERT INTO crypto_prices (symbol, last_price, updated_at) VALUES ($1, $2, $3)
				  ON CONFLICT(symbol) DO UPDATE SET last_price = $2, updated_at = $3`
		if _, err := DB.Exec(query, symbol, price, now); err != nil {
			return err
		}
	}
	return nil
}

// GetCryptoPriceDB gets the stored price of a crypto token
func GetCryptoPriceDB(symbol string) (float64, error) {
	if DB == nil {
		return 0, nil
	}
	var price float64
	query := prepareQuery("SELECT last_price FROM crypto_prices WHERE symbol = ?")
	err := DB.QueryRow(query, symbol).Scan(&price)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, nil
		}
		return 0, err
	}
	return price, nil
}


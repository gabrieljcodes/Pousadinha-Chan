package database

import (
	"database/sql"
	"time"
)

// GetInvestment returns the number of shares a user holds for a ticker in a guild
func GetInvestment(guildID, userID, ticker string) (float64, error) {
	var shares float64
	query := prepareQuery("SELECT shares FROM stock_investments WHERE guild_id = ? AND user_id = ? AND ticker = ?")
	err := DB.QueryRow(query, guildID, userID, ticker).Scan(&shares)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, nil
		}
		return 0, err
	}
	return shares, nil
}

func AddShares(guildID, userID, ticker string, amount float64, cost int) error {
	query := `INSERT INTO stock_investments (guild_id, user_id, ticker, shares, total_invested) VALUES ($1, $2, $3, $4, $5) 
			  ON CONFLICT(guild_id, user_id, ticker) DO UPDATE SET 
			    shares = stock_investments.shares + $4,
			    total_invested = stock_investments.total_invested + $5`
	_, err := DB.Exec(query, guildID, userID, ticker, amount, cost)
	return err
}

// RemoveShares removes shares from a user atomically and proportionally reduces cost basis
func RemoveShares(guildID, userID, ticker string, amount float64) error {
	if amount <= 0 {
		return nil
	}

	res, err := DB.Exec(`UPDATE stock_investments 
		SET total_invested = CASE WHEN shares <= $1 THEN 0 ELSE total_invested * (1 - ($1 / shares)) END,
		    shares = shares - $1 
		WHERE guild_id = $2 AND user_id = $3 AND ticker = $4 AND shares >= $1`, amount, guildID, userID, ticker)
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

	// Clean up dust or zero shares
	_, _ = DB.Exec(`DELETE FROM stock_investments WHERE guild_id = $1 AND user_id = $2 AND ticker = $3 AND shares <= 0.000001`, guildID, userID, ticker)
	return nil
}

func SetStockPriceDB(ticker string, price float64) error {
	now := time.Now()
	query := `INSERT INTO stock_prices (ticker, last_price, updated_at) VALUES ($1, $2, $3) 
			  ON CONFLICT(ticker) DO UPDATE SET last_price = $2, updated_at = $3`
	_, err := DB.Exec(query, ticker, price, now)
	return err
}

// GetStockPriceDB returns the price of a stock
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

// GetAllInvestmentsByTicker returns all investments for a specific ticker in a guild
func GetAllInvestmentsByTicker(guildID, ticker string) ([]Investment, error) {
	query := `SELECT user_id, shares, COALESCE(total_invested, 0) FROM stock_investments WHERE guild_id = $1 AND ticker = $2`
	rows, err := DB.Query(query, guildID, ticker)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var investments []Investment
	for rows.Next() {
		var i Investment
		i.GuildID = guildID
		i.Ticker = ticker
		if err := rows.Scan(&i.UserID, &i.Shares, &i.TotalInvested); err != nil {
			continue
		}
		investments = append(investments, i)
	}
	return investments, nil
}

// GetAllInvestmentsByUser returns all stock investments for a user in a guild
func GetAllInvestmentsByUser(guildID, userID string) ([]Investment, error) {
	query := `SELECT ticker, shares, COALESCE(total_invested, 0) FROM stock_investments WHERE guild_id = $1 AND user_id = $2`
	rows, err := DB.Query(query, guildID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var investments []Investment
	for rows.Next() {
		var i Investment
		i.GuildID = guildID
		i.UserID = userID
		if err := rows.Scan(&i.Ticker, &i.Shares, &i.TotalInvested); err != nil {
			continue
		}
		investments = append(investments, i)
	}
	return investments, nil
}

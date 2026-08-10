package database

import (
	"database/sql"
)

// CryptoInvestment represents a cryptocurrency investment
type CryptoInvestment struct {
	UserID string
	Symbol string
	Coins  float64
}

// CreateCryptoTables cria as tabelas necessárias para criptomoedas
func (p *PostgresDatabase) CreateCryptoTables() error {
	createCryptoInvestmentsSQL := `CREATE TABLE IF NOT EXISTS crypto_investments (
		"user_id" TEXT NOT NULL,
		"symbol" TEXT NOT NULL,
		"coins" REAL DEFAULT 0,
		PRIMARY KEY (user_id, symbol)
	);`
	if _, err := p.db.Exec(createCryptoInvestmentsSQL); err != nil {
		return err
	}
	return nil
}



// GetCryptoInvestment retorna a quantidade de coins que um usuário tem de uma crypto
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

func AddCryptoShares(userID, symbol string, coins float64) error {
	query := `INSERT INTO crypto_investments (user_id, symbol, coins) VALUES ($1, $2, $3) 
			  ON CONFLICT(user_id, symbol) DO UPDATE SET coins = crypto_investments.coins + $3`
	_, err := DB.Exec(query, userID, symbol, coins)
	return err
}

// RemoveCryptoShares remove coins de um usuário
func RemoveCryptoShares(userID, symbol string, coins float64) error {
	current, err := GetCryptoInvestment(userID, symbol)
	if err != nil {
		return err
	}
	if current < coins {
		return sql.ErrNoRows
	}

	newAmount := current - coins
	if newAmount <= 0.00000001 { // Float precision safety
		query := prepareQuery("DELETE FROM crypto_investments WHERE user_id = ? AND symbol = ?")
		_, err = DB.Exec(query, userID, symbol)
	} else {
		query := prepareQuery("UPDATE crypto_investments SET coins = ? WHERE user_id = ? AND symbol = ?")
		_, err = DB.Exec(query, newAmount, userID, symbol)
	}
	return err
}

// GetAllCryptoInvestmentsByUser retorna todos os investimentos em crypto de um usuário
func GetAllCryptoInvestmentsByUser(userID string) ([]CryptoInvestment, error) {
	query := prepareQuery("SELECT symbol, coins FROM crypto_investments WHERE user_id = ?")
	rows, err := DB.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var investments []CryptoInvestment
	for rows.Next() {
		var i CryptoInvestment
		i.UserID = userID
		if err := rows.Scan(&i.Symbol, &i.Coins); err != nil {
			continue
		}
		if i.Coins > 0 {
			investments = append(investments, i)
		}
	}
	return investments, nil
}

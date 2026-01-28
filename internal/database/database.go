package database

import (
	"database/sql"
	"log"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

var DB *sql.DB

func Initialize() {
	var err error
	DB, err = sql.Open("sqlite3", "./pousadinha-chan.db")
	if err != nil {
		log.Fatal(err)
	}

	createTableSQL := `CREATE TABLE IF NOT EXISTS users (
		"id" TEXT NOT NULL PRIMARY KEY,
		"balance" INTEGER DEFAULT 0,
		"last_daily" DATETIME,
		"webhook_url" TEXT
	);`

	_, err = DB.Exec(createTableSQL)
	if err != nil {
		log.Fatal(err)
	}

	// Migration: Try to add column if it doesn't exist (SQLite safe-ish way is just try/ignore or check)
	// Simple way: Try adding, ignore error
	_, _ = DB.Exec(`ALTER TABLE users ADD COLUMN webhook_url TEXT;`)

	createApiTableSQL := `CREATE TABLE IF NOT EXISTS api_keys (
		"key" TEXT NOT NULL PRIMARY KEY,
		"user_id" TEXT NOT NULL,
		"name" TEXT,
		"created_at" DATETIME
	);`

	_, err = DB.Exec(createApiTableSQL)
	if err != nil {
		log.Fatal(err)
	}

	createStockInvestmentsSQL := `CREATE TABLE IF NOT EXISTS stock_investments (
		"user_id" TEXT NOT NULL,
		"ticker" TEXT NOT NULL,
		"shares" REAL DEFAULT 0,
		PRIMARY KEY (user_id, ticker)
	);`

	_, err = DB.Exec(createStockInvestmentsSQL)
	if err != nil {
		log.Fatal(err)
	}

	createStockPricesSQL := `CREATE TABLE IF NOT EXISTS stock_prices (
		"ticker" TEXT NOT NULL PRIMARY KEY,
		"last_price" REAL DEFAULT 0,
		"updated_at" DATETIME
	);`

	_, err = DB.Exec(createStockPricesSQL)
	if err != nil {
		log.Fatal(err)
	}
}

func GetBalance(userID string) int {
	var balance int
	err := DB.QueryRow("SELECT balance FROM users WHERE id = ?", userID).Scan(&balance)
	if err != nil {
		if err == sql.ErrNoRows {
			DB.Exec("INSERT INTO users (id, balance) VALUES (?, 0)", userID)
			return 0
		}
		log.Println("Error getting balance:", err)
		return 0
	}
	return balance
}

type UserBalance struct {
	ID      string
	Balance int
}

func GetLeaderboard(limit int) ([]UserBalance, error) {
	rows, err := DB.Query("SELECT id, balance FROM users ORDER BY balance DESC LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []UserBalance
	for rows.Next() {
		var u UserBalance
		if err := rows.Scan(&u.ID, &u.Balance); err != nil {
			continue
		}
		users = append(users, u)
	}
	return users, nil
}

func AddCoins(userID string, amount int) error {
	_, err := DB.Exec("INSERT INTO users (id, balance) VALUES (?, ?) ON CONFLICT(id) DO UPDATE SET balance = balance + ?", userID, amount, amount)
	return err
}

func RemoveCoins(userID string, amount int) error {
	current := GetBalance(userID)
	if current < amount {
		return sql.ErrNoRows
	}
	_, err := DB.Exec("UPDATE users SET balance = balance - ? WHERE id = ?", amount, userID)
	return err
}

func TransferCoins(fromID, toID string, amount int) error {
	tx, err := DB.Begin()
	if err != nil {
		return err
	}

	var fromBalance int
	err = tx.QueryRow("SELECT balance FROM users WHERE id = ?", fromID).Scan(&fromBalance)
	if err != nil {
		tx.Rollback()
		return err
	}

	if fromBalance < amount {
		tx.Rollback()
		return sql.ErrNoRows
	}

	_, err = tx.Exec("UPDATE users SET balance = balance - ? WHERE id = ?", amount, fromID)
	if err != nil {
		tx.Rollback()
		return err
	}

	_, err = tx.Exec("INSERT INTO users (id, balance) VALUES (?, ?) ON CONFLICT(id) DO UPDATE SET balance = balance + ?", toID, amount, amount)
	if err != nil {
		tx.Rollback()
		return err
	}

	return tx.Commit()
}

func CanDaily(userID string) bool {
	var lastDaily sql.NullTime
	err := DB.QueryRow("SELECT last_daily FROM users WHERE id = ?", userID).Scan(&lastDaily)
	if err != nil {
		return true
	}

	if !lastDaily.Valid {
		return true
	}

	return time.Since(lastDaily.Time) >= 24*time.Hour
}

func GetNextDailyTime(userID string) time.Time {
	var lastDaily sql.NullTime
	err := DB.QueryRow("SELECT last_daily FROM users WHERE id = ?", userID).Scan(&lastDaily)
	if err != nil || !lastDaily.Valid {
		return time.Now() // Available now or never collected
	}
	return lastDaily.Time.Add(24 * time.Hour)
}

func SetDaily(userID string) error {
	_, err := DB.Exec("INSERT INTO users (id, balance, last_daily) VALUES (?, 0, ?) ON CONFLICT(id) DO UPDATE SET last_daily = ?", userID, time.Now(), time.Now())
	return err
}

func CreateAPIKey(key, userID, name string) error {
	_, err := DB.Exec("INSERT INTO api_keys (key, user_id, name, created_at) VALUES (?, ?, ?, ?)", key, userID, name, time.Now())
	return err
}

func GetUserByAPIKey(key string) (string, error) {
	var userID string
	err := DB.QueryRow("SELECT user_id FROM api_keys WHERE key = ?", key).Scan(&userID)
	if err != nil {
		return "", err
	}
	return userID, nil
}

type APIKeyStruct struct {
	Key       string
	Name      string
	CreatedAt time.Time
}

func ListAPIKeys(userID string) ([]APIKeyStruct, error) {
	rows, err := DB.Query("SELECT key, name, created_at FROM api_keys WHERE user_id = ?", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []APIKeyStruct
	for rows.Next() {
		var k APIKeyStruct
		if err := rows.Scan(&k.Key, &k.Name, &k.CreatedAt); err != nil {
			continue
		}
		keys = append(keys, k)
	}
	return keys, nil
}

func DeleteAPIKey(userID, prefix string) error {
	_, err := DB.Exec("DELETE FROM api_keys WHERE user_id = ? AND key LIKE ?", userID, prefix+"%")
	return err
}

func SetWebhook(userID, url string) error {
	// Upsert user if not exists
	_, err := DB.Exec("INSERT INTO users (id, balance, webhook_url) VALUES (?, 0, ?) ON CONFLICT(id) DO UPDATE SET webhook_url = ?", userID, url, url)
	return err
}

func GetWebhook(userID string) (string, error) {
	var url sql.NullString
	err := DB.QueryRow("SELECT webhook_url FROM users WHERE id = ?", userID).Scan(&url)
	if err != nil {
		return "", err
	}
	return url.String, nil
}

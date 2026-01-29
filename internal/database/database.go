package database

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"time"

	"estudocoin/pkg/config"
)

// Initialize inicializa o banco de dados baseado na configuração
func Initialize() {
	var err error

	switch config.DBType {
	case "postgres":
		log.Println("Initializing PostgreSQL database...")
		DB, err = NewPostgres(config.ConnString)
	case "sqlite":
		fallthrough
	default:
		log.Println("Initializing SQLite database...")
		DB, err = NewSQLite(config.ConnString)
	}

	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}

	if err := DB.Ping(); err != nil {
		log.Fatalf("Failed to ping database: %v", err)
	}

	log.Printf("Database initialized successfully (type: %s)", config.DBType)
}

// ShouldSkipTableCreation verifica se deve pular a criação de tabelas
func ShouldSkipTableCreation() bool {
	return os.Getenv("DB_SKIP_TABLE_CREATION") == "true"
}

// NewSQLite cria e inicializa um banco SQLite
func NewSQLite(connString string) (Database, error) {
	db := NewSQLiteDatabase(connString)
	if err := db.Open(); err != nil {
		return nil, err
	}
	if err := db.CreateTables(); err != nil {
		return nil, err
	}
	return db, nil
}

// NewPostgres cria e inicializa um banco PostgreSQL
func NewPostgres(connString string) (Database, error) {
	db := NewPostgresDatabase(connString)
	if err := db.Open(); err != nil {
		return nil, err
	}
	if err := db.CreateTables(); err != nil {
		return nil, err
	}
	return db, nil
}

// Helper functions para facilitar a migração das queries existentes

// prepareQuery converte uma query com ? para o formato correto do driver
func prepareQuery(query string) string {
	if config.DBType == "postgres" {
		// Converter ? para $1, $2, etc.
		return convertPlaceholders(query)
	}
	return query
}

// convertPlaceholders converte ? placeholders para $N (PostgreSQL)
func convertPlaceholders(query string) string {
	if config.DBType != "postgres" {
		return query
	}

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

// GetBalance retorna o saldo de um usuário
func GetBalance(userID string) int {
	var balance int
	query := prepareQuery("SELECT balance FROM users WHERE id = ?")
	err := DB.QueryRow(query, userID).Scan(&balance)
	if err != nil {
		if err == sql.ErrNoRows {
			DB.Exec(prepareQuery("INSERT INTO users (id, balance) VALUES (?, 0)"), userID)
			return 0
		}
		log.Println("Error getting balance:", err)
		return 0
	}
	return balance
}

// GetLeaderboard retorna o ranking de saldos
func GetLeaderboard(limit int) ([]UserBalance, error) {
	query := prepareQuery("SELECT id, balance FROM users ORDER BY balance DESC LIMIT ?")
	rows, err := DB.Query(query, limit)
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

// AddCoins adiciona moedas a um usuário
func AddCoins(userID string, amount int) error {
	if config.DBType == "postgres" {
		// PostgreSQL usa sintaxe diferente para upsert
		query := `INSERT INTO users (id, balance) VALUES ($1, $2) 
				  ON CONFLICT(id) DO UPDATE SET balance = users.balance + $2`
		_, err := DB.Exec(query, userID, amount)
		return err
	}
	query := "INSERT INTO users (id, balance) VALUES (?, ?) ON CONFLICT(id) DO UPDATE SET balance = balance + ?"
	_, err := DB.Exec(query, userID, amount, amount)
	return err
}

// RemoveCoins remove moedas de um usuário
func RemoveCoins(userID string, amount int) error {
	current := GetBalance(userID)
	if current < amount {
		return sql.ErrNoRows
	}
	query := prepareQuery("UPDATE users SET balance = balance - ? WHERE id = ?")
	_, err := DB.Exec(query, amount, userID)
	return err
}

// TransferCoins transfere moedas entre usuários
func TransferCoins(fromID, toID string, amount int) error {
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var fromBalance int
	err = tx.QueryRow(prepareQuery("SELECT balance FROM users WHERE id = ?"), fromID).Scan(&fromBalance)
	if err != nil {
		return err
	}

	if fromBalance < amount {
		return sql.ErrNoRows
	}

	_, err = tx.Exec(prepareQuery("UPDATE users SET balance = balance - ? WHERE id = ?"), amount, fromID)
	if err != nil {
		return err
	}

	if config.DBType == "postgres" {
		_, err = tx.Exec(`INSERT INTO users (id, balance) VALUES ($1, $2) 
						  ON CONFLICT(id) DO UPDATE SET balance = users.balance + $2`,
			toID, amount)
	} else {
		_, err = tx.Exec("INSERT INTO users (id, balance) VALUES (?, ?) ON CONFLICT(id) DO UPDATE SET balance = balance + ?",
			toID, amount, amount)
	}
	if err != nil {
		return err
	}

	return tx.Commit()
}

// CanDaily verifica se o usuário pode coletar o daily
func CanDaily(userID string) bool {
	var lastDaily sql.NullTime
	query := prepareQuery("SELECT last_daily FROM users WHERE id = ?")
	err := DB.QueryRow(query, userID).Scan(&lastDaily)
	if err != nil {
		return true
	}

	if !lastDaily.Valid {
		return true
	}

	return time.Since(lastDaily.Time) >= 24*time.Hour
}

// GetNextDailyTime retorna quando o próximo daily estará disponível
func GetNextDailyTime(userID string) time.Time {
	var lastDaily sql.NullTime
	query := prepareQuery("SELECT last_daily FROM users WHERE id = ?")
	err := DB.QueryRow(query, userID).Scan(&lastDaily)
	if err != nil || !lastDaily.Valid {
		return time.Now()
	}
	return lastDaily.Time.Add(24 * time.Hour)
}

// SetDaily registra que o usuário coletou o daily
func SetDaily(userID string) error {
	now := time.Now()
	if config.DBType == "postgres" {
		query := `INSERT INTO users (id, balance, last_daily) VALUES ($1, 0, $2) 
				  ON CONFLICT(id) DO UPDATE SET last_daily = $2`
		_, err := DB.Exec(query, userID, now)
		return err
	}
	query := "INSERT INTO users (id, balance, last_daily) VALUES (?, 0, ?) ON CONFLICT(id) DO UPDATE SET last_daily = ?"
	_, err := DB.Exec(query, userID, now, now)
	return err
}

// CreateAPIKey cria uma nova chave de API
func CreateAPIKey(key, userID, name string) error {
	query := prepareQuery("INSERT INTO api_keys (key, user_id, name, created_at) VALUES (?, ?, ?, ?)")
	_, err := DB.Exec(query, key, userID, name, time.Now())
	return err
}

// GetUserByAPIKey retorna o userID de uma chave de API
func GetUserByAPIKey(key string) (string, error) {
	var userID string
	query := prepareQuery("SELECT user_id FROM api_keys WHERE key = ?")
	err := DB.QueryRow(query, key).Scan(&userID)
	if err != nil {
		return "", err
	}
	return userID, nil
}

// ListAPIKeys lista todas as chaves de API de um usuário
func ListAPIKeys(userID string) ([]APIKeyStruct, error) {
	query := prepareQuery("SELECT key, name, created_at FROM api_keys WHERE user_id = ?")
	rows, err := DB.Query(query, userID)
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

// DeleteAPIKey deleta uma chave de API
func DeleteAPIKey(userID, prefix string) error {
	if config.DBType == "postgres" {
		query := prepareQuery("DELETE FROM api_keys WHERE user_id = ? AND key LIKE ?")
		_, err := DB.Exec(query, userID, prefix+"%")
		return err
	}
	query := prepareQuery("DELETE FROM api_keys WHERE user_id = ? AND key LIKE ?")
	_, err := DB.Exec(query, userID, prefix+"%")
	return err
}

// SetWebhook define a URL de webhook de um usuário
func SetWebhook(userID, url string) error {
	if config.DBType == "postgres" {
		query := `INSERT INTO users (id, balance, webhook_url) VALUES ($1, 0, $2) 
				  ON CONFLICT(id) DO UPDATE SET webhook_url = $2`
		_, err := DB.Exec(query, userID, url)
		return err
	}
	query := "INSERT INTO users (id, balance, webhook_url) VALUES (?, 0, ?) ON CONFLICT(id) DO UPDATE SET webhook_url = ?"
	_, err := DB.Exec(query, userID, url, url)
	return err
}

// GetWebhook retorna a URL de webhook de um usuário
func GetWebhook(userID string) (string, error) {
	var url sql.NullString
	query := prepareQuery("SELECT webhook_url FROM users WHERE id = ?")
	err := DB.QueryRow(query, userID).Scan(&url)
	if err != nil {
		return "", err
	}
	return url.String, nil
}

package database

import (
	"database/sql"
	"time"
)

// Database defines the interface for database operations
type Database interface {
	// Connection
	Open() error
	Close() error
	Ping() error
	GetDB() *sql.DB

	// Query Builders - return formatted queries for the specific driver
	Query(query string, args ...interface{}) (*sql.Rows, error)
	QueryRow(query string, args ...interface{}) *sql.Row
	Exec(query string, args ...interface{}) (sql.Result, error)
	Begin() (*sql.Tx, error)

	// Placeholder returns the correct placeholder for the driver ($N for PostgreSQL)
	Placeholder(index int) string

	// UpsertSyntax returns the correct syntax for upsert
	UpsertSyntax(table string, conflictCols []string, updateCols []string, values []interface{}) (string, []interface{})
}

// UserBalance represents a user's balance
type UserBalance struct {
	ID              string
	Balance         int
	StockValue      int
	CryptoValue     int
	TotalNetWorth   int
}

// UserStreakRank represents a user's daily streak ranking
type UserStreakRank struct {
	ID        string
	Streak    int
	MaxStreak int
}

// APIKeyStruct represents an API key
type APIKeyStruct struct {
	Key       string
	KeyPrefix string
	Name      string
	CreatedAt time.Time
}

// Investment represents a stock investment
type Investment struct {
	UserID        string
	Ticker        string
	Shares        float64
	TotalInvested float64
}

// DB is the global database instance
var DB Database

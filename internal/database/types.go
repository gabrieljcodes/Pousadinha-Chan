package database

import (
	"database/sql"
	"time"
)

// Database define a interface para operações de banco de dados
type Database interface {
	// Connection
	Open() error
	Close() error
	Ping() error
	GetDB() *sql.DB

	// Query Builders - retornam queries formatadas para o driver específico
	Query(query string, args ...interface{}) (*sql.Rows, error)
	QueryRow(query string, args ...interface{}) *sql.Row
	Exec(query string, args ...interface{}) (sql.Result, error)
	Begin() (*sql.Tx, error)

	// Placeholder retorna o placeholder correto para o driver (? para SQLite, $N para PostgreSQL)
	Placeholder(index int) string

	// UpsertSyntax retorna a sintaxe correta para upsert
	UpsertSyntax(table string, conflictCols []string, updateCols []string, values []interface{}) (string, []interface{})
}

// UserBalance representa o saldo de um usuário
type UserBalance struct {
	ID              string
	Balance         int
	StockValue      int
	CryptoValue     int
	TotalNetWorth   int
}

// APIKeyStruct representa uma chave de API
type APIKeyStruct struct {
	Key       string
	Name      string
	CreatedAt time.Time
}

// Investment representa um investimento em ações
type Investment struct {
	UserID string
	Ticker string
	Shares float64
}

// DB é a instância global do database
var DB Database

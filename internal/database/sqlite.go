package database

import (
	"database/sql"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
)

// SQLiteDatabase implementa a interface Database para SQLite
type SQLiteDatabase struct {
	connString string
	db         *sql.DB
}

// NewSQLiteDatabase cria uma nova instância do database SQLite
func NewSQLiteDatabase(connString string) *SQLiteDatabase {
	return &SQLiteDatabase{
		connString: connString,
	}
}

// Open abre a conexão com o banco de dados
func (s *SQLiteDatabase) Open() error {
	db, err := sql.Open("sqlite3", s.connString)
	if err != nil {
		return err
	}
	s.db = db
	return nil
}

// Close fecha a conexão com o banco de dados
func (s *SQLiteDatabase) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// Ping verifica se a conexão está ativa
func (s *SQLiteDatabase) Ping() error {
	if s.db == nil {
		return fmt.Errorf("database not connected")
	}
	return s.db.Ping()
}

// GetDB retorna a instância *sql.DB subjacente
func (s *SQLiteDatabase) GetDB() *sql.DB {
	return s.db
}

// Query executa uma query SELECT
func (s *SQLiteDatabase) Query(query string, args ...interface{}) (*sql.Rows, error) {
	return s.db.Query(query, args...)
}

// QueryRow executa uma query que retorna uma única linha
func (s *SQLiteDatabase) QueryRow(query string, args ...interface{}) *sql.Row {
	return s.db.QueryRow(query, args...)
}

// Exec executa uma query que não retorna linhas
func (s *SQLiteDatabase) Exec(query string, args ...interface{}) (sql.Result, error) {
	return s.db.Exec(query, args...)
}

// Begin inicia uma transação
func (s *SQLiteDatabase) Begin() (*sql.Tx, error) {
	return s.db.Begin()
}

// Placeholder retorna ? para SQLite (não usa índice)
func (s *SQLiteDatabase) Placeholder(index int) string {
	return "?"
}

// UpsertSyntax retorna a sintaxe de upsert para SQLite (INSERT ON CONFLICT)
func (s *SQLiteDatabase) UpsertSyntax(table string, conflictCols []string, updateCols []string, values []interface{}) (string, []interface{}) {
	// Construir colunas
	allCols := append(conflictCols, updateCols...)
	colNames := ""
	placeholders := ""
	for i, col := range allCols {
		if i > 0 {
			colNames += ", "
			placeholders += ", "
		}
		colNames += col
		placeholders += "?"
	}

	// Construir updates
	updates := ""
	for i, col := range updateCols {
		if i > 0 {
			updates += ", "
		}
		updates += fmt.Sprintf("%s = ?", col)
		// Adicionar valores para update
		values = append(values, values[len(conflictCols)+i])
	}

	// Construir conflict target
	conflictTarget := ""
	for i, col := range conflictCols {
		if i > 0 {
			conflictTarget += ", "
		}
		conflictTarget += col
	}

	query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) ON CONFLICT(%s) DO UPDATE SET %s",
		table, colNames, placeholders, conflictTarget, updates)

	return query, values
}

// CreateTables cria as tabelas necessárias para SQLite
func (s *SQLiteDatabase) CreateTables() error {
	createTableSQL := `CREATE TABLE IF NOT EXISTS users (
		"id" TEXT NOT NULL PRIMARY KEY,
		"balance" INTEGER DEFAULT 0,
		"last_daily" DATETIME,
		"webhook_url" TEXT
	);`
	if _, err := s.db.Exec(createTableSQL); err != nil {
		return err
	}

	// Migration: Try to add column if it doesn't exist
	_, _ = s.db.Exec(`ALTER TABLE users ADD COLUMN webhook_url TEXT;`)

	createApiTableSQL := `CREATE TABLE IF NOT EXISTS api_keys (
		"key" TEXT NOT NULL PRIMARY KEY,
		"user_id" TEXT NOT NULL,
		"name" TEXT,
		"created_at" DATETIME
	);`
	if _, err := s.db.Exec(createApiTableSQL); err != nil {
		return err
	}

	createStockInvestmentsSQL := `CREATE TABLE IF NOT EXISTS stock_investments (
		"user_id" TEXT NOT NULL,
		"ticker" TEXT NOT NULL,
		"shares" REAL DEFAULT 0,
		PRIMARY KEY (user_id, ticker)
	);`
	if _, err := s.db.Exec(createStockInvestmentsSQL); err != nil {
		return err
	}

	createStockPricesSQL := `CREATE TABLE IF NOT EXISTS stock_prices (
		"ticker" TEXT NOT NULL PRIMARY KEY,
		"last_price" REAL DEFAULT 0,
		"updated_at" DATETIME
	);`
	if _, err := s.db.Exec(createStockPricesSQL); err != nil {
		return err
	}

	// Criar tabelas de crypto
	if err := s.CreateCryptoTables(); err != nil {
		return err
	}
	
	createValorantBetsSQL := `CREATE TABLE IF NOT EXISTS valorant_bets (
        id SERIAL PRIMARY KEY,
        user_id TEXT NOT NULL,
        riot_id TEXT NOT NULL,
        bet_on_loss BOOLEAN NOT NULL,
        amount INTEGER NOT NULL,
        created_at TIMESTAMP NOT NULL,
        checked_at TIMESTAMP,
        match_id TEXT,
        resolved BOOLEAN DEFAULT FALSE
    );`
    if _, err := p.db.Exec(createValorantBetsSQL); err != nil {
        return err
    }

	return nil
}

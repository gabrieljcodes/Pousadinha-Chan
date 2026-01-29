package database

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// PostgresDatabase implementa a interface Database para PostgreSQL usando pgx (recomendado pelo Supabase)
type PostgresDatabase struct {
	connString string
	db         *sql.DB
}

// NewPostgresDatabase cria uma nova instância do database PostgreSQL
func NewPostgresDatabase(connString string) *PostgresDatabase {
	return &PostgresDatabase{
		connString: connString,
	}
}

// Open abre a conexão com o banco de dados
func (p *PostgresDatabase) Open() error {
	log.Printf("Connecting to PostgreSQL using pgx driver...")
	log.Printf("Connection string (masked): %s", maskPassword(p.connString))

	// Usar pgx como driver em vez de pq - melhor suporte para Supabase pooler
	db, err := sql.Open("pgx", p.connString)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	// Configurar pool de conexões otimizado para Supabase
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(0)

	p.db = db
	return nil
}

// maskPassword oculta a senha na string de conexão para logs
func maskPassword(connString string) string {
	result := connString
	if idx := indexOf(result, "://"); idx >= 0 {
		start := idx + 3
		if atIdx := indexOf(result[start:], "@"); atIdx >= 0 {
			userPass := result[start : start+atIdx]
			if colonIdx := indexOf(userPass, ":"); colonIdx >= 0 {
				user := userPass[:colonIdx]
				result = result[:start] + user + ":****@" + result[start+atIdx+1:]
			}
		}
	}
	return result
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// Close fecha a conexão com o banco de dados
func (p *PostgresDatabase) Close() error {
	if p.db != nil {
		return p.db.Close()
	}
	return nil
}

// Ping verifica se a conexão está ativa
func (p *PostgresDatabase) Ping() error {
	if p.db == nil {
		return fmt.Errorf("database not connected")
	}
	return p.db.Ping()
}

// GetDB retorna a instância *sql.DB subjacente
func (p *PostgresDatabase) GetDB() *sql.DB {
	return p.db
}

// Query executa uma query SELECT
func (p *PostgresDatabase) Query(query string, args ...interface{}) (*sql.Rows, error) {
	return p.db.Query(query, args...)
}

// QueryRow executa uma query que retorna uma única linha
func (p *PostgresDatabase) QueryRow(query string, args ...interface{}) *sql.Row {
	return p.db.QueryRow(query, args...)
}

// Exec executa uma query que não retorna linhas
func (p *PostgresDatabase) Exec(query string, args ...interface{}) (sql.Result, error) {
	return p.db.Exec(query, args...)
}

// Begin inicia uma transação
func (p *PostgresDatabase) Begin() (*sql.Tx, error) {
	return p.db.Begin()
}

// Placeholder retorna $N para PostgreSQL (1-indexed)
func (p *PostgresDatabase) Placeholder(index int) string {
	return fmt.Sprintf("$%d", index)
}

// UpsertSyntax retorna a sintaxe de upsert para PostgreSQL (INSERT ON CONFLICT)
func (p *PostgresDatabase) UpsertSyntax(table string, conflictCols []string, updateCols []string, values []interface{}) (string, []interface{}) {
	// Construir colunas
	allCols := append(conflictCols, updateCols...)
	colNames := ""
	placeholders := ""
	placeholderIndex := 1

	for i, col := range allCols {
		if i > 0 {
			colNames += ", "
			placeholders += ", "
		}
		colNames += col
		placeholders += p.Placeholder(placeholderIndex)
		placeholderIndex++
	}

	// Construir updates com placeholders
	updates := ""
	updatePlaceholderIndex := placeholderIndex
	for i, col := range updateCols {
		if i > 0 {
			updates += ", "
		}
		updates += fmt.Sprintf("%s = %s", col, p.Placeholder(updatePlaceholderIndex))
		// Adicionar valores para update
		values = append(values, values[len(conflictCols)+i])
		updatePlaceholderIndex++
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

// CreateTables cria as tabelas necessárias para PostgreSQL
func (p *PostgresDatabase) CreateTables() error {
	// Verificar se deve pular criação de tabelas
	if os.Getenv("DB_SKIP_TABLE_CREATION") == "true" {
		log.Println("Skipping table creation (DB_SKIP_TABLE_CREATION=true)")
		return nil
	}

	log.Println("Creating PostgreSQL tables if not exists...")

	createTableSQL := `CREATE TABLE IF NOT EXISTS users (
		id TEXT PRIMARY KEY,
		balance INTEGER DEFAULT 0,
		last_daily TIMESTAMP,
		webhook_url TEXT
	);`
	if _, err := p.db.Exec(createTableSQL); err != nil {
		log.Printf("Warning: error creating users table (may already exist): %v", err)
	}

	createApiTableSQL := `CREATE TABLE IF NOT EXISTS api_keys (
		key TEXT PRIMARY KEY,
		user_id TEXT NOT NULL,
		name TEXT,
		created_at TIMESTAMP
	);`
	if _, err := p.db.Exec(createApiTableSQL); err != nil {
		log.Printf("Warning: error creating api_keys table (may already exist): %v", err)
	}

	createStockInvestmentsSQL := `CREATE TABLE IF NOT EXISTS stock_investments (
		user_id TEXT NOT NULL,
		ticker TEXT NOT NULL,
		shares REAL DEFAULT 0,
		PRIMARY KEY (user_id, ticker)
	);`
	if _, err := p.db.Exec(createStockInvestmentsSQL); err != nil {
		log.Printf("Warning: error creating stock_investments table (may already exist): %v", err)
	}

	createStockPricesSQL := `CREATE TABLE IF NOT EXISTS stock_prices (
		ticker TEXT PRIMARY KEY,
		last_price REAL DEFAULT 0,
		updated_at TIMESTAMP
	);`
	if _, err := p.db.Exec(createStockPricesSQL); err != nil {
		log.Printf("Warning: error creating stock_prices table (may already exist): %v", err)
	}

	// Criar tabelas de crypto
	if err := p.CreateCryptoTables(); err != nil {
		log.Printf("Warning: error creating crypto tables: %v", err)
	}

	log.Println("Table creation completed")
	return nil
}

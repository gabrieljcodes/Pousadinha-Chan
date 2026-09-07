package database

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// PostgresDatabase implements the Database interface for PostgreSQL using pgx (recommended by Supabase)
type PostgresDatabase struct {
	connString string
	db         *sql.DB
}

// NewPostgresDatabase creates a new instance of the PostgreSQL database
func NewPostgresDatabase(connString string) *PostgresDatabase {
	return &PostgresDatabase{
		connString: connString,
	}
}

// Open opens the connection to the database
func (p *PostgresDatabase) Open() error {
	log.Printf("Connecting to PostgreSQL using pgx driver...")
	log.Printf("Connection string (masked): %s", maskPassword(p.connString))

	// Use pgx driver instead of pq - better support for Supabase pooler
	db, err := sql.Open("pgx", p.connString)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool optimized for Supabase pooler / PgBouncer
	db.SetMaxOpenConns(15)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)
	db.SetConnMaxIdleTime(2 * time.Minute)

	p.db = db
	return nil
}

// maskPassword hides the password in the connection string for logs
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

// Close closes the database connection
func (p *PostgresDatabase) Close() error {
	if p.db != nil {
		return p.db.Close()
	}
	return nil
}

// Ping checks if the database connection is active
func (p *PostgresDatabase) Ping() error {
	if p.db == nil {
		return fmt.Errorf("database not connected")
	}
	return p.db.Ping()
}

// GetDB returns the underlying *sql.DB instance
func (p *PostgresDatabase) GetDB() *sql.DB {
	return p.db
}

// Query executes a SELECT query
func (p *PostgresDatabase) Query(query string, args ...interface{}) (*sql.Rows, error) {
	return p.db.Query(query, args...)
}

// QueryRow executes a query returning a single row
func (p *PostgresDatabase) QueryRow(query string, args ...interface{}) *sql.Row {
	return p.db.QueryRow(query, args...)
}

// Exec executes a query without returning rows
func (p *PostgresDatabase) Exec(query string, args ...interface{}) (sql.Result, error) {
	return p.db.Exec(query, args...)
}

// Begin begins a transaction
func (p *PostgresDatabase) Begin() (*sql.Tx, error) {
	return p.db.Begin()
}

// Placeholder returns $N for PostgreSQL (1-indexed)
func (p *PostgresDatabase) Placeholder(index int) string {
	return fmt.Sprintf("$%d", index)
}

// UpsertSyntax returns the upsert syntax for PostgreSQL (INSERT ON CONFLICT)
func (p *PostgresDatabase) UpsertSyntax(table string, conflictCols []string, updateCols []string, values []interface{}) (string, []interface{}) {
	// Build column names
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

	// Build update clauses with placeholders
	updates := ""
	updatePlaceholderIndex := placeholderIndex
	for i, col := range updateCols {
		if i > 0 {
			updates += ", "
		}
		updates += fmt.Sprintf("%s = %s", col, p.Placeholder(updatePlaceholderIndex))
		// Append values for update
		values = append(values, values[len(conflictCols)+i])
		updatePlaceholderIndex++
	}

	// Build conflict target
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

// CreateTables creates required PostgreSQL tables and applies necessary schema migrations
func (p *PostgresDatabase) CreateTables() error {
	log.Println("Creating PostgreSQL tables and verifying schema migrations...")

	// 1. Base table creation (optimized types for fresh setups)
	createTableQueries := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			balance BIGINT DEFAULT 0,
			last_daily TIMESTAMPTZ,
			webhook_url TEXT,
			daily_streak INTEGER DEFAULT 0,
			max_daily_streak INTEGER DEFAULT 0
		);`,
		`CREATE TABLE IF NOT EXISTS api_keys (
			key TEXT PRIMARY KEY,
			key_prefix TEXT,
			user_id TEXT NOT NULL,
			name TEXT,
			created_at TIMESTAMPTZ DEFAULT NOW()
		);`,
		`CREATE TABLE IF NOT EXISTS stock_prices (
			ticker TEXT PRIMARY KEY,
			last_price NUMERIC(20, 6) DEFAULT 0,
			updated_at TIMESTAMPTZ DEFAULT NOW()
		);`,
		`CREATE TABLE IF NOT EXISTS crypto_prices (
			symbol TEXT PRIMARY KEY,
			last_price NUMERIC(28, 6) DEFAULT 0,
			updated_at TIMESTAMPTZ DEFAULT NOW()
		);`,
		`CREATE TABLE IF NOT EXISTS stock_investments (
			user_id TEXT NOT NULL,
			ticker TEXT NOT NULL,
			shares NUMERIC(28, 12) DEFAULT 0,
			PRIMARY KEY (user_id, ticker)
		);`,
		`CREATE TABLE IF NOT EXISTS loans (
			id TEXT PRIMARY KEY,
			lender_id TEXT NOT NULL,
			borrower_id TEXT NOT NULL,
			amount BIGINT DEFAULT 0,
			interest_rate NUMERIC(8, 4) DEFAULT 0,
			due_date TIMESTAMPTZ,
			total_owed BIGINT DEFAULT 0,
			paid BOOLEAN DEFAULT FALSE,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			channel_id TEXT,
			guild_id TEXT
		);`,
		`CREATE TABLE IF NOT EXISTS betting_events (
			id TEXT PRIMARY KEY,
			guild_id TEXT NOT NULL,
			channel_id TEXT NOT NULL,
			message_id TEXT,
			creator_id TEXT NOT NULL,
			question TEXT NOT NULL,
			options JSONB NOT NULL,
			total_pool BIGINT DEFAULT 0,
			status TEXT DEFAULT 'open',
			winner_id TEXT,
			end_time TIMESTAMPTZ NOT NULL,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			resolved_at TIMESTAMPTZ
		);`,
		`CREATE TABLE IF NOT EXISTS event_bets (
			id BIGSERIAL PRIMARY KEY,
			event_id TEXT NOT NULL,
			user_id TEXT NOT NULL,
			username TEXT NOT NULL,
			option_id TEXT NOT NULL,
			amount BIGINT NOT NULL,
			created_at TIMESTAMPTZ DEFAULT NOW()
		);`,
	}

	for _, query := range createTableQueries {
		if _, err := p.db.Exec(query); err != nil {
			log.Printf("Warning: error creating table: %v", err)
		}
	}

	// Create crypto tables
	if err := p.CreateCryptoTables(); err != nil {
		log.Printf("Warning: error creating crypto tables: %v", err)
	}

	// 2. Safe Schema Type Migrations (for existing databases)
	alterQueries := []string{
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS daily_streak INTEGER DEFAULT 0;`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS max_daily_streak INTEGER DEFAULT 0;`,
		`ALTER TABLE users ALTER COLUMN balance TYPE BIGINT;`,
		`ALTER TABLE users ALTER COLUMN last_daily TYPE TIMESTAMPTZ;`,
		`ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS key_prefix TEXT;`,
		`ALTER TABLE api_keys ALTER COLUMN created_at TYPE TIMESTAMPTZ;`,
		`ALTER TABLE stock_prices ALTER COLUMN last_price TYPE NUMERIC(20, 6);`,
		`ALTER TABLE stock_prices ALTER COLUMN updated_at TYPE TIMESTAMPTZ;`,
		`ALTER TABLE stock_investments ALTER COLUMN shares TYPE NUMERIC(28, 12);`,
		`ALTER TABLE stock_investments ADD COLUMN IF NOT EXISTS total_invested NUMERIC(28, 4) DEFAULT 0;`,
		`ALTER TABLE crypto_investments ALTER COLUMN coins TYPE NUMERIC(28, 12);`,
		`ALTER TABLE crypto_investments ADD COLUMN IF NOT EXISTS total_invested NUMERIC(28, 4) DEFAULT 0;`,
		`ALTER TABLE loans ALTER COLUMN amount TYPE BIGINT;`,
		`ALTER TABLE loans ALTER COLUMN total_owed TYPE BIGINT;`,
		`ALTER TABLE loans ALTER COLUMN interest_rate TYPE NUMERIC(8, 4);`,
		`ALTER TABLE loans ALTER COLUMN due_date TYPE TIMESTAMPTZ;`,
		`ALTER TABLE loans ALTER COLUMN created_at TYPE TIMESTAMPTZ;`,
	}

	for _, query := range alterQueries {
		if _, err := p.db.Exec(query); err != nil {
			log.Printf("Notice: schema migration step: %v", err)
		}
	}

	// 3. Heal orphaned records before adding foreign keys
	healQueries := []string{
		`INSERT INTO users (id, balance) SELECT DISTINCT user_id, 0 FROM api_keys WHERE user_id NOT IN (SELECT id FROM users) ON CONFLICT (id) DO NOTHING;`,
		`INSERT INTO users (id, balance) SELECT DISTINCT user_id, 0 FROM stock_investments WHERE user_id NOT IN (SELECT id FROM users) ON CONFLICT (id) DO NOTHING;`,
		`INSERT INTO users (id, balance) SELECT DISTINCT user_id, 0 FROM crypto_investments WHERE user_id NOT IN (SELECT id FROM users) ON CONFLICT (id) DO NOTHING;`,
		`INSERT INTO users (id, balance) SELECT DISTINCT lender_id, 0 FROM loans WHERE lender_id NOT IN (SELECT id FROM users) ON CONFLICT (id) DO NOTHING;`,
		`INSERT INTO users (id, balance) SELECT DISTINCT borrower_id, 0 FROM loans WHERE borrower_id NOT IN (SELECT id FROM users) ON CONFLICT (id) DO NOTHING;`,
		`INSERT INTO users (id, balance) SELECT DISTINCT creator_id, 0 FROM betting_events WHERE creator_id NOT IN (SELECT id FROM users) ON CONFLICT (id) DO NOTHING;`,
		`INSERT INTO users (id, balance) SELECT DISTINCT user_id, 0 FROM event_bets WHERE user_id NOT IN (SELECT id FROM users) ON CONFLICT (id) DO NOTHING;`,
	}
	for _, query := range healQueries {
		_, _ = p.db.Exec(query)
	}

	// 4. Foreign Key Constraints (ON DELETE CASCADE)
	fkBlock := `
	DO $$
	BEGIN
		IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_api_keys_user') THEN
			ALTER TABLE api_keys ADD CONSTRAINT fk_api_keys_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;
		END IF;
		IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_stock_investments_user') THEN
			ALTER TABLE stock_investments ADD CONSTRAINT fk_stock_investments_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;
		END IF;
		IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_crypto_investments_user') THEN
			ALTER TABLE crypto_investments ADD CONSTRAINT fk_crypto_investments_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;
		END IF;
		IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_loans_lender') THEN
			ALTER TABLE loans ADD CONSTRAINT fk_loans_lender FOREIGN KEY (lender_id) REFERENCES users(id) ON DELETE CASCADE;
		END IF;
		IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_loans_borrower') THEN
			ALTER TABLE loans ADD CONSTRAINT fk_loans_borrower FOREIGN KEY (borrower_id) REFERENCES users(id) ON DELETE CASCADE;
		END IF;
		IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_betting_events_creator') THEN
			ALTER TABLE betting_events ADD CONSTRAINT fk_betting_events_creator FOREIGN KEY (creator_id) REFERENCES users(id) ON DELETE CASCADE;
		END IF;
		IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_event_bets_event') THEN
			ALTER TABLE event_bets ADD CONSTRAINT fk_event_bets_event FOREIGN KEY (event_id) REFERENCES betting_events(id) ON DELETE CASCADE;
		END IF;
		IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_event_bets_user') THEN
			ALTER TABLE event_bets ADD CONSTRAINT fk_event_bets_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;
		END IF;
	END $$;`
	if _, err := p.db.Exec(fkBlock); err != nil {
		log.Printf("Notice: foreign key configuration: %v", err)
	}

	// 5. Check Constraints
	chkBlock := `
	DO $$
	BEGIN
		IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_users_balance') THEN
			ALTER TABLE users ADD CONSTRAINT chk_users_balance CHECK (balance >= 0);
		END IF;
		IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_stock_prices_last_price') THEN
			ALTER TABLE stock_prices ADD CONSTRAINT chk_stock_prices_last_price CHECK (last_price >= 0);
		END IF;
		IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_stock_investments_shares') THEN
			ALTER TABLE stock_investments ADD CONSTRAINT chk_stock_investments_shares CHECK (shares >= 0);
		END IF;
		IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_stock_investments_total_invested') THEN
			ALTER TABLE stock_investments ADD CONSTRAINT chk_stock_investments_total_invested CHECK (total_invested >= 0);
		END IF;
		IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_crypto_investments_coins') THEN
			ALTER TABLE crypto_investments ADD CONSTRAINT chk_crypto_investments_coins CHECK (coins >= 0);
		END IF;
		IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_crypto_investments_total_invested') THEN
			ALTER TABLE crypto_investments ADD CONSTRAINT chk_crypto_investments_total_invested CHECK (total_invested >= 0);
		END IF;
		IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_loans_total_owed') THEN
			ALTER TABLE loans ADD CONSTRAINT chk_loans_total_owed CHECK (total_owed >= 0);
		END IF;
		IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_betting_events_total_pool') THEN
			ALTER TABLE betting_events ADD CONSTRAINT chk_betting_events_total_pool CHECK (total_pool >= 0);
		END IF;
		IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_betting_events_status') THEN
			ALTER TABLE betting_events ADD CONSTRAINT chk_betting_events_status CHECK (status IN ('open', 'closed', 'resolved', 'cancelled'));
		END IF;
		IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_event_bets_amount') THEN
			ALTER TABLE event_bets ADD CONSTRAINT chk_event_bets_amount CHECK (amount > 0);
		END IF;
		IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'uq_event_bets_user') THEN
			ALTER TABLE event_bets ADD CONSTRAINT uq_event_bets_user UNIQUE (event_id, user_id);
		END IF;
	END $$;`
	if _, err := p.db.Exec(chkBlock); err != nil {
		log.Printf("Notice: check constraint configuration: %v", err)
	}

	// 6. Performance & Partial Indexes
	indexQueries := []string{
		`CREATE INDEX IF NOT EXISTS idx_users_balance ON users (balance DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_users_daily_streak ON users (daily_streak DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_api_keys_user_id ON api_keys (user_id);`,
		`CREATE INDEX IF NOT EXISTS idx_stock_investments_ticker ON stock_investments (ticker);`,
		`CREATE INDEX IF NOT EXISTS idx_crypto_investments_symbol ON crypto_investments (symbol);`,
		`CREATE INDEX IF NOT EXISTS idx_loans_due_date_unpaid ON loans (due_date) WHERE paid = FALSE;`,
		`CREATE INDEX IF NOT EXISTS idx_loans_borrower_unpaid ON loans (borrower_id) WHERE paid = FALSE;`,
		`CREATE INDEX IF NOT EXISTS idx_loans_lender_id ON loans (lender_id);`,
		`CREATE INDEX IF NOT EXISTS idx_betting_events_active ON betting_events (end_time) WHERE status IN ('open', 'closed');`,
		`CREATE INDEX IF NOT EXISTS idx_betting_events_guild ON betting_events (guild_id);`,
		`CREATE INDEX IF NOT EXISTS idx_event_bets_event_id ON event_bets (event_id);`,
		`CREATE INDEX IF NOT EXISTS idx_event_bets_user_id ON event_bets (user_id);`,
	}
	for _, query := range indexQueries {
		if _, err := p.db.Exec(query); err != nil {
			log.Printf("Notice: index creation: %v", err)
		}
	}

	// 7. Security: Enable Row Level Security (RLS) on all tables & set policies
	rlsQueries := []string{
		`ALTER TABLE users ENABLE ROW LEVEL SECURITY;`,
		`ALTER TABLE api_keys ENABLE ROW LEVEL SECURITY;`,
		`ALTER TABLE stock_prices ENABLE ROW LEVEL SECURITY;`,
		`ALTER TABLE stock_investments ENABLE ROW LEVEL SECURITY;`,
		`ALTER TABLE crypto_prices ENABLE ROW LEVEL SECURITY;`,
		`ALTER TABLE crypto_investments ENABLE ROW LEVEL SECURITY;`,
		`ALTER TABLE loans ENABLE ROW LEVEL SECURITY;`,
		`ALTER TABLE betting_events ENABLE ROW LEVEL SECURITY;`,
		`ALTER TABLE event_bets ENABLE ROW LEVEL SECURITY;`,
	}
	for _, query := range rlsQueries {
		if _, err := p.db.Exec(query); err != nil {
			log.Printf("Notice: RLS configuration: %v", err)
		}
	}

	rlsPolicyBlock := `
	DO $$
	BEGIN
		IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE tablename = 'stock_prices' AND policyname = 'allow_public_read_stock_prices') THEN
			CREATE POLICY allow_public_read_stock_prices ON stock_prices FOR SELECT TO anon, authenticated USING (true);
		END IF;
		IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE tablename = 'betting_events' AND policyname = 'allow_public_read_betting_events') THEN
			CREATE POLICY allow_public_read_betting_events ON betting_events FOR SELECT TO anon, authenticated USING (true);
		END IF;
		IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE tablename = 'event_bets' AND policyname = 'allow_public_read_event_bets') THEN
			CREATE POLICY allow_public_read_event_bets ON event_bets FOR SELECT TO anon, authenticated USING (true);
		END IF;
		IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE tablename = 'users' AND policyname = 'service_role_all_users') THEN
			CREATE POLICY service_role_all_users ON users FOR ALL TO service_role USING (true) WITH CHECK (true);
		END IF;
		IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE tablename = 'api_keys' AND policyname = 'service_role_all_api_keys') THEN
			CREATE POLICY service_role_all_api_keys ON api_keys FOR ALL TO service_role USING (true) WITH CHECK (true);
		END IF;
		IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE tablename = 'stock_investments' AND policyname = 'service_role_all_stock_investments') THEN
			CREATE POLICY service_role_all_stock_investments ON stock_investments FOR ALL TO service_role USING (true) WITH CHECK (true);
		END IF;
		IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE tablename = 'stock_prices' AND policyname = 'service_role_all_stock_prices') THEN
			CREATE POLICY service_role_all_stock_prices ON stock_prices FOR ALL TO service_role USING (true) WITH CHECK (true);
		END IF;
		IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE tablename = 'crypto_investments' AND policyname = 'service_role_all_crypto_investments') THEN
			CREATE POLICY service_role_all_crypto_investments ON crypto_investments FOR ALL TO service_role USING (true) WITH CHECK (true);
		END IF;
		IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE tablename = 'crypto_prices' AND policyname = 'allow_public_read_crypto_prices') THEN
			CREATE POLICY allow_public_read_crypto_prices ON crypto_prices FOR SELECT TO anon, authenticated USING (true);
		END IF;
		IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE tablename = 'crypto_prices' AND policyname = 'service_role_all_crypto_prices') THEN
			CREATE POLICY service_role_all_crypto_prices ON crypto_prices FOR ALL TO service_role USING (true) WITH CHECK (true);
		END IF;
		IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE tablename = 'loans' AND policyname = 'service_role_all_loans') THEN
			CREATE POLICY service_role_all_loans ON loans FOR ALL TO service_role USING (true) WITH CHECK (true);
		END IF;
		IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE tablename = 'betting_events' AND policyname = 'service_role_all_betting_events') THEN
			CREATE POLICY service_role_all_betting_events ON betting_events FOR ALL TO service_role USING (true) WITH CHECK (true);
		END IF;
		IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE tablename = 'event_bets' AND policyname = 'service_role_all_event_bets') THEN
			CREATE POLICY service_role_all_event_bets ON event_bets FOR ALL TO service_role USING (true) WITH CHECK (true);
		END IF;
	END $$;`
	if _, err := p.db.Exec(rlsPolicyBlock); err != nil {
		log.Printf("Notice: RLS policy configuration: %v", err)
	}

	log.Println("Table creation, migrations, indexing, and security policies completed successfully")
	return nil
}

-- Migration: 001_schema_optimization.sql
-- Purpose: Optimize PostgreSQL schema according to Supabase Best Practices
-- Author: Antigravity Pair Programmer
-- Date: 2026-09-07

BEGIN;

-- ============================================================================
-- 1. USERS TABLE OPTIMIZATIONS
-- ============================================================================
CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    balance BIGINT DEFAULT 0,
    last_daily TIMESTAMPTZ,
    webhook_url TEXT,
    daily_streak INTEGER DEFAULT 0,
    max_daily_streak INTEGER DEFAULT 0
);

-- Upgrade column types if they exist with older types
ALTER TABLE users 
    ALTER COLUMN balance TYPE BIGINT,
    ALTER COLUMN last_daily TYPE TIMESTAMPTZ USING (
        CASE 
            WHEN last_daily IS NOT NULL THEN last_daily AT TIME ZONE 'UTC'
            ELSE NULL 
        END
    );

-- Add CHECK constraints if not present
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_users_balance') THEN
        ALTER TABLE users ADD CONSTRAINT chk_users_balance CHECK (balance >= 0);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_users_daily_streak') THEN
        ALTER TABLE users ADD CONSTRAINT chk_users_daily_streak CHECK (daily_streak >= 0);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_users_max_daily_streak') THEN
        ALTER TABLE users ADD CONSTRAINT chk_users_max_daily_streak CHECK (max_daily_streak >= 0);
    END IF;
END $$;


-- ============================================================================
-- 2. API KEYS TABLE OPTIMIZATIONS
-- ============================================================================
CREATE TABLE IF NOT EXISTS api_keys (
    key TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    name TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Upgrade created_at to TIMESTAMPTZ
ALTER TABLE api_keys 
    ALTER COLUMN created_at TYPE TIMESTAMPTZ USING (
        CASE 
            WHEN created_at IS NOT NULL THEN created_at AT TIME ZONE 'UTC'
            ELSE NOW()
        END
    );

-- Ensure parent user records exist before adding foreign key
INSERT INTO users (id, balance)
SELECT DISTINCT user_id, 0 
FROM api_keys 
WHERE user_id NOT IN (SELECT id FROM users)
ON CONFLICT (id) DO NOTHING;

-- Add Foreign Key with CASCADE deletion
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_api_keys_user') THEN
        ALTER TABLE api_keys 
            ADD CONSTRAINT fk_api_keys_user 
            FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;
    END IF;
END $$;

-- Index foreign key lookup
CREATE INDEX IF NOT EXISTS idx_api_keys_user_id ON api_keys (user_id);


-- ============================================================================
-- 3. STOCK PRICES TABLE OPTIMIZATIONS
-- ============================================================================
CREATE TABLE IF NOT EXISTS stock_prices (
    ticker TEXT PRIMARY KEY,
    last_price NUMERIC(20, 6) DEFAULT 0,
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Upgrade types to NUMERIC and TIMESTAMPTZ for precision
ALTER TABLE stock_prices 
    ALTER COLUMN last_price TYPE NUMERIC(20, 6),
    ALTER COLUMN updated_at TYPE TIMESTAMPTZ USING (
        CASE 
            WHEN updated_at IS NOT NULL THEN updated_at AT TIME ZONE 'UTC'
            ELSE NOW()
        END
    );

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_stock_prices_last_price') THEN
        ALTER TABLE stock_prices ADD CONSTRAINT chk_stock_prices_last_price CHECK (last_price >= 0);
    END IF;
END $$;


-- ============================================================================
-- 4. STOCK INVESTMENTS TABLE OPTIMIZATIONS
-- ============================================================================
CREATE TABLE IF NOT EXISTS stock_investments (
    user_id TEXT NOT NULL,
    ticker TEXT NOT NULL,
    shares NUMERIC(28, 12) DEFAULT 0,
    PRIMARY KEY (user_id, ticker)
);

-- Upgrade shares from REAL to NUMERIC for fractional accuracy
ALTER TABLE stock_investments 
    ALTER COLUMN shares TYPE NUMERIC(28, 12);

-- Ensure parent user records exist
INSERT INTO users (id, balance)
SELECT DISTINCT user_id, 0 
FROM stock_investments 
WHERE user_id NOT IN (SELECT id FROM users)
ON CONFLICT (id) DO NOTHING;

-- Add Foreign Key with CASCADE
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_stock_investments_user') THEN
        ALTER TABLE stock_investments 
            ADD CONSTRAINT fk_stock_investments_user 
            FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_stock_investments_shares') THEN
        ALTER TABLE stock_investments ADD CONSTRAINT chk_stock_investments_shares CHECK (shares >= 0);
    END IF;
END $$;

-- Index ticker for market aggregations
CREATE INDEX IF NOT EXISTS idx_stock_investments_ticker ON stock_investments (ticker);


-- ============================================================================
-- 5. CRYPTO INVESTMENTS TABLE OPTIMIZATIONS
-- ============================================================================
CREATE TABLE IF NOT EXISTS crypto_investments (
    user_id TEXT NOT NULL,
    symbol TEXT NOT NULL,
    coins NUMERIC(28, 12) DEFAULT 0,
    PRIMARY KEY (user_id, symbol)
);

-- Upgrade coins from REAL to NUMERIC(28, 12) to avoid floating-point loss on satoshis/wei
ALTER TABLE crypto_investments 
    ALTER COLUMN coins TYPE NUMERIC(28, 12);

-- Ensure parent user records exist
INSERT INTO users (id, balance)
SELECT DISTINCT user_id, 0 
FROM crypto_investments 
WHERE user_id NOT IN (SELECT id FROM users)
ON CONFLICT (id) DO NOTHING;

-- Add Foreign Key and CHECK constraint
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_crypto_investments_user') THEN
        ALTER TABLE crypto_investments 
            ADD CONSTRAINT fk_crypto_investments_user 
            FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_crypto_investments_coins') THEN
        ALTER TABLE crypto_investments ADD CONSTRAINT chk_crypto_investments_coins CHECK (coins >= 0);
    END IF;
END $$;

-- Index symbol
CREATE INDEX IF NOT EXISTS idx_crypto_investments_symbol ON crypto_investments (symbol);


-- ============================================================================
-- 6. LOANS TABLE OPTIMIZATIONS
-- ============================================================================
CREATE TABLE IF NOT EXISTS loans (
    id TEXT PRIMARY KEY,
    lender_id TEXT NOT NULL,
    borrower_id TEXT NOT NULL,
    amount BIGINT DEFAULT 0,
    interest_rate NUMERIC(8, 4) DEFAULT 0,
    due_date TIMESTAMPTZ NOT NULL,
    total_owed BIGINT DEFAULT 0,
    paid BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    channel_id TEXT,
    guild_id TEXT
);

-- Upgrade column types
ALTER TABLE loans 
    ALTER COLUMN amount TYPE BIGINT,
    ALTER COLUMN total_owed TYPE BIGINT,
    ALTER COLUMN interest_rate TYPE NUMERIC(8, 4),
    ALTER COLUMN due_date TYPE TIMESTAMPTZ USING (
        CASE 
            WHEN due_date IS NOT NULL THEN due_date AT TIME ZONE 'UTC'
            ELSE NOW()
        END
    ),
    ALTER COLUMN created_at TYPE TIMESTAMPTZ USING (
        CASE 
            WHEN created_at IS NOT NULL THEN created_at AT TIME ZONE 'UTC'
            ELSE NOW()
        END
    );

-- Ensure parent user records exist
INSERT INTO users (id, balance)
SELECT DISTINCT lender_id, 0 FROM loans WHERE lender_id NOT IN (SELECT id FROM users)
UNION
SELECT DISTINCT borrower_id, 0 FROM loans WHERE borrower_id NOT IN (SELECT id FROM users)
ON CONFLICT (id) DO NOTHING;

-- Add Foreign Keys
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_loans_lender') THEN
        ALTER TABLE loans 
            ADD CONSTRAINT fk_loans_lender 
            FOREIGN KEY (lender_id) REFERENCES users(id) ON DELETE CASCADE;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_loans_borrower') THEN
        ALTER TABLE loans 
            ADD CONSTRAINT fk_loans_borrower 
            FOREIGN KEY (borrower_id) REFERENCES users(id) ON DELETE CASCADE;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_loans_amount') THEN
        ALTER TABLE loans ADD CONSTRAINT chk_loans_amount CHECK (amount > 0);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_loans_total_owed') THEN
        ALTER TABLE loans ADD CONSTRAINT chk_loans_total_owed CHECK (total_owed >= 0);
    END IF;
END $$;

-- Partial index for active (unpaid) loans query GetActiveLoans()
CREATE INDEX IF NOT EXISTS idx_loans_due_date_unpaid ON loans (due_date) WHERE paid = FALSE;
CREATE INDEX IF NOT EXISTS idx_loans_borrower_unpaid ON loans (borrower_id) WHERE paid = FALSE;
CREATE INDEX IF NOT EXISTS idx_loans_lender_id ON loans (lender_id);

COMMIT;

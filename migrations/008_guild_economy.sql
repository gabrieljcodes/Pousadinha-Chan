-- Guild-scoped economy schema. Never copy or reconcile legacy wallet balances.
-- Legacy investments without a guild remain unchanged until explicitly assigned
-- by an operator; an empty guild is never a real Discord server.

-- 1. Create guild_members table with composite PK and check constraints
CREATE TABLE IF NOT EXISTS guild_members (
    guild_id TEXT NOT NULL,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    balance BIGINT NOT NULL DEFAULT 0,
    last_daily TIMESTAMPTZ,
    daily_streak INTEGER NOT NULL DEFAULT 0,
    max_daily_streak INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT pk_guild_members PRIMARY KEY (guild_id, user_id),
    CONSTRAINT chk_guild_members_balance CHECK (balance >= 0),
    CONSTRAINT chk_guild_members_daily_streak CHECK (daily_streak >= 0),
    CONSTRAINT chk_guild_members_max_daily_streak CHECK (max_daily_streak >= 0)
);

-- 2. Composite indexes for fast queries and leaderboards
CREATE INDEX IF NOT EXISTS idx_guild_members_leaderboard ON guild_members (guild_id, balance DESC);
CREATE INDEX IF NOT EXISTS idx_guild_members_streak ON guild_members (guild_id, daily_streak DESC);
CREATE INDEX IF NOT EXISTS idx_guild_members_user ON guild_members (user_id);

-- 3. Guild scoping for stock investments
ALTER TABLE stock_investments ADD COLUMN IF NOT EXISTS guild_id TEXT NOT NULL DEFAULT '';
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'stock_investments'::regclass AND conname = 'stock_investments_pkey') THEN
        ALTER TABLE stock_investments DROP CONSTRAINT stock_investments_pkey;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'stock_investments'::regclass AND conname = 'pk_stock_investments') THEN
        ALTER TABLE stock_investments ADD CONSTRAINT pk_stock_investments PRIMARY KEY (guild_id, user_id, ticker);
    END IF;
END $$;
CREATE INDEX IF NOT EXISTS idx_stock_investments_guild_user ON stock_investments (guild_id, user_id);

-- 4. Guild scoping for crypto investments
ALTER TABLE crypto_investments ADD COLUMN IF NOT EXISTS guild_id TEXT NOT NULL DEFAULT '';
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'crypto_investments'::regclass AND conname = 'crypto_investments_pkey') THEN
        ALTER TABLE crypto_investments DROP CONSTRAINT crypto_investments_pkey;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'crypto_investments'::regclass AND conname = 'pk_crypto_investments') THEN
        ALTER TABLE crypto_investments ADD CONSTRAINT pk_crypto_investments PRIMARY KEY (guild_id, user_id, symbol);
    END IF;
END $$;
CREATE INDEX IF NOT EXISTS idx_crypto_investments_guild_user ON crypto_investments (guild_id, user_id);

-- 5. Row Level Security
ALTER TABLE guild_members ENABLE ROW LEVEL SECURITY;
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'service_role')
       AND NOT EXISTS (SELECT 1 FROM pg_policies WHERE schemaname = current_schema() AND tablename = 'guild_members' AND policyname = 'service_role_all_guild_members') THEN
        CREATE POLICY service_role_all_guild_members ON guild_members FOR ALL TO service_role USING (true) WITH CHECK (true);
    END IF;
END $$;

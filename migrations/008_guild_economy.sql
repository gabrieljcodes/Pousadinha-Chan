-- Migration: 008_guild_economy.sql
-- Purpose: Isolate economy 100% per Discord server (guild-scoped)
-- Compliant with Supabase Postgres Best Practices

BEGIN;

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

-- 3. Migrate existing user balances to guild_members for all known guilds
INSERT INTO guild_members (guild_id, user_id, balance, last_daily, daily_streak, max_daily_streak)
SELECT DISTINCT g.guild_id, u.id, u.balance, u.last_daily, u.daily_streak, u.max_daily_streak
FROM users u
JOIN (
    SELECT guild_id, user_id FROM gacha_players
    UNION
    SELECT guild_id, user_id FROM bicho_bets
    UNION
    SELECT guild_id, lender_id AS user_id FROM loans WHERE guild_id IS NOT NULL AND guild_id != ''
    UNION
    SELECT guild_id, borrower_id AS user_id FROM loans WHERE guild_id IS NOT NULL AND guild_id != ''
    UNION
    SELECT guild_id, user_id FROM event_bets eb JOIN betting_events e ON eb.event_id = e.id
    UNION
    SELECT guild_id, user_id FROM polymarket_orders po JOIN polymarket_markets pm ON po.market_id = pm.id
) g ON g.user_id = u.id
ON CONFLICT (guild_id, user_id) DO NOTHING;

-- Backfill any remaining users to the primary active guild so no balance is lost
INSERT INTO guild_members (guild_id, user_id, balance, last_daily, daily_streak, max_daily_streak)
SELECT 
    COALESCE(
        (SELECT guild_id FROM gacha_players LIMIT 1),
        (SELECT guild_id FROM loans WHERE guild_id IS NOT NULL AND guild_id != '' LIMIT 1),
        '1332552218699628605'
    ),
    u.id, u.balance, u.last_daily, u.daily_streak, u.max_daily_streak
FROM users u
WHERE NOT EXISTS (
    SELECT 1 FROM guild_members gm WHERE gm.user_id = u.id
)
ON CONFLICT (guild_id, user_id) DO NOTHING;

-- If migrating while an older bot instance is running, sync any higher user balances
UPDATE guild_members gm
SET balance = u.balance, last_daily = u.last_daily, daily_streak = u.daily_streak, max_daily_streak = u.max_daily_streak
FROM users u
WHERE gm.user_id = u.id AND gm.guild_id = '1332552218699628605' AND gm.balance < u.balance;

-- 4. Guild scoping for stock investments
ALTER TABLE stock_investments ADD COLUMN IF NOT EXISTS guild_id TEXT NOT NULL DEFAULT '';
UPDATE stock_investments SET guild_id = COALESCE((SELECT guild_id FROM gacha_players LIMIT 1), '1332552218699628605') WHERE guild_id = '';
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'stock_investments_pkey') THEN
        ALTER TABLE stock_investments DROP CONSTRAINT stock_investments_pkey;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'pk_stock_investments') THEN
        ALTER TABLE stock_investments ADD CONSTRAINT pk_stock_investments PRIMARY KEY (guild_id, user_id, ticker);
    END IF;
END $$;
CREATE INDEX IF NOT EXISTS idx_stock_investments_guild_user ON stock_investments (guild_id, user_id);

-- 5. Guild scoping for crypto investments
ALTER TABLE crypto_investments ADD COLUMN IF NOT EXISTS guild_id TEXT NOT NULL DEFAULT '';
UPDATE crypto_investments SET guild_id = COALESCE((SELECT guild_id FROM gacha_players LIMIT 1), '1332552218699628605') WHERE guild_id = '';
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'crypto_investments_pkey') THEN
        ALTER TABLE crypto_investments DROP CONSTRAINT crypto_investments_pkey;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'pk_crypto_investments') THEN
        ALTER TABLE crypto_investments ADD CONSTRAINT pk_crypto_investments PRIMARY KEY (guild_id, user_id, symbol);
    END IF;
END $$;
CREATE INDEX IF NOT EXISTS idx_crypto_investments_guild_user ON crypto_investments (guild_id, user_id);

-- 6. Row Level Security
ALTER TABLE guild_members ENABLE ROW LEVEL SECURITY;
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE tablename = 'guild_members' AND policyname = 'service_role_all_guild_members') THEN
        CREATE POLICY service_role_all_guild_members ON guild_members FOR ALL TO service_role USING (true) WITH CHECK (true);
    END IF;
END $$;

COMMIT;

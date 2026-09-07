-- Migration: 003_event_betting.sql
-- Description: Creates persistent betting_events and event_bets tables with constraints, indexes, and RLS.

-- 1. Create betting_events table
CREATE TABLE IF NOT EXISTS betting_events (
    id TEXT PRIMARY KEY,
    guild_id TEXT NOT NULL,
    channel_id TEXT NOT NULL,
    message_id TEXT,
    creator_id TEXT NOT NULL,
    question TEXT NOT NULL,
    options JSONB NOT NULL,
    total_pool BIGINT NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'open',
    winner_id TEXT,
    end_time TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    resolved_at TIMESTAMPTZ
);

-- 2. Create event_bets table
CREATE TABLE IF NOT EXISTS event_bets (
    id BIGSERIAL PRIMARY KEY,
    event_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    username TEXT NOT NULL,
    option_id TEXT NOT NULL,
    amount BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 3. Heal any orphaned references prior to constraint enforcement
INSERT INTO users (id, balance)
SELECT DISTINCT creator_id, 0 FROM betting_events
WHERE creator_id NOT IN (SELECT id FROM users)
ON CONFLICT (id) DO NOTHING;

INSERT INTO users (id, balance)
SELECT DISTINCT user_id, 0 FROM event_bets
WHERE user_id NOT IN (SELECT id FROM users)
ON CONFLICT (id) DO NOTHING;

-- 4. Foreign Key Constraints (ON DELETE CASCADE)
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_betting_events_creator') THEN
        ALTER TABLE betting_events 
            ADD CONSTRAINT fk_betting_events_creator 
            FOREIGN KEY (creator_id) REFERENCES users(id) ON DELETE CASCADE;
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_event_bets_event') THEN
        ALTER TABLE event_bets 
            ADD CONSTRAINT fk_event_bets_event 
            FOREIGN KEY (event_id) REFERENCES betting_events(id) ON DELETE CASCADE;
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_event_bets_user') THEN
        ALTER TABLE event_bets 
            ADD CONSTRAINT fk_event_bets_user 
            FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;
    END IF;
END $$;

-- 5. Check Constraints & Unique Constraints
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_betting_events_total_pool') THEN
        ALTER TABLE betting_events ADD CONSTRAINT chk_betting_events_total_pool CHECK (total_pool >= 0);
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_betting_events_status') THEN
        ALTER TABLE betting_events ADD CONSTRAINT chk_betting_events_status CHECK (status IN ('open', 'closed', 'resolved', 'cancelled'));
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_event_bets_amount') THEN
        ALTER TABLE event_bets ADD CONSTRAINT chk_event_bets_amount CHECK (amount > 0);
    END IF;

    -- Guarantee only one bet per user per event at the database level
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'uq_event_bets_user') THEN
        ALTER TABLE event_bets ADD CONSTRAINT uq_event_bets_user UNIQUE (event_id, user_id);
    END IF;
END $$;

-- 6. Performance & Partial Indexes
CREATE INDEX IF NOT EXISTS idx_betting_events_active ON betting_events (end_time) WHERE status IN ('open', 'closed');
CREATE INDEX IF NOT EXISTS idx_betting_events_guild ON betting_events (guild_id);
CREATE INDEX IF NOT EXISTS idx_event_bets_event_id ON event_bets (event_id);
CREATE INDEX IF NOT EXISTS idx_event_bets_user_id ON event_bets (user_id);

-- 7. Security: Enable Row Level Security (RLS) & Policies
ALTER TABLE betting_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE event_bets ENABLE ROW LEVEL SECURITY;

DO $$
BEGIN
    -- Public read policy for betting events
    IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE tablename = 'betting_events' AND policyname = 'allow_public_read_betting_events') THEN
        CREATE POLICY allow_public_read_betting_events ON betting_events FOR SELECT TO anon, authenticated USING (true);
    END IF;

    -- Service role full access for betting events
    IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE tablename = 'betting_events' AND policyname = 'service_role_all_betting_events') THEN
        CREATE POLICY service_role_all_betting_events ON betting_events FOR ALL TO service_role USING (true) WITH CHECK (true);
    END IF;

    -- Public read policy for event bets
    IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE tablename = 'event_bets' AND policyname = 'allow_public_read_event_bets') THEN
        CREATE POLICY allow_public_read_event_bets ON event_bets FOR SELECT TO anon, authenticated USING (true);
    END IF;

    -- Service role full access for event bets
    IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE tablename = 'event_bets' AND policyname = 'service_role_all_event_bets') THEN
        CREATE POLICY service_role_all_event_bets ON event_bets FOR ALL TO service_role USING (true) WITH CHECK (true);
    END IF;
END $$;

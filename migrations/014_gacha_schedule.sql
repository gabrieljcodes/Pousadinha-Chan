CREATE TABLE IF NOT EXISTS gacha_guild_settings (
    guild_id TEXT PRIMARY KEY,
    reset_minute SMALLINT NOT NULL DEFAULT 0 CHECK (reset_minute >= 0 AND reset_minute <= 59),
    rolls_per_hour INT NOT NULL DEFAULT 10 CHECK (rolls_per_hour >= 1 AND rolls_per_hour <= 100),
    claim_hours INT NOT NULL DEFAULT 3 CHECK (claim_hours >= 1 AND claim_hours <= 24),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
ALTER TABLE gacha_guild_settings ENABLE ROW LEVEL SECURITY;

ALTER TABLE gacha_rolls ADD COLUMN IF NOT EXISTS gem_type TEXT NOT NULL DEFAULT '';
ALTER TABLE gacha_rolls ADD COLUMN IF NOT EXISTS gem_value INT NOT NULL DEFAULT 0;
ALTER TABLE gacha_rolls ADD COLUMN IF NOT EXISTS gem_power_cost INT NOT NULL DEFAULT 25;
ALTER TABLE gacha_rolls ADD COLUMN IF NOT EXISTS gem_claimed_by TEXT REFERENCES users(id);
ALTER TABLE gacha_rolls ADD COLUMN IF NOT EXISTS gem_claimed_at TIMESTAMPTZ;

ALTER TABLE gacha_players ADD COLUMN IF NOT EXISTS gem_power INT NOT NULL DEFAULT 100 CHECK (gem_power >= 0 AND gem_power <= 100);

CREATE INDEX IF NOT EXISTS idx_gacha_rolls_unclaimed_gems ON gacha_rolls(id) WHERE gem_type != '' AND gem_claimed_by IS NULL;

-- 019_gacha_shop.sql: Gacha shop, inventory, lootbox, and player perk boosts
ALTER TABLE gacha_players ADD COLUMN IF NOT EXISTS extra_permanent_rolls INT NOT NULL DEFAULT 0 CHECK (extra_permanent_rolls >= 0);
ALTER TABLE gacha_players ADD COLUMN IF NOT EXISTS extra_wish_slots INT NOT NULL DEFAULT 0 CHECK (extra_wish_slots >= 0);
ALTER TABLE gacha_players ADD COLUMN IF NOT EXISTS permanent_wish_bonus NUMERIC NOT NULL DEFAULT 0 CHECK (permanent_wish_bonus >= 0);
ALTER TABLE gacha_players ADD COLUMN IF NOT EXISTS stored_extra_rolls INT NOT NULL DEFAULT 0 CHECK (stored_extra_rolls >= 0);
ALTER TABLE gacha_players ADD COLUMN IF NOT EXISTS wish_flare_rolls INT NOT NULL DEFAULT 0 CHECK (wish_flare_rolls >= 0);
ALTER TABLE gacha_players ADD COLUMN IF NOT EXISTS snipe_shield_until TIMESTAMPTZ;
ALTER TABLE gacha_rolls ADD COLUMN IF NOT EXISTS wish_spawn BOOLEAN NOT NULL DEFAULT FALSE;

CREATE TABLE IF NOT EXISTS gacha_inventory (
    guild_id TEXT NOT NULL,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    item_id TEXT NOT NULL,
    quantity INT NOT NULL DEFAULT 0 CHECK (quantity >= 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (guild_id, user_id, item_id)
);

CREATE INDEX IF NOT EXISTS idx_gacha_inventory_active ON gacha_inventory (guild_id, user_id) WHERE quantity > 0;

CREATE TABLE IF NOT EXISTS gacha_shop_logs (
    id BIGSERIAL PRIMARY KEY,
    guild_id TEXT NOT NULL,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    action TEXT NOT NULL CHECK (action IN ('buy', 'use', 'lootbox_open')),
    item_id TEXT NOT NULL,
    cost BIGINT NOT NULL DEFAULT 0,
    reward_type TEXT NOT NULL DEFAULT '',
    reward_value TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_gacha_shop_logs_user ON gacha_shop_logs (guild_id, user_id, created_at DESC);

ALTER TABLE gacha_inventory ENABLE ROW LEVEL SECURITY;
ALTER TABLE gacha_shop_logs ENABLE ROW LEVEL SECURITY;

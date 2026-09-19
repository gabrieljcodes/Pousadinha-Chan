-- 020_gacha_custom_images.sql: Custom user photo/GIF submissions, quota, and auto-approve
ALTER TABLE gacha_assets ADD COLUMN IF NOT EXISTS submitted_by TEXT;
ALTER TABLE gacha_assets ADD COLUMN IF NOT EXISTS guild_id TEXT;

CREATE INDEX IF NOT EXISTS gacha_assets_submitted_by ON gacha_assets(submitted_by) WHERE provider = 'user_custom' AND archived_at IS NULL;

ALTER TABLE gacha_guild_settings ADD COLUMN IF NOT EXISTS custom_image_auto_approve BOOLEAN NOT NULL DEFAULT true;

CREATE TABLE IF NOT EXISTS gacha_guild_character_images (
    guild_id TEXT NOT NULL,
    character_id BIGINT NOT NULL REFERENCES gacha_characters(id) ON DELETE CASCADE,
    asset_id BIGINT NOT NULL REFERENCES gacha_assets(id) ON DELETE CASCADE,
    set_by TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (guild_id, character_id)
);

CREATE INDEX IF NOT EXISTS idx_gacha_guild_char_images_asset ON gacha_guild_character_images (asset_id);

ALTER TABLE gacha_guild_character_images ENABLE ROW LEVEL SECURITY;

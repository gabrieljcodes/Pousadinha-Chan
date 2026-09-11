ALTER TABLE gacha_assets ADD COLUMN IF NOT EXISTS is_extra BOOLEAN NOT NULL DEFAULT false;
UPDATE gacha_assets SET is_extra = true WHERE provider != 'anilist';
CREATE INDEX IF NOT EXISTS gacha_assets_extra ON gacha_assets(character_id, is_extra);

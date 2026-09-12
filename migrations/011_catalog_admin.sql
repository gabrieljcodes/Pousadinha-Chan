-- Editorial state is separate from provider popularity and guild collections.
ALTER TABLE gacha_characters ADD COLUMN IF NOT EXISTS editorial_favorite BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE gacha_characters ADD COLUMN IF NOT EXISTS archived_at TIMESTAMPTZ;
ALTER TABLE gacha_assets ADD COLUMN IF NOT EXISTS editorial_favorite BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE gacha_assets ADD COLUMN IF NOT EXISTS is_primary BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE gacha_assets ADD COLUMN IF NOT EXISTS archived_at TIMESTAMPTZ;
CREATE UNIQUE INDEX IF NOT EXISTS gacha_assets_primary ON gacha_assets(character_id) WHERE is_primary;
CREATE INDEX IF NOT EXISTS gacha_characters_editorial ON gacha_characters(id) WHERE editorial_favorite AND archived_at IS NULL;
DO $$ BEGIN
 IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='gacha_characters'::regclass AND conname='gacha_archived_disabled') THEN
  ALTER TABLE gacha_characters ADD CONSTRAINT gacha_archived_disabled CHECK(archived_at IS NULL OR (NOT enabled AND auto_publish_blocked));
 END IF;
 IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='gacha_assets'::regclass AND conname='gacha_archived_asset_hidden') THEN
  ALTER TABLE gacha_assets ADD CONSTRAINT gacha_archived_asset_hidden CHECK(archived_at IS NULL OR (status='rejected' AND NOT is_primary));
 END IF;
END $$;

-- Classify legacy assets only when introducing this column. Subsequent starts
-- must preserve explicit classifications, including portraits from new providers.
DO $$ BEGIN
 IF NOT EXISTS (SELECT 1 FROM pg_attribute
                WHERE attrelid='gacha_assets'::regclass AND attname='is_extra' AND NOT attisdropped) THEN
  ALTER TABLE gacha_assets ADD COLUMN is_extra BOOLEAN NOT NULL DEFAULT false;
  UPDATE gacha_assets SET is_extra = true WHERE provider != 'anilist';
 END IF;
END $$;
CREATE INDEX IF NOT EXISTS gacha_assets_extra ON gacha_assets(character_id, is_extra);

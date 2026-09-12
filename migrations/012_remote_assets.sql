-- Allow remote assets (e.g. MyAnimeList CDN links) and varied image formats
ALTER TABLE gacha_assets DROP CONSTRAINT IF EXISTS gacha_assets_media_type_check;
ALTER TABLE gacha_assets ADD CONSTRAINT gacha_assets_media_type_check 
  CHECK (media_type IN ('image/png', 'image/gif', 'image/jpeg', 'image/jpg', 'image/webp'));

ALTER TABLE gacha_assets DROP CONSTRAINT IF EXISTS gacha_assets_width_check;
ALTER TABLE gacha_assets DROP CONSTRAINT IF EXISTS gacha_assets_height_check;
ALTER TABLE gacha_assets DROP CONSTRAINT IF EXISTS gacha_assets_dimensions_check;
ALTER TABLE gacha_assets ADD CONSTRAINT gacha_assets_dimensions_check 
  CHECK (width > 0 AND height > 0);

-- Allow path to be NULL for remote-only assets (before being downloaded)
ALTER TABLE gacha_assets ALTER COLUMN path DROP NOT NULL;

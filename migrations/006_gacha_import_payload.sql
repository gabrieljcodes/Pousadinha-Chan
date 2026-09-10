-- Persist fetched metadata separately from portrait processing so a restart does not
-- repeat completed API work. NULL also permits resuming jobs created before batching.
ALTER TABLE gacha_import_items ADD COLUMN IF NOT EXISTS payload JSONB;

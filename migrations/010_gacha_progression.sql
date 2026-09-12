ALTER TABLE gacha_collection ADD COLUMN IF NOT EXISTS keys BIGINT NOT NULL DEFAULT 0 CHECK(keys >= 0);
-- This lineage survives trades/gifts, but a new claim after divorce gets a new one.
ALTER TABLE gacha_collection ADD COLUMN IF NOT EXISTS key_epoch UUID NOT NULL DEFAULT gen_random_uuid();
ALTER TABLE gacha_rolls ADD COLUMN IF NOT EXISTS key_awarded BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE gacha_rolls ADD COLUMN IF NOT EXISTS key_epoch UUID;
ALTER TABLE gacha_rolls ADD COLUMN IF NOT EXISTS key_revoked BOOLEAN NOT NULL DEFAULT false;

DO $$ BEGIN
 IF NOT EXISTS(SELECT 1 FROM pg_constraint WHERE conrelid='gacha_rolls'::regclass AND conname='gacha_roll_key_state') THEN
  ALTER TABLE gacha_rolls ADD CONSTRAINT gacha_roll_key_state CHECK(
   (NOT key_awarded AND key_epoch IS NULL AND NOT key_revoked) OR (key_awarded AND key_epoch IS NOT NULL)
  );
 END IF;
END $$;

-- Remove the old gameplay ceiling. BIGINT wallet overflow still aborts the transaction.
ALTER TABLE gacha_actions DROP CONSTRAINT IF EXISTS gacha_actions_payout_check;
ALTER TABLE gacha_actions ADD CONSTRAINT gacha_actions_payout_check CHECK(payout >= 0);
ALTER TABLE gacha_actions ADD COLUMN IF NOT EXISTS pricing_version INT NOT NULL DEFAULT 1;
-- Do not honor pending old-price quotes after upgrading. Keep completed audit records.
UPDATE gacha_actions SET status='cancelled',resolved_at=now()
 WHERE kind='divorce' AND status='pending' AND pricing_version=1;
ALTER TABLE gacha_actions ALTER COLUMN pricing_version SET DEFAULT 2;

-- One exact-decimal implementation for all contextual values, with no gameplay cap.
-- +1 basis point per currently owned character; +200 bp/key, +1000 bp/10 keys.
CREATE OR REPLACE FUNCTION gacha_character_value(favorites BIGINT, claimed BIGINT, key_count BIGINT)
RETURNS NUMERIC LANGUAGE SQL IMMUTABLE PARALLEL SAFE AS $$
 SELECT floor(
  greatest(10::numeric,floor(greatest(favorites,0)::numeric * 15 / 1000))
  * (10000::numeric + greatest(claimed,0)::numeric)
  * (10000::numeric + greatest(key_count,0)::numeric * 200 + floor(greatest(key_count,0)::numeric / 10) * 1000)
  / 100000000
 );
$$;

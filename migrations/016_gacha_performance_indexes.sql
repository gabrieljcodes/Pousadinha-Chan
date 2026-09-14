-- Performance indexes for Gacha queries according to Supabase best practices.

-- 1. Accelerates /top rankings by favourites and id
CREATE INDEX IF NOT EXISTS gacha_characters_favourites ON gacha_characters(favourites DESC, id ASC) WHERE enabled;

-- 2. Accelerates gender-filtered /top waifus and husbandos
CREATE INDEX IF NOT EXISTS gacha_characters_gender_favourites ON gacha_characters(lower(trim(gender)), favourites DESC, id ASC) WHERE enabled;

-- 3. Accelerates primary asset portrait lookup without in-memory sorting
CREATE INDEX IF NOT EXISTS gacha_assets_primary_lookup ON gacha_assets(character_id, is_primary DESC, id ASC) WHERE status = 'approved';

-- 4. Foreign Key indexes to prevent table-level locks and slow CASCADE scans
CREATE INDEX IF NOT EXISTS gacha_rolls_character_id ON gacha_rolls(character_id);
CREATE INDEX IF NOT EXISTS gacha_rolls_user_id ON gacha_rolls(user_id);
CREATE INDEX IF NOT EXISTS gacha_rolls_claimed_by ON gacha_rolls(claimed_by) WHERE claimed_by IS NOT NULL;
CREATE INDEX IF NOT EXISTS gacha_guild_character_aliases_set_by ON gacha_guild_character_aliases(set_by);
CREATE INDEX IF NOT EXISTS gacha_import_items_character_id ON gacha_import_items(character_id) WHERE character_id IS NOT NULL;

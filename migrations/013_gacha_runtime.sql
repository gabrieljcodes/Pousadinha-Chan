-- Derived state only. Ownership, keys and wallets remain in their original tables.
-- Lock writers while establishing the initial count and its maintenance triggers.
LOCK TABLE gacha_collection IN SHARE ROW EXCLUSIVE MODE;
DO $$ BEGIN
 IF to_regclass('gacha_guild_stats') IS NULL THEN
  CREATE TABLE gacha_guild_stats (
   guild_id TEXT PRIMARY KEY,
   claimed BIGINT NOT NULL DEFAULT 0 CHECK(claimed >= 0)
  );
  INSERT INTO gacha_guild_stats(guild_id,claimed)
   SELECT guild_id,count(*) FROM gacha_collection GROUP BY guild_id;
 END IF;
END $$;
ALTER TABLE gacha_guild_stats ENABLE ROW LEVEL SECURITY;

CREATE OR REPLACE FUNCTION gacha_claimed_count(target_guild TEXT)
RETURNS BIGINT LANGUAGE SQL STABLE AS $$
 SELECT COALESCE((SELECT claimed FROM gacha_guild_stats WHERE guild_id=target_guild),0);
$$;

CREATE OR REPLACE FUNCTION gacha_count_insert() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO gacha_guild_stats(guild_id,claimed)
  SELECT guild_id,count(*) FROM new_claims GROUP BY guild_id ORDER BY guild_id
  ON CONFLICT(guild_id) DO UPDATE SET claimed=gacha_guild_stats.claimed+excluded.claimed;
 RETURN NULL;
END $$;
CREATE OR REPLACE FUNCTION gacha_count_delete() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE delta RECORD;
BEGIN
 FOR delta IN SELECT guild_id,count(*) AS n FROM old_claims GROUP BY guild_id ORDER BY guild_id LOOP
  UPDATE gacha_guild_stats SET claimed=claimed-delta.n WHERE guild_id=delta.guild_id;
 END LOOP;
 RETURN NULL;
END $$;
CREATE OR REPLACE FUNCTION gacha_count_update() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE delta RECORD;
BEGIN
 -- Ordinary keys/trades change no guild counts and acquire no counter row locks.
 FOR delta IN SELECT guild_id,sum(n)::bigint AS n FROM (
  SELECT guild_id,count(*) AS n FROM new_claims GROUP BY guild_id
  UNION ALL SELECT guild_id,-count(*) FROM old_claims GROUP BY guild_id
 ) changes GROUP BY guild_id HAVING sum(n)<>0 ORDER BY guild_id LOOP
  IF delta.n>0 THEN
   INSERT INTO gacha_guild_stats(guild_id,claimed) VALUES(delta.guild_id,delta.n)
    ON CONFLICT(guild_id) DO UPDATE SET claimed=gacha_guild_stats.claimed+excluded.claimed;
  ELSE
   UPDATE gacha_guild_stats SET claimed=claimed+delta.n WHERE guild_id=delta.guild_id;
  END IF;
 END LOOP;
 RETURN NULL;
END $$;
CREATE OR REPLACE FUNCTION gacha_count_truncate() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 TRUNCATE gacha_guild_stats;
 RETURN NULL;
END $$;
CREATE OR REPLACE TRIGGER gacha_count_insert AFTER INSERT ON gacha_collection
 REFERENCING NEW TABLE AS new_claims FOR EACH STATEMENT EXECUTE FUNCTION gacha_count_insert();
CREATE OR REPLACE TRIGGER gacha_count_delete AFTER DELETE ON gacha_collection
 REFERENCING OLD TABLE AS old_claims FOR EACH STATEMENT EXECUTE FUNCTION gacha_count_delete();
CREATE OR REPLACE TRIGGER gacha_count_update AFTER UPDATE ON gacha_collection
 REFERENCING OLD TABLE AS old_claims NEW TABLE AS new_claims FOR EACH STATEMENT EXECUTE FUNCTION gacha_count_update();
CREATE OR REPLACE TRIGGER gacha_count_truncate AFTER TRUNCATE ON gacha_collection
 FOR EACH STATEMENT EXECUTE FUNCTION gacha_count_truncate();

-- Namespace prevents caches from leaking across databases/test schemas. Versions
-- change in the catalog writer's transaction, including direct SQL/admin edits.
CREATE TABLE IF NOT EXISTS gacha_catalog_revision (
 singleton BOOLEAN PRIMARY KEY DEFAULT true CHECK(singleton),
 namespace UUID NOT NULL DEFAULT gen_random_uuid(),
 revision BIGINT NOT NULL DEFAULT 0
);
INSERT INTO gacha_catalog_revision(singleton) VALUES(true) ON CONFLICT DO NOTHING;
ALTER TABLE gacha_catalog_revision ENABLE ROW LEVEL SECURITY;
CREATE OR REPLACE FUNCTION gacha_catalog_changed() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 UPDATE gacha_catalog_revision SET revision=revision+1 WHERE singleton;
 RETURN NULL;
END $$;
CREATE OR REPLACE TRIGGER gacha_catalog_changed AFTER INSERT OR DELETE OR TRUNCATE OR UPDATE OF enabled,gender ON gacha_characters
 FOR EACH STATEMENT EXECUTE FUNCTION gacha_catalog_changed();
CREATE OR REPLACE TRIGGER gacha_assets_changed AFTER INSERT OR DELETE OR TRUNCATE OR UPDATE OF status,character_id ON gacha_assets
 FOR EACH STATEMENT EXECUTE FUNCTION gacha_catalog_changed();
CREATE OR REPLACE TRIGGER gacha_links_changed AFTER INSERT OR UPDATE OR DELETE OR TRUNCATE ON gacha_character_works
 FOR EACH STATEMENT EXECUTE FUNCTION gacha_catalog_changed();
CREATE OR REPLACE TRIGGER gacha_works_changed AFTER UPDATE OF kind OR DELETE OR TRUNCATE ON gacha_works
 FOR EACH STATEMENT EXECUTE FUNCTION gacha_catalog_changed();

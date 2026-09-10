-- Additive migration. Catalog is global; ownership and limits belong to a guild.
CREATE TABLE IF NOT EXISTS gacha_characters (
 id BIGSERIAL PRIMARY KEY, name TEXT NOT NULL, native_name TEXT NOT NULL DEFAULT '',
 aliases JSONB NOT NULL DEFAULT '[]', gender TEXT NOT NULL DEFAULT '', description TEXT NOT NULL DEFAULT '',
 favourites INTEGER NOT NULL DEFAULT 0 CHECK(favourites >= 0), enabled BOOLEAN NOT NULL DEFAULT false,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS gacha_character_sources (
 provider TEXT NOT NULL, external_id TEXT NOT NULL, character_id BIGINT NOT NULL REFERENCES gacha_characters(id),
 source_url TEXT NOT NULL, synced_at TIMESTAMPTZ NOT NULL DEFAULT now(), PRIMARY KEY(provider, external_id)
);
CREATE TABLE IF NOT EXISTS gacha_works (
 id BIGSERIAL PRIMARY KEY, provider TEXT NOT NULL, external_id TEXT NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('anime','manga','game','other')), title TEXT NOT NULL,
 native_title TEXT NOT NULL DEFAULT '', genres JSONB NOT NULL DEFAULT '[]', studios JSONB NOT NULL DEFAULT '[]',
 source_url TEXT NOT NULL, UNIQUE(provider, external_id)
);
CREATE TABLE IF NOT EXISTS gacha_character_works (
 character_id BIGINT NOT NULL REFERENCES gacha_characters(id), work_id BIGINT NOT NULL REFERENCES gacha_works(id),
 role TEXT NOT NULL DEFAULT '', PRIMARY KEY(character_id, work_id)
);
CREATE TABLE IF NOT EXISTS gacha_booru_tags (
 character_id BIGINT NOT NULL REFERENCES gacha_characters(id), provider TEXT NOT NULL,
 tag TEXT NOT NULL, PRIMARY KEY(character_id, provider)
);
CREATE TABLE IF NOT EXISTS gacha_assets (
 id BIGSERIAL PRIMARY KEY, character_id BIGINT NOT NULL REFERENCES gacha_characters(id),
 provider TEXT NOT NULL, external_id TEXT NOT NULL, source_url TEXT NOT NULL, attribution TEXT NOT NULL DEFAULT '',
 sha256 TEXT NOT NULL, path TEXT NOT NULL UNIQUE, media_type TEXT NOT NULL CHECK(media_type IN ('image/png','image/gif')),
 width INT NOT NULL DEFAULT 420 CHECK(width=420), height INT NOT NULL DEFAULT 600 CHECK(height=600),
 status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','approved','rejected')),
 reviewed_by TEXT, reviewed_at TIMESTAMPTZ, created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 UNIQUE(character_id, provider, external_id), UNIQUE(character_id, sha256)
);
CREATE INDEX IF NOT EXISTS gacha_assets_available ON gacha_assets(character_id,id) WHERE status='approved';
CREATE INDEX IF NOT EXISTS gacha_character_names ON gacha_characters(lower(name));
CREATE INDEX IF NOT EXISTS gacha_work_characters ON gacha_character_works(work_id,character_id);
CREATE INDEX IF NOT EXISTS gacha_source_character ON gacha_character_sources(character_id);
CREATE TABLE IF NOT EXISTS gacha_players (
 guild_id TEXT NOT NULL, user_id TEXT NOT NULL REFERENCES users(id),
 window_start TIMESTAMPTZ NOT NULL DEFAULT now(), rolls_used INT NOT NULL DEFAULT 0 CHECK(rolls_used >= 0),
 claim_after TIMESTAMPTZ NOT NULL DEFAULT '-infinity', PRIMARY KEY(guild_id,user_id)
);
CREATE TABLE IF NOT EXISTS gacha_rolls (
 id TEXT PRIMARY KEY, guild_id TEXT NOT NULL, channel_id TEXT NOT NULL, user_id TEXT NOT NULL REFERENCES users(id),
 character_id BIGINT NOT NULL REFERENCES gacha_characters(id), created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 expires_at TIMESTAMPTZ NOT NULL, claimed_by TEXT REFERENCES users(id), request_id TEXT NOT NULL UNIQUE
);
CREATE INDEX IF NOT EXISTS gacha_rolls_expiry ON gacha_rolls(expires_at);
CREATE TABLE IF NOT EXISTS gacha_collection (
 guild_id TEXT NOT NULL, character_id BIGINT NOT NULL REFERENCES gacha_characters(id),
 user_id TEXT NOT NULL REFERENCES users(id), claimed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 PRIMARY KEY(guild_id,character_id)
);
CREATE INDEX IF NOT EXISTS gacha_collection_owner ON gacha_collection(guild_id,user_id,character_id);
CREATE TABLE IF NOT EXISTS gacha_wishes (
 guild_id TEXT NOT NULL, user_id TEXT NOT NULL REFERENCES users(id), character_id BIGINT NOT NULL REFERENCES gacha_characters(id),
 PRIMARY KEY(guild_id,user_id,character_id)
);
-- No browser/client access. Bot connects as owner or a BYPASSRLS service role.
ALTER TABLE gacha_characters ENABLE ROW LEVEL SECURITY;
ALTER TABLE gacha_character_sources ENABLE ROW LEVEL SECURITY;
ALTER TABLE gacha_works ENABLE ROW LEVEL SECURITY;
ALTER TABLE gacha_character_works ENABLE ROW LEVEL SECURITY;
ALTER TABLE gacha_booru_tags ENABLE ROW LEVEL SECURITY;
ALTER TABLE gacha_assets ENABLE ROW LEVEL SECURITY;
ALTER TABLE gacha_players ENABLE ROW LEVEL SECURITY;
ALTER TABLE gacha_rolls ENABLE ROW LEVEL SECURITY;
ALTER TABLE gacha_collection ENABLE ROW LEVEL SECURITY;
ALTER TABLE gacha_wishes ENABLE ROW LEVEL SECURITY;

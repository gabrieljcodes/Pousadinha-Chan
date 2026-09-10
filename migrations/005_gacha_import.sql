ALTER TABLE gacha_characters ADD COLUMN IF NOT EXISTS auto_publish_blocked BOOLEAN NOT NULL DEFAULT false;
CREATE TABLE IF NOT EXISTS gacha_import_jobs (
 name TEXT PRIMARY KEY, target INT NOT NULL CHECK(target BETWEEN 1 AND 100000),
 auto_approve BOOLEAN NOT NULL, next_page INT NOT NULL DEFAULT 1,
 exhausted BOOLEAN NOT NULL DEFAULT false, created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS gacha_import_items (
 job TEXT NOT NULL REFERENCES gacha_import_jobs(name), external_id BIGINT NOT NULL,
 status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','imported','skipped','failed')),
 attempts INT NOT NULL DEFAULT 0, character_id BIGINT REFERENCES gacha_characters(id),
 last_error TEXT NOT NULL DEFAULT '', updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 PRIMARY KEY(job,external_id)
);
CREATE INDEX IF NOT EXISTS gacha_import_pending ON gacha_import_items(job,status,external_id);
CREATE TABLE IF NOT EXISTS gacha_import_leases (
 provider TEXT PRIMARY KEY, owner TEXT NOT NULL, expires_at TIMESTAMPTZ NOT NULL
);
ALTER TABLE gacha_import_jobs ENABLE ROW LEVEL SECURITY;
ALTER TABLE gacha_import_items ENABLE ROW LEVEL SECURITY;
ALTER TABLE gacha_import_leases ENABLE ROW LEVEL SECURITY;

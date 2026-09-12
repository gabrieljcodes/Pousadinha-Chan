-- A token identifies a specific period of ownership. Returning to the same owner
-- must not make an old divorce/trade confirmation valid again.
ALTER TABLE gacha_collection ADD COLUMN IF NOT EXISTS ownership_token UUID NOT NULL DEFAULT gen_random_uuid();
CREATE TABLE IF NOT EXISTS gacha_actions (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(), request_id TEXT NOT NULL UNIQUE,
 guild_id TEXT NOT NULL, channel_id TEXT NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('divorce','trade','gift')),
 proposer_id TEXT NOT NULL REFERENCES users(id), recipient_id TEXT REFERENCES users(id),
 offered_id BIGINT NOT NULL REFERENCES gacha_characters(id), requested_id BIGINT REFERENCES gacha_characters(id),
 offered_token UUID NOT NULL, requested_token UUID,
 payout BIGINT NOT NULL DEFAULT 0 CHECK(payout BETWEEN 0 AND 25000),
 status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','completed','cancelled','declined','expired','stale')),
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(), expires_at TIMESTAMPTZ NOT NULL DEFAULT now()+interval '10 minutes',
 resolved_at TIMESTAMPTZ,
 CHECK((kind='divorce' AND recipient_id IS NULL AND requested_id IS NULL AND requested_token IS NULL)
    OR (kind='gift' AND recipient_id IS NOT NULL AND requested_id IS NULL AND requested_token IS NULL AND payout=0)
    OR (kind='trade' AND recipient_id IS NOT NULL AND requested_id IS NOT NULL AND requested_token IS NOT NULL AND offered_id<>requested_id AND payout=0)),
 CHECK(recipient_id IS NULL OR recipient_id<>proposer_id)
);
CREATE INDEX IF NOT EXISTS gacha_actions_proposer ON gacha_actions(proposer_id,guild_id,created_at DESC);
CREATE INDEX IF NOT EXISTS gacha_actions_recipient ON gacha_actions(recipient_id,guild_id,created_at DESC);
CREATE INDEX IF NOT EXISTS gacha_actions_offered ON gacha_actions(offered_id);
CREATE INDEX IF NOT EXISTS gacha_actions_requested ON gacha_actions(requested_id);
CREATE INDEX IF NOT EXISTS gacha_actions_pending ON gacha_actions(guild_id,expires_at) WHERE status='pending';
CREATE INDEX IF NOT EXISTS gacha_characters_gender_enabled ON gacha_characters(lower(trim(gender)),id) WHERE enabled;
ALTER TABLE gacha_actions ENABLE ROW LEVEL SECURITY;
-- Owner/BYPASSRLS service connections only; Discord user identity is checked by the bot.

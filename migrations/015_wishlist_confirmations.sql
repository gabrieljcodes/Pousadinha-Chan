-- One expiring, single-use confirmation per guild member. No existing data changes.
CREATE TABLE IF NOT EXISTS gacha_wishlist_confirmations (
 guild_id TEXT NOT NULL,
 user_id TEXT NOT NULL,
 channel_id TEXT NOT NULL,
 token UUID NOT NULL,
 expires_at TIMESTAMPTZ NOT NULL,
 PRIMARY KEY (guild_id,user_id),
 FOREIGN KEY (guild_id,user_id) REFERENCES gacha_players(guild_id,user_id) ON DELETE CASCADE
);
ALTER TABLE gacha_wishlist_confirmations ENABLE ROW LEVEL SECURITY;

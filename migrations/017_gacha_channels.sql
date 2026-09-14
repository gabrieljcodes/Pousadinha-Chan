-- Add channel restrictions for gacha: roll channel and command channel
ALTER TABLE gacha_guild_settings
    ADD COLUMN IF NOT EXISTS roll_channel_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS cmd_channel_id TEXT NOT NULL DEFAULT '';

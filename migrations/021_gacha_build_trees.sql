-- 021_gacha_build_trees.sql: Gacha build trees, classes, skill unlocks, and fake wish trap
ALTER TABLE gacha_rolls ADD COLUMN IF NOT EXISTS fake_character_id BIGINT REFERENCES gacha_characters(id) ON DELETE SET NULL;
ALTER TABLE gacha_rolls ADD COLUMN IF NOT EXISTS is_trap BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE gacha_rolls ADD COLUMN IF NOT EXISTS trap_revealed BOOLEAN NOT NULL DEFAULT false;

CREATE INDEX IF NOT EXISTS idx_gacha_rolls_trap ON gacha_rolls (id) WHERE is_trap = true;
CREATE INDEX IF NOT EXISTS idx_gacha_rolls_fake_char ON gacha_rolls (fake_character_id) WHERE fake_character_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS gacha_player_builds (
    guild_id TEXT NOT NULL,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    total_points INT NOT NULL DEFAULT 0 CHECK (total_points >= 0 AND total_points <= 20),
    spent_points INT NOT NULL DEFAULT 0 CHECK (spent_points >= 0 AND spent_points <= total_points),
    last_respec_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (guild_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_gacha_player_builds_user ON gacha_player_builds (user_id);

CREATE TABLE IF NOT EXISTS gacha_player_skills (
    guild_id TEXT NOT NULL,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    class_id TEXT NOT NULL,
    skill_id TEXT NOT NULL,
    tier INT NOT NULL CHECK (tier BETWEEN 1 AND 4),
    points_cost INT NOT NULL CHECK (points_cost > 0),
    unlocked_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (guild_id, user_id, skill_id)
);

CREATE INDEX IF NOT EXISTS idx_gacha_player_skills_user ON gacha_player_skills (user_id, class_id);

ALTER TABLE gacha_player_builds ENABLE ROW LEVEL SECURITY;
ALTER TABLE gacha_player_skills ENABLE ROW LEVEL SECURITY;

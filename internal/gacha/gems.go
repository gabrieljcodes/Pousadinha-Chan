package gacha

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

type GemType string

const (
	GemPeridot   GemType = "peridot"
	GemTopaz     GemType = "topaz"
	GemEmerald   GemType = "emerald"
	GemAmethyst  GemType = "amethyst"
	GemRuby      GemType = "ruby"
	GemDiamond   GemType = "diamond"
	GemPrismatic GemType = "prismatic"

	BaseGemSpawnChance = 0.40 // 40% chance for married rolls of other users
	MaxGemPower        = 100
	DefaultPowerCost   = 25
)

// Gem defines the attributes, visual representation, economy value, and power cost of a gacha gem.
type Gem struct {
	Type          GemType
	Name          string
	EmojiID       string
	EmojiName     string
	FallbackEmoji string
	Value         int
	PowerCost     int // 0 for Peridot, 25 for standard, -25 for Prismatic (restores 25%)
	Weight        int // Weighted random spawn frequency
	Color         int // Discord embed highlight color
	Description   string
}

// ComponentEmoji returns the discordgo ComponentEmoji for buttons.
func (g Gem) ComponentEmoji() *discordgo.ComponentEmoji {
	if g.EmojiID != "" {
		return &discordgo.ComponentEmoji{
			ID:   g.EmojiID,
			Name: g.EmojiName,
		}
	}
	return &discordgo.ComponentEmoji{
		Name: g.FallbackEmoji,
	}
}

// DiscordString formats the emoji for embed text.
func (g Gem) DiscordString() string {
	if g.EmojiID != "" {
		return fmt.Sprintf("<:%s:%s>", g.EmojiName, g.EmojiID)
	}
	return g.FallbackEmoji
}

var (
	allGems = []Gem{
		{
			Type:          GemPeridot,
			Name:          "Peridot",
			EmojiID:       "1549191525001461912",
			EmojiName:     "gem_peridot",
			FallbackEmoji: "🟢",
			Value:         50,
			PowerCost:     0, // Free energy power
			Weight:        30,
			Color:         0x88d436,
			Description:   "Zero Astral Power Cost",
		},
		{
			Type:          GemTopaz,
			Name:          "Topaz",
			EmojiID:       "1549191529296560261",
			EmojiName:     "gem_topaz",
			FallbackEmoji: "🟠",
			Value:         100,
			PowerCost:     DefaultPowerCost,
			Weight:        25,
			Color:         0xf39c12,
			Description:   "Energized solar crystal",
		},
		{
			Type:          GemEmerald,
			Name:          "Emerald",
			EmojiID:       "1549191523315490857",
			EmojiName:     "gem_emerald",
			FallbackEmoji: "🟩",
			Value:         175,
			PowerCost:     DefaultPowerCost,
			Weight:        18,
			Color:         0x2ecc71,
			Description:   "Mystic polyhedron of harmony",
		},
		{
			Type:          GemAmethyst,
			Name:          "Amethyst",
			EmojiID:       "1549191519498674216",
			EmojiName:     "gem_amethyst",
			FallbackEmoji: "🟣",
			Value:         275,
			PowerCost:     DefaultPowerCost,
			Weight:        13,
			Color:         0x9b59b6,
			Description:   "Deep arcane crystal",
		},
		{
			Type:          GemRuby,
			Name:          "Ruby",
			EmojiID:       "1549191463575887933",
			EmojiName:     "gem_ruby",
			FallbackEmoji: "🔴",
			Value:         450,
			PowerCost:     DefaultPowerCost,
			Weight:        8,
			Color:         0xe74c3c,
			Description:   "Radiant crimson jewel",
		},
		{
			Type:          GemDiamond,
			Name:          "Diamond",
			EmojiID:       "1549191520756830258",
			EmojiName:     "gem_diamond",
			FallbackEmoji: "💎",
			Value:         700,
			PowerCost:     DefaultPowerCost,
			Weight:        4,
			Color:         0x3498db,
			Description:   "Cut celestial purity",
		},
		{
			Type:          GemPrismatic,
			Name:          "Astral Prism",
			EmojiID:       "1549191522031763476",
			EmojiName:     "gem_prismatic",
			FallbackEmoji: "✨",
			Value:         1200,
			PowerCost:     -25, // Restores 25% power!
			Weight:        2,
			Color:         0xffffff,
			Description:   "Overcharge: Restores +25% Astral Power",
		},
	}

	gemMap   map[GemType]Gem
	gemInit  sync.Once
	totalGemWeight int
)

func initGems() {
	gemMap = make(map[GemType]Gem, len(allGems))
	totalGemWeight = 0
	for _, g := range allGems {
		gemMap[g.Type] = g
		totalGemWeight += g.Weight
	}
}

// FindGem returns a Gem definition by its type code.
func FindGem(t GemType) (Gem, bool) {
	gemInit.Do(initGems)
	g, ok := gemMap[t]
	return g, ok
}

// RollGem determines whether a gem spawns and selects its rarity tier.
// If isKey is true (user rolled their own married character), spawn is 100% guaranteed.
// Otherwise, it checks the base spawn probability (40%).
func RollGem(isKey bool) (Gem, bool) {
	gemInit.Do(initGems)

	if !isKey {
		if rand.Float64() >= BaseGemSpawnChance {
			return Gem{}, false
		}
	}

	roll := rand.Intn(totalGemWeight)
	running := 0
	for _, g := range allGems {
		running += g.Weight
		if roll < running {
			return g, true
		}
	}
	return allGems[0], true
}

// GetEffectiveGemPower computes the user's current gem power in the guild,
// automatically returning 100% if the hourly roll window has reset.
func (s *Store) GetEffectiveGemPower(ctx context.Context, guildID, userID string, now time.Time) (int, error) {
	schedule := s.GuildSchedule(ctx, guildID)
	rWin := schedule.RollWindow(now)

	var windowStart time.Time
	var power int
	err := s.DB.QueryRowContext(ctx, `
		SELECT window_start, gem_power 
		FROM gacha_players 
		WHERE guild_id=$1 AND user_id=$2
	`, guildID, userID).Scan(&windowStart, &power)

	if err == sql.ErrNoRows || windowStart.Before(rWin.CurrentStart) {
		return MaxGemPower, nil
	}
	if err != nil {
		return MaxGemPower, err
	}
	return max(0, min(MaxGemPower, power)), nil
}

// GemClaimResult contains the outcome of an atomic gem claim attempt.
type GemClaimResult struct {
	Gem               Gem
	ClaimedBy         string
	AlreadyClaimed    bool
	ClaimedByOther    string
	Expired           bool
	InsufficientPower bool
	CurrentPower      int
	RequiredPower     int
	RemainingPower    int
	NewBalance        int64
	NextReset         time.Time
}

// ClaimGemAtomic executes an atomic, concurrency-safe claim of a roll's gem.
// Exactly one user can successfully claim a gem.
func (s *Store) ClaimGemAtomic(ctx context.Context, guildID, channelID, rollID, userID string) (*GemClaimResult, error) {
	if guildID == "" || rollID == "" || userID == "" {
		return nil, fmt.Errorf("invalid parameters for gem claim")
	}

	now := time.Now()
	schedule := s.GuildSchedule(ctx, guildID)
	rWin := schedule.RollWindow(now)

	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	// Ensure parent user exists
	_, _ = tx.ExecContext(ctx, `INSERT INTO users(id, balance) VALUES($1, 0) ON CONFLICT(id) DO NOTHING`, userID)
	_, _ = tx.ExecContext(ctx, `INSERT INTO guild_members(guild_id, user_id, balance) VALUES($1, $2, 0) ON CONFLICT(guild_id, user_id) DO NOTHING`, guildID, userID)
	_, _ = tx.ExecContext(ctx, `
		INSERT INTO gacha_players(guild_id, user_id, window_start, rolls_used, gem_power)
		VALUES($1, $2, $3, 0, 100)
		ON CONFLICT(guild_id, user_id) DO NOTHING
	`, guildID, userID, rWin.CurrentStart)

	// 1. Lock the roll row to verify gem availability
	var gemTypeCode string
	var gemValue, powerCost int
	var expiresAt time.Time
	var claimedBy sql.NullString

	err = tx.QueryRowContext(ctx, `
		SELECT gem_type, gem_value, gem_power_cost, expires_at, gem_claimed_by
		FROM gacha_rolls
		WHERE id = $1 AND guild_id = $2
		FOR UPDATE
	`, rollID, guildID).Scan(&gemTypeCode, &gemValue, &powerCost, &expiresAt, &claimedBy)

	if err == sql.ErrNoRows || gemTypeCode == "" {
		return nil, fmt.Errorf("this roll does not contain a gem")
	}
	if err != nil {
		return nil, err
	}

	gem, ok := FindGem(GemType(gemTypeCode))
	if !ok {
		gem = Gem{
			Type:          GemType(gemTypeCode),
			Name:          "Gem",
			Value:         gemValue,
			PowerCost:     powerCost,
			FallbackEmoji: "💎",
		}
	}

	if claimedBy.Valid && claimedBy.String != "" {
		return &GemClaimResult{
			Gem:            gem,
			AlreadyClaimed: true,
			ClaimedByOther: claimedBy.String,
			NextReset:      rWin.NextReset,
		}, nil
	}

	if now.After(expiresAt) {
		return &GemClaimResult{
			Gem:       gem,
			Expired:   true,
			NextReset: rWin.NextReset,
		}, nil
	}

	// 2. Lock user's player record to verify & deduct/restore gem power
	var playerWindowStart time.Time
	var userPower int
	err = tx.QueryRowContext(ctx, `
		SELECT window_start, gem_power
		FROM gacha_players
		WHERE guild_id = $1 AND user_id = $2
		FOR UPDATE
	`, guildID, userID).Scan(&playerWindowStart, &userPower)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}

	currentPower := MaxGemPower
	if err == nil && !playerWindowStart.Before(rWin.CurrentStart) {
		currentPower = max(0, min(MaxGemPower, userPower))
	}

	// Check if user has enough power for standard/costly gems
	if powerCost > 0 && currentPower < powerCost {
		return &GemClaimResult{
			Gem:               gem,
			InsufficientPower: true,
			CurrentPower:      currentPower,
			RequiredPower:     powerCost,
			NextReset:         rWin.NextReset,
		}, nil
	}

	// Calculate new power (if powerCost < 0, it restores power up to MaxGemPower)
	newPower := currentPower
	if powerCost > 0 {
		newPower = max(0, currentPower-powerCost)
	} else if powerCost < 0 {
		newPower = min(MaxGemPower, currentPower-powerCost) // e.g. -(-25) = +25
	}

	// 3. Mark gem as claimed on gacha_rolls
	_, err = tx.ExecContext(ctx, `
		UPDATE gacha_rolls
		SET gem_claimed_by = $1, gem_claimed_at = $2
		WHERE id = $3
	`, userID, now, rollID)
	if err != nil {
		return nil, err
	}

	// 4. Update player's gem power and current window
	_, err = tx.ExecContext(ctx, `
		UPDATE gacha_players
		SET window_start = $3, gem_power = $4
		WHERE guild_id = $1 AND user_id = $2
	`, guildID, userID, rWin.CurrentStart, newPower)
	if err != nil {
		return nil, err
	}

	// 5. Credit coins to user balance
	var newBalance int64
	err = tx.QueryRowContext(ctx, `
		UPDATE guild_members
		SET balance = balance + $1, updated_at = $2
		WHERE guild_id = $3 AND user_id = $4
		RETURNING balance
	`, gemValue, now, guildID, userID).Scan(&newBalance)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &GemClaimResult{
		Gem:            gem,
		ClaimedBy:      userID,
		RemainingPower: newPower,
		NewBalance:     newBalance,
		NextReset:      rWin.NextReset,
	}, nil
}

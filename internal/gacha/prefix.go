package gacha

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// PrefixCommand holds the parsed intent of a prefix roll command.
type PrefixCommand struct {
	Pool   string // gacha pool code ("w", "h", "wa", "roll", etc.)
	SubCmd string // "roll" or "status"
}

// User concurrency lock map to serialize roll executions per user and prevent burst race conditions.
var userRollLocks sync.Map

func getUserRollLock(userID string) *sync.Mutex {
	val, _ := userRollLocks.LoadOrStore(userID, &sync.Mutex{})
	return val.(*sync.Mutex)
}

// Supported pool prefix shortcuts.
var prefixPoolAliases = map[string]string{
	"!w":    "w",
	"!wa":   "wa",
	"!wg":   "wg",
	"!h":    "h",
	"!ha":   "ha",
	"!hg":   "hg",
	"!m":    "roll",
	"!mu":   "roll",
	"!ma":   "ma",
	"!mg":   "mg",
	"!roll": "roll",
	"!r":    "roll",
}

// parsePrefixCommand checks if a string is a recognized gacha prefix command.
func parsePrefixCommand(content string) (PrefixCommand, bool) {
	trimmed := strings.TrimSpace(content)
	if !strings.HasPrefix(trimmed, "!") {
		return PrefixCommand{}, false
	}

	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return PrefixCommand{}, false
	}

	trigger := strings.ToLower(fields[0])

	// Status shortcuts: !tu (time until), !rolls, !quota
	if trigger == "!tu" || trigger == "!rolls" || trigger == "!quota" {
		return PrefixCommand{SubCmd: "status"}, true
	}

	// Direct pool trigger: !w, !wa, !h, !ha, etc.
	if pool, ok := prefixPoolAliases[trigger]; ok {
		// If triggered with "!roll" or "!r", check for an optional sub-pool argument
		if (trigger == "!roll" || trigger == "!r") && len(fields) > 1 {
			arg := strings.ToLower(fields[1])
			switch arg {
			case "w", "waifu", "female":
				pool = "w"
			case "h", "husbando", "male":
				pool = "h"
			case "wa", "anime_female":
				pool = "wa"
			case "ha", "anime_male":
				pool = "ha"
			case "wg", "game_female":
				pool = "wg"
			case "hg", "game_male":
				pool = "hg"
			case "ma", "anime":
				pool = "ma"
			case "mg", "game":
				pool = "mg"
			case "all", "mixed", "m":
				pool = "roll"
			}
		}
		return PrefixCommand{Pool: pool, SubCmd: "roll"}, true
	}

	return PrefixCommand{}, false
}

// PrefixHandler listens for fast '!' roll triggers strictly inside the server's designated roll channel.
func PrefixHandler(s *discordgo.Session, m *discordgo.MessageCreate) {
	if Default == nil || m == nil || m.Author == nil || m.Author.Bot || m.GuildID == "" {
		return
	}

	cmd, ok := parsePrefixCommand(m.Content)
	if !ok {
		// Silently ignore non-gacha commands so other bot prefix commands are unaffected
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	sch := Default.GuildSchedule(ctx, m.GuildID)

	// Gacha channels must be configured and message must be in the designated roll channel
	if sch.RollChannelID == "" || m.ChannelID != sch.RollChannelID {
		return
	}

	// Fast status response inside the roll channel
	if cmd.SubCmd == "status" {
		handlePrefixStatus(ctx, s, m, sch)
		return
	}

	// User lock to prevent rapid spam race conditions
	mu := getUserRollLock(m.Author.ID)
	if !mu.TryLock() {
		// Dropped to protect against burst double-taps
		return
	}
	defer mu.Unlock()

	msg, err := Default.Execute(ctx, m.GuildID, m.ChannelID, m.Author.ID, m.ID, cmd.Pool, "", 1)
	if err != nil {
		if errors.Is(err, ErrLimit) {
			rWin := sch.RollWindow(time.Now())
			content := fmt.Sprintf("❌ %s • Next reset <t:%d:R>", err.Error(), rWin.NextReset.Unix())
			_, _ = s.ChannelMessageSendReply(m.ChannelID, content, m.Reference())
			return
		}

		friendlyText := friendly(err)
		_, _ = s.ChannelMessageSendReply(m.ChannelID, friendlyText, m.Reference())
		return
	}

	// Set message reference for direct reply
	msg.Reference = m.Reference()
	if _, err := s.ChannelMessageSendComplex(m.ChannelID, msg); err != nil {
		log.Printf("[gacha] Prefix roll delivery failed: %v", err)
	}
}

func handlePrefixStatus(ctx context.Context, s *discordgo.Session, m *discordgo.MessageCreate, sch ResetSchedule) {
	now := time.Now()
	rWin := sch.RollWindow(now)

	var count int
	_ = Default.DB.QueryRowContext(ctx, `SELECT count(*) FROM gacha_rolls WHERE guild_id=$1 AND user_id=$2 AND created_at >= $3`, m.GuildID, m.Author.ID, rWin.CurrentStart).Scan(&count)
	remaining := max(0, sch.RollsPerHour-count)
	gemPower, _ := Default.GetEffectiveGemPower(ctx, m.GuildID, m.Author.ID, now)

	var claimAfter time.Time
	_ = Default.DB.QueryRowContext(ctx, `SELECT claim_after FROM gacha_players WHERE guild_id=$1 AND user_id=$2`, m.GuildID, m.Author.ID).Scan(&claimAfter)

	var b strings.Builder
	b.WriteString(fmt.Sprintf("🎲 **Rolls:** **%d** / %d left • Next reset <t:%d:R>\n", remaining, sch.RollsPerHour, rWin.NextReset.Unix()))

	if claimAfter.IsZero() || !claimAfter.After(now) {
		b.WriteString("💍 **Claim:** ✅ Available now!\n")
	} else {
		resetsLeft := 0
		t := rWin.NextReset
		for !t.After(claimAfter) {
			resetsLeft++
			t = t.Add(1 * time.Hour)
		}
		if resetsLeft <= 1 {
			b.WriteString(fmt.Sprintf("💍 **Claim:** ❌ Unavailable • Restores <t:%d:R> (1 reset left)\n", claimAfter.Unix()))
		} else {
			b.WriteString(fmt.Sprintf("💍 **Claim:** ❌ Unavailable • Restores <t:%d:R> (%d resets left)\n", claimAfter.Unix(), resetsLeft))
		}
	}

	if gemPower >= MaxGemPower {
		b.WriteString(fmt.Sprintf("💎 **Astral Power:** **%d%%** / %d%% (Full)", gemPower, MaxGemPower))
	} else {
		b.WriteString(fmt.Sprintf("💎 **Astral Power:** **%d%%** / %d%% • Next reset <t:%d:R>", gemPower, MaxGemPower, rWin.NextReset.Unix()))
	}

	_, _ = s.ChannelMessageSendReply(m.ChannelID, b.String(), m.Reference())
}

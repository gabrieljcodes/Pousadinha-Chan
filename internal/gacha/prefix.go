package gacha

import (
	"bot/internal/locale"
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// PrefixCommand holds the parsed intent of a prefix roll command.
type PrefixCommand struct {
	Pool   string // gacha pool code ("w", "h", "wa", "roll", etc.)
	SubCmd string // "roll", "status", "shop", "inventory", "buy", "use", "open", "addcustom", "mycustoms", "removecustom", "customimage", "autoapprove"
	Arg1   string
	Arg2   int
	Arg3   string
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

	// Shop shortcuts: !shop, !store
	if trigger == "!shop" || trigger == "!store" {
		return PrefixCommand{SubCmd: "shop"}, true
	}

	// Inventory shortcuts: !inv, !inventory, !bag
	if trigger == "!inv" || trigger == "!inventory" || trigger == "!bag" {
		return PrefixCommand{SubCmd: "inventory"}, true
	}

	// Buy command: !buy <item> [qty]
	if trigger == "!buy" && len(fields) > 1 {
		qty := 1
		if len(fields) > 2 {
			if parsedQty, err := strconv.Atoi(fields[2]); err == nil && parsedQty > 0 {
				qty = parsedQty
			}
		}
		return PrefixCommand{SubCmd: "buy", Arg1: strings.ToLower(fields[1]), Arg2: qty}, true
	}

	// Use command: !use <item>
	if trigger == "!use" && len(fields) > 1 {
		return PrefixCommand{SubCmd: "use", Arg1: strings.ToLower(fields[1])}, true
	}

	// Quick use shortcuts: !rr (roll reset), !rc or !rt (claim reset), !shield (snipe shield), !battery, !flare
	if trigger == "!rr" {
		return PrefixCommand{SubCmd: "use", Arg1: string(ItemRollReset)}, true
	}
	if trigger == "!rc" || trigger == "!rt" {
		return PrefixCommand{SubCmd: "use", Arg1: string(ItemClaimReset)}, true
	}
	if trigger == "!shield" {
		return PrefixCommand{SubCmd: "use", Arg1: string(ItemSnipeShield)}, true
	}
	if trigger == "!battery" {
		return PrefixCommand{SubCmd: "use", Arg1: string(ItemGemBattery)}, true
	}
	if trigger == "!flare" {
		return PrefixCommand{SubCmd: "use", Arg1: string(ItemWishFlare)}, true
	}

	// Open lootbox: !open [qty], !chest
	if trigger == "!open" || trigger == "!chest" {
		qty := 1
		if len(fields) > 1 {
			if parsedQty, err := strconv.Atoi(fields[1]); err == nil && parsedQty > 0 {
				qty = parsedQty
			}
		}
		return PrefixCommand{SubCmd: "open", Arg1: string(ItemLootbox), Arg2: qty}, true
	}

	// Custom image submission: !addcustom <character> [url], !aic, !aig, !addimage
	if (trigger == "!addcustom" || trigger == "!aic" || trigger == "!aig" || trigger == "!addimage") && len(fields) > 1 {
		charName := ""
		mediaURL := ""
		lastField := fields[len(fields)-1]
		if len(fields) > 2 && (strings.HasPrefix(lastField, "http://") || strings.HasPrefix(lastField, "https://")) {
			charName = strings.Join(fields[1:len(fields)-1], " ")
			mediaURL = lastField
		} else {
			charName = strings.Join(fields[1:], " ")
		}
		return PrefixCommand{SubCmd: "addcustom", Arg1: charName, Arg3: mediaURL}, true
	}

	// View user custom images: !mycustoms
	if trigger == "!mycustoms" {
		return PrefixCommand{SubCmd: "mycustoms"}, true
	}

	// Build system shortcuts: !builds, !build, !skills, !tree
	if trigger == "!builds" || trigger == "!build" || trigger == "!skills" || trigger == "!tree" {
		return PrefixCommand{SubCmd: "builds"}, true
	}
	if (trigger == "!learnskill" || trigger == "!learn") && len(fields) > 1 {
		return PrefixCommand{SubCmd: "learnskill", Arg1: strings.ToLower(fields[1])}, true
	}
	if trigger == "!respec" || trigger == "!resetbuild" {
		return PrefixCommand{SubCmd: "respec"}, true
	}

	// Remove custom image: !removecustom <asset_id>
	if trigger == "!removecustom" && len(fields) > 1 {
		if id, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
			return PrefixCommand{SubCmd: "removecustom", Arg2: int(id)}, true
		}
	}

	// Set active character image: !ci <character> [index], !customimage <character> [index]
	if (trigger == "!ci" || trigger == "!customimage") && len(fields) > 1 {
		idx := 1
		charName := strings.Join(fields[1:], " ")
		if len(fields) > 2 {
			if parsedIdx, err := strconv.Atoi(fields[len(fields)-1]); err == nil && parsedIdx > 0 {
				idx = parsedIdx
				charName = strings.Join(fields[1:len(fields)-1], " ")
			}
		}
		return PrefixCommand{SubCmd: "customimage", Arg1: charName, Arg2: idx}, true
	}

	// Autoapprove toggle: !autoapprove <on|off|true|false>, !gachaconfig autoapprove <on|off|true|false>
	if trigger == "!autoapprove" && len(fields) > 1 {
		return PrefixCommand{SubCmd: "autoapprove", Arg1: strings.ToLower(fields[1])}, true
	}
	if trigger == "!gachaconfig" && len(fields) > 2 && strings.ToLower(fields[1]) == "autoapprove" {
		return PrefixCommand{SubCmd: "autoapprove", Arg1: strings.ToLower(fields[2])}, true
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

	// Channel permissions: rolls are strictly inside RollChannelID. Other gacha commands allowed in RollChannelID or CmdChannelID.
	if cmd.SubCmd == "roll" {
		if sch.RollChannelID != "" && m.ChannelID != sch.RollChannelID {
			return
		}
	} else {
		if (sch.RollChannelID != "" || sch.CmdChannelID != "") &&
			(m.ChannelID != sch.RollChannelID && m.ChannelID != sch.CmdChannelID) {
			return
		}
	}

	switch cmd.SubCmd {
	case "status":
		handlePrefixStatus(ctx, s, m, sch)
		return
	case "shop":
		msg, err := Default.RenderShop(ctx, m.GuildID, m.Author.ID)
		if err != nil {
			_, _ = s.ChannelMessageSendReply(m.ChannelID, friendly(err), m.Reference())
			return
		}
		msg.Reference = m.Reference()
		_, _ = s.ChannelMessageSendComplex(m.ChannelID, msg)
		return
	case "inventory":
		msg, err := Default.RenderInventory(ctx, m.GuildID, m.Author.ID)
		if err != nil {
			_, _ = s.ChannelMessageSendReply(m.ChannelID, friendly(err), m.Reference())
			return
		}
		msg.Reference = m.Reference()
		_, _ = s.ChannelMessageSendComplex(m.ChannelID, msg)
		return
	case "buy":
		buyRes, err := Default.BuyItem(ctx, m.GuildID, m.Author.ID, ItemID(cmd.Arg1), cmd.Arg2)
		if err != nil {
			_, _ = s.ChannelMessageSendReply(m.ChannelID, friendly(err), m.Reference())
			return
		}
		content := locale.Text("gacha.shop.buy_success", locale.Data{
			"Quantity":   buyRes.Quantity,
			"ItemName":   buyRes.Item.Name(),
			"TotalCost":  buyRes.TotalCost,
			"NewBalance": buyRes.NewBalance,
		})
		_, _ = s.ChannelMessageSendReply(m.ChannelID, content, m.Reference())
		return
	case "use":
		useRes, err := Default.UseItem(ctx, m.GuildID, m.Author.ID, ItemID(cmd.Arg1))
		if err != nil {
			_, _ = s.ChannelMessageSendReply(m.ChannelID, friendly(err), m.Reference())
			return
		}
		if useRes.LootReward != nil {
			embed := &discordgo.MessageEmbed{
				Title:       useRes.LootReward.Title,
				Description: useRes.LootReward.Description,
				Color:       0xe67e22,
				Footer: &discordgo.MessageEmbedFooter{
					Text: "Cosmic Chest Rewards • Pousadinha Gacha",
				},
			}
			msg := &discordgo.MessageSend{
				Embeds:    []*discordgo.MessageEmbed{embed},
				Reference: m.Reference(),
			}
			_, _ = s.ChannelMessageSendComplex(m.ChannelID, msg)
			return
		}
		_, _ = s.ChannelMessageSendReply(m.ChannelID, useRes.Message, m.Reference())
		return
	case "open":
		reward, err := Default.OpenLootbox(ctx, m.GuildID, m.Author.ID)
		if err != nil {
			_, _ = s.ChannelMessageSendReply(m.ChannelID, friendly(err), m.Reference())
			return
		}
		embed := &discordgo.MessageEmbed{
			Title:       reward.Title,
			Description: reward.Description,
			Color:       0xe67e22,
			Footer: &discordgo.MessageEmbedFooter{
				Text: "Cosmic Chest Rewards • Pousadinha Gacha",
			},
		}
		msg := &discordgo.MessageSend{
			Embeds:    []*discordgo.MessageEmbed{embed},
			Reference: m.Reference(),
		}
		_, _ = s.ChannelMessageSendComplex(m.ChannelID, msg)
		return
	case "addcustom":
		var att *discordgo.MessageAttachment
		if len(m.Attachments) > 0 {
			att = m.Attachments[0]
		}
		isAdmin := false
		if m.Member != nil {
			isAdmin = (m.Member.Permissions & (discordgo.PermissionAdministrator | discordgo.PermissionManageServer)) != 0
		}
		res, err := HandleAddCustom(ctx, s, m.GuildID, m.ChannelID, m.Author.ID, m.Author.Username, cmd.Arg1, cmd.Arg3, att, isAdmin)
		if err != nil {
			_, _ = s.ChannelMessageSendReply(m.ChannelID, friendly(err), m.Reference())
			return
		}
		res.Reference = m.Reference()
		_, _ = s.ChannelMessageSendComplex(m.ChannelID, res)
		return
	case "mycustoms":
		res, err := HandleMyCustoms(ctx, m.GuildID, m.Author.ID)
		if err != nil {
			_, _ = s.ChannelMessageSendReply(m.ChannelID, friendly(err), m.Reference())
			return
		}
		res.Reference = m.Reference()
		_, _ = s.ChannelMessageSendComplex(m.ChannelID, res)
		return
	case "removecustom":
		isAdmin := false
		if m.Member != nil {
			isAdmin = (m.Member.Permissions & (discordgo.PermissionAdministrator | discordgo.PermissionManageServer)) != 0
		}
		res, err := HandleRemoveCustom(ctx, m.GuildID, m.Author.ID, int64(cmd.Arg2), isAdmin)
		if err != nil {
			_, _ = s.ChannelMessageSendReply(m.ChannelID, friendly(err), m.Reference())
			return
		}
		res.Reference = m.Reference()
		_, _ = s.ChannelMessageSendComplex(m.ChannelID, res)
		return
	case "customimage":
		isAdmin := false
		if m.Member != nil {
			isAdmin = (m.Member.Permissions & (discordgo.PermissionAdministrator | discordgo.PermissionManageServer)) != 0
		}
		res, err := HandleCustomImageSelect(ctx, m.GuildID, m.Author.ID, cmd.Arg1, cmd.Arg2, isAdmin)
		if err != nil {
			_, _ = s.ChannelMessageSendReply(m.ChannelID, friendly(err), m.Reference())
			return
		}
		res.Reference = m.Reference()
		_, _ = s.ChannelMessageSendComplex(m.ChannelID, res)
		return
	case "autoapprove":
		isAdmin := false
		if m.Member != nil {
			isAdmin = (m.Member.Permissions & (discordgo.PermissionAdministrator | discordgo.PermissionManageServer)) != 0
		}
		if !isAdmin {
			_, _ = s.ChannelMessageSendReply(m.ChannelID, locale.Text("gacha.config.admin_only"), m.Reference())
			return
		}
		enabled := (cmd.Arg1 == "on" || cmd.Arg1 == "true" || cmd.Arg1 == "1" || cmd.Arg1 == "yes")
		if err := Default.SetGuildAutoApprove(ctx, m.GuildID, enabled); err != nil {
			_, _ = s.ChannelMessageSendReply(m.ChannelID, fmt.Sprintf("❌ %s", err.Error()), m.Reference())
			return
		}
		statusText := "Enabled"
		if !enabled {
			statusText = "Disabled"
		}
		msg := locale.Text("gacha.config.autoapprove_updated.formatted", locale.Data{"Status": statusText})
		_, _ = s.ChannelMessageSendReply(m.ChannelID, "✅ "+msg, m.Reference())
		return
	case "builds":
		msg, err := Default.RenderBuildOverview(ctx, m.GuildID, m.Author.ID)
		if err != nil {
			_, _ = s.ChannelMessageSendReply(m.ChannelID, friendly(err), m.Reference())
			return
		}
		msg.Reference = m.Reference()
		_, _ = s.ChannelMessageSendComplex(m.ChannelID, msg)
		return
	case "learnskill":
		_, err := Default.UnlockSkill(ctx, m.GuildID, m.Author.ID, cmd.Arg1)
		if err != nil {
			_, _ = s.ChannelMessageSendReply(m.ChannelID, friendly(err), m.Reference())
			return
		}
		sk, _ := Default.GetSkillDef(cmd.Arg1)
		msg, _ := Default.RenderClassTree(ctx, m.GuildID, m.Author.ID, sk.ClassID)
		msg.Reference = m.Reference()
		_, _ = s.ChannelMessageSendComplex(m.ChannelID, msg)
		return
	case "respec":
		_, err := Default.RespecBuild(ctx, m.GuildID, m.Author.ID)
		if err != nil {
			_, _ = s.ChannelMessageSendReply(m.ChannelID, friendly(err), m.Reference())
			return
		}
		msg, _ := Default.RenderBuildOverview(ctx, m.GuildID, m.Author.ID)
		msg.Reference = m.Reference()
		msg.Content = locale.Text("gacha.builds.respec_success")
		_, _ = s.ChannelMessageSendComplex(m.ChannelID, msg)
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

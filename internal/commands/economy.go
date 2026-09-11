package commands

import (
	"bot/internal/database"
	"bot/internal/webhook"
	"bot/pkg/config"
	"bot/pkg/utils"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// ExecuteDaily processes a daily claim and returns a formatted Discord embed
func ExecuteDaily(guildID, userID string) *discordgo.MessageEmbed {
	if guildID == "" {
		return utils.ErrorEmbed("This command can only be used within a server.")
	}
	info, err := database.ClaimDaily(guildID, userID)
	if err != nil {
		if info != nil && !info.CanClaim {
			discordTime := fmt.Sprintf("<t:%d:R>", info.NextDaily.Unix())
			return utils.ErrorEmbed(fmt.Sprintf("You already collected your daily reward! Come back %s.", discordTime))
		}
		return utils.ErrorEmbed("Error claiming daily reward. Please try again.")
	}

	dayUnit := "days"
	if info.Streak == 1 {
		dayUnit = "day"
	}
	streakText := fmt.Sprintf("\n\n🔥 **Streak: %d %s**", info.Streak, dayUnit)
	if info.Streak >= 50 {
		streakText += " (MAX)"
	}
	if info.IsNewRecord && info.Streak > 1 {
		streakText += " 🏆 **New Personal Record!**"
	} else if info.MaxStreak > 0 {
		streakText += fmt.Sprintf("\n🏆 Max Streak: %d days", info.MaxStreak)
	}

	if info.StreakReset {
		streakText += "\n⚠️ *Your previous streak was reset because more than 48 hours passed.*"
	}

	return utils.SuccessEmbed("Daily Collected!",
		fmt.Sprintf("You received **%d %s**!%s", info.Reward, config.Bot.CurrencyName, streakText))
}

func CmdDaily(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.GuildID == "" {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("This command can only be used within a server."))
		return
	}
	s.ChannelMessageSendEmbed(m.ChannelID, ExecuteDaily(m.GuildID, m.Author.ID))
}

func CmdBalance(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.GuildID == "" {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("This command can only be used within a server."))
		return
	}
	targetUser := m.Author
	if len(m.Mentions) > 0 {
		targetUser = m.Mentions[0]
	}

	balance := database.GetBalance(m.GuildID, targetUser.ID)
	
	// Debug log
	log.Printf("[BALANCE] Guild: %s, User: %s (ID: %s), Balance: %d", m.GuildID, targetUser.Username, targetUser.ID, balance)
	
	s.ChannelMessageSendEmbed(m.ChannelID, utils.GoldEmbed("Balance", fmt.Sprintf("**%s** has **%d %s**.", targetUser.Username, balance, config.Bot.CurrencyName)))
}

func CmdPay(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	if m.GuildID == "" {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("This command can only be used within a server."))
		return
	}
	if len(m.Mentions) == 0 || len(args) < 2 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.InfoEmbed("Usage", "!pay @user <amount>"))
		return
	}

	toUser := m.Mentions[0]
	if toUser.ID == m.Author.ID {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("You cannot pay yourself."))
		return
	}

	var amount int
	
	// Find amount in args
	found := false
	for _, arg := range args {
		if val, err := strconv.Atoi(arg); err == nil {
			amount = val
			found = true
			break
		}
	}
	
	if !found || amount <= 0 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Invalid amount."))
		return
	}

	err := database.TransferCoins(m.GuildID, m.Author.ID, toUser.ID, amount)
	if err != nil {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Insufficient funds or transaction error."))
		return
	}

	// Trigger Webhook
	webhook.SendTransferNotification(m.Author.ID, toUser.ID, amount)

	s.ChannelMessageSendEmbed(m.ChannelID, utils.SuccessEmbed("Transfer Successful", fmt.Sprintf("You sent **%d %s** to **%s**.", amount, config.Bot.CurrencyName, toUser.Username)))
}

type leaderboardCacheEntry struct {
	timestamp time.Time
	users     []database.UserBalance
	streaks   []database.UserStreakRank
}

var (
	lbCache   = make(map[string]leaderboardCacheEntry)
	lbCacheMu sync.RWMutex
	lbTTL     = 30 * time.Second
)

func getCachedLeaderboard(guildID, category string) ([]database.UserBalance, []database.UserStreakRank, bool) {
	lbCacheMu.RLock()
	defer lbCacheMu.RUnlock()
	key := guildID + ":" + category
	entry, found := lbCache[key]
	if found && time.Since(entry.timestamp) < lbTTL {
		return entry.users, entry.streaks, true
	}
	return nil, nil, false
}

func setCachedLeaderboard(guildID, category string, users []database.UserBalance, streaks []database.UserStreakRank) {
	lbCacheMu.Lock()
	defer lbCacheMu.Unlock()
	key := guildID + ":" + category
	lbCache[key] = leaderboardCacheEntry{
		timestamp: time.Now(),
		users:     users,
		streaks:   streaks,
	}
}

func formatNumber(n int) string {
	if n < 0 {
		return "-" + formatNumber(-n)
	}
	in := strconv.Itoa(n)
	if len(in) <= 3 {
		return in
	}
	out := make([]byte, 0, len(in)+len(in)/3)
	offset := len(in) % 3
	if offset == 0 {
		offset = 3
	}
	out = append(out, in[:offset]...)
	for i := offset; i < len(in); i += 3 {
		out = append(out, ',')
		out = append(out, in[i:i+3]...)
	}
	return string(out)
}

func resolveUsername(s *discordgo.Session, guildID, userID string) string {
	if s != nil && s.State != nil && guildID != "" {
		if member, err := s.State.Member(guildID, userID); err == nil && member != nil {
			if member.Nick != "" {
				return member.Nick
			}
			if member.User != nil && member.User.Username != "" {
				return member.User.Username
			}
		}
	}
	return fmt.Sprintf("<@%s>", userID)
}

func getMedal(rank int) string {
	switch rank {
	case 1:
		return "🥇"
	case 2:
		return "🥈"
	case 3:
		return "🥉"
	default:
		return fmt.Sprintf("`#%d`", rank)
	}
}

// ExecuteLeaderboard returns a formatted leaderboard embed for net worth, wallet, or streak in a guild
func ExecuteLeaderboard(s *discordgo.Session, guildID, callerID, category string) *discordgo.MessageEmbed {
	if guildID == "" {
		return utils.ErrorEmbed("This command can only be used within a server.")
	}

	category = strings.ToLower(strings.TrimSpace(category))
	if category == "" || category == "networth" || category == "total" || category == "patrimonio" {
		category = "networth"
	} else if category == "wallet" || category == "carteira" || category == "saldo" || category == "cash" {
		category = "wallet"
	} else if category == "streak" || category == "streaks" || category == "diario" || category == "daily" {
		category = "streak"
	}

	sym := config.Bot.CurrencySymbol
	var title string
	var description strings.Builder

	switch category {
	case "streak":
		title = "🔥 Daily Streak Leaderboard"
		cachedUsers, cachedStreaks, found := getCachedLeaderboard(guildID, category)
		var streaks []database.UserStreakRank
		var err error
		if found {
			streaks = cachedStreaks
		} else {
			streaks, err = database.GetStreakLeaderboard(guildID, 10)
			if err != nil {
				return utils.ErrorEmbed("Could not retrieve streak leaderboard.")
			}
			setCachedLeaderboard(guildID, category, cachedUsers, streaks)
		}

		if len(streaks) == 0 {
			return utils.InfoEmbed("Daily Streak Leaderboard", "No active streaks found.")
		}

		for i, st := range streaks {
			name := resolveUsername(s, guildID, st.ID)
			medal := getMedal(i + 1)
			description.WriteString(fmt.Sprintf("%s %s • **🔥 %d days** (Record: %d)\n", medal, name, st.Streak, st.MaxStreak))
		}

		embed := utils.GoldEmbed(title, description.String())
		if callerID != "" {
			rank, streak, err := database.GetUserStreakRank(guildID, callerID)
			if err == nil && rank > 0 {
				embed.Footer = &discordgo.MessageEmbedFooter{
					Text: fmt.Sprintf("Your Rank: #%d • Active Streak: 🔥 %d days", rank, streak),
				}
			}
		}
		return embed

	case "wallet":
		title = "🪙 Wallet Balance Leaderboard"
		cachedUsers, _, found := getCachedLeaderboard(guildID, category)
		var users []database.UserBalance
		var err error
		if found {
			users = cachedUsers
		} else {
			users, err = database.GetWalletLeaderboard(guildID, 10)
			if err != nil {
				return utils.ErrorEmbed("Could not retrieve wallet leaderboard.")
			}
			setCachedLeaderboard(guildID, category, users, nil)
		}

		if len(users) == 0 {
			return utils.InfoEmbed("Wallet Balance Leaderboard", "No users found.")
		}

		for i, u := range users {
			name := resolveUsername(s, guildID, u.ID)
			medal := getMedal(i + 1)
			description.WriteString(fmt.Sprintf("%s %s • **%s %s**\n", medal, name, formatNumber(u.Balance), sym))
		}

		embed := utils.GoldEmbed(title, description.String())
		if callerID != "" {
			rank, nw, err := database.GetUserNetWorthAndRank(guildID, callerID)
			if err == nil && rank > 0 {
				embed.Footer = &discordgo.MessageEmbedFooter{
					Text: fmt.Sprintf("Your Rank: #%d • Net Worth: %s %s", rank, formatNumber(nw), sym),
				}
			}
		}
		return embed

	default: // "networth"
		title = "🏆 Richest Users (Net Worth)"
		cachedUsers, _, found := getCachedLeaderboard(guildID, "networth")
		var users []database.UserBalance
		var err error
		if found {
			users = cachedUsers
		} else {
			users, err = database.GetLeaderboard(guildID, 10)
			if err != nil {
				return utils.ErrorEmbed("Could not retrieve leaderboard.")
			}
			setCachedLeaderboard(guildID, "networth", users, nil)
		}

		if len(users) == 0 {
			return utils.InfoEmbed("Leaderboard", "No users found.")
		}

		for i, u := range users {
			name := resolveUsername(s, guildID, u.ID)
			medal := getMedal(i + 1)
			description.WriteString(fmt.Sprintf("%s %s • **%s %s**\n┗ 🪙 %s | 📈 %s | 💎 %s\n\n",
				medal, name, formatNumber(u.TotalNetWorth), sym,
				formatNumber(u.Balance), formatNumber(u.StockValue), formatNumber(u.CryptoValue)))
		}

		description.WriteString("🪙 = Wallet | 📈 = Stocks | 💎 = Crypto")
		embed := utils.GoldEmbed(title, description.String())
		if callerID != "" {
			rank, nw, err := database.GetUserNetWorthAndRank(guildID, callerID)
			if err == nil && rank > 0 {
				embed.Footer = &discordgo.MessageEmbedFooter{
					Text: fmt.Sprintf("Your Rank: #%d • Net Worth: %s %s", rank, formatNumber(nw), sym),
				}
			}
		}
		return embed
	}
}

// CmdLeaderboard displays the leaderboard
func CmdLeaderboard(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	if m.GuildID == "" {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("This command can only be used within a server."))
		return
	}
	category := "networth"
	if len(args) > 0 {
		category = args[0]
	}
	s.ChannelMessageSendEmbed(m.ChannelID, ExecuteLeaderboard(s, m.GuildID, m.Author.ID, category))
}
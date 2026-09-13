package commands

import (
	"bot/internal/database"
	"bot/internal/locale"

	"bot/pkg/config"
	"bot/pkg/utils"
	"fmt"

	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// ExecuteDaily processes a daily claim and returns a formatted Discord embed
func ExecuteDaily(guildID, userID string) *discordgo.MessageEmbed {
	if guildID == "" {
		return utils.ErrorEmbed(locale.Text("commands.economy.this_command_can_only_be_used_within"))
	}
	info, err := database.ClaimDaily(guildID, userID)
	if err != nil {
		if info != nil && !info.CanClaim {
			discordTime := fmt.Sprintf("<t:%d:R>", info.NextDaily.Unix())
			return utils.ErrorEmbed(locale.Text("commands.economy.you_already_collected_your_daily_reward_come.formatted", locale.Data{"DiscordTime": discordTime}))
		}
		return utils.ErrorEmbed(locale.Text("commands.economy.error_claiming_daily_reward_please_try_again"))
	}

	dayUnit := "days"
	if info.Streak == 1 {
		dayUnit = "day"
	}
	streakText := locale.Text("commands.economy.streak.formatted", locale.Data{"Streak": info.Streak, "DayUnit": dayUnit})
	if info.Streak >= 50 {
		streakText += locale.Text("commands.economy.max")
	}
	if info.IsNewRecord && info.Streak > 1 {
		streakText += locale.Text("commands.economy.new_personal_record")
	} else if info.MaxStreak > 0 {
		streakText += locale.Text("commands.economy.max_streak_days.formatted", locale.Data{"MaxStreak": info.MaxStreak})
	}

	if info.StreakReset {
		streakText += locale.Text("commands.economy.your_previous_streak_was_reset_because_more")
	}

	return utils.SuccessEmbed(locale.Text("commands.economy.daily_collected"),
		locale.Text("commands.economy.you_received.formatted", locale.Data{"Reward": info.Reward, "CurrencyName": config.Bot.CurrencyName, "StreakText": streakText}))
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
		return utils.ErrorEmbed(locale.Text("commands.economy.this_command_can_only_be_used_within"))
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
		title = locale.Text("commands.economy.daily_streak_leaderboard")
		cachedUsers, cachedStreaks, found := getCachedLeaderboard(guildID, category)
		var streaks []database.UserStreakRank
		var err error
		if found {
			streaks = cachedStreaks
		} else {
			streaks, err = database.GetStreakLeaderboard(guildID, 10)
			if err != nil {
				return utils.ErrorEmbed(locale.Text("commands.economy.could_not_retrieve_streak_leaderboard"))
			}
			setCachedLeaderboard(guildID, category, cachedUsers, streaks)
		}

		if len(streaks) == 0 {
			return utils.InfoEmbed(locale.Text("commands.economy.daily_streak_leaderboard_009686"), locale.Text("commands.economy.no_active_streaks_found"))
		}

		for i, st := range streaks {
			name := resolveUsername(s, guildID, st.ID)
			medal := getMedal(i + 1)
			description.WriteString(locale.Text("commands.economy.days_record.formatted", locale.Data{"Medal": medal, "Name": name, "Streak": st.Streak, "MaxStreak": st.MaxStreak}))
		}

		embed := utils.GoldEmbed(title, description.String())
		if callerID != "" {
			rank, streak, err := database.GetUserStreakRank(guildID, callerID)
			if err == nil && rank > 0 {
				embed.Footer = &discordgo.MessageEmbedFooter{
					Text: locale.Text("commands.economy.your_rank_active_streak_days.formatted", locale.Data{"Rank": rank, "Streak": streak}),
				}
			}
		}
		return embed

	case "wallet":
		title = locale.Text("commands.economy.wallet_balance_leaderboard")
		cachedUsers, _, found := getCachedLeaderboard(guildID, category)
		var users []database.UserBalance
		var err error
		if found {
			users = cachedUsers
		} else {
			users, err = database.GetWalletLeaderboard(guildID, 10)
			if err != nil {
				return utils.ErrorEmbed(locale.Text("commands.economy.could_not_retrieve_wallet_leaderboard"))
			}
			setCachedLeaderboard(guildID, category, users, nil)
		}

		if len(users) == 0 {
			return utils.InfoEmbed(locale.Text("commands.economy.wallet_balance_leaderboard_d0e097"), locale.Text("commands.economy.no_users_found"))
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
					Text: locale.Text("commands.economy.your_rank_net_worth.formatted", locale.Data{"Rank": rank, "Value2": formatNumber(nw), "Sym": sym}),
				}
			}
		}
		return embed

	default: // "networth"
		title = locale.Text("commands.economy.richest_users_net_worth")
		cachedUsers, _, found := getCachedLeaderboard(guildID, "networth")
		var users []database.UserBalance
		var err error
		if found {
			users = cachedUsers
		} else {
			users, err = database.GetLeaderboard(guildID, 10)
			if err != nil {
				return utils.ErrorEmbed(locale.Text("commands.economy.could_not_retrieve_leaderboard"))
			}
			setCachedLeaderboard(guildID, "networth", users, nil)
		}

		if len(users) == 0 {
			return utils.InfoEmbed(locale.Text("commands.economy.leaderboard"), locale.Text("commands.economy.no_users_found"))
		}

		for i, u := range users {
			name := resolveUsername(s, guildID, u.ID)
			medal := getMedal(i + 1)
			description.WriteString(fmt.Sprintf("%s %s • **%s %s**\n┗ 🪙 %s | 📈 %s | 💎 %s\n\n",
				medal, name, formatNumber(u.TotalNetWorth), sym,
				formatNumber(u.Balance), formatNumber(u.StockValue), formatNumber(u.CryptoValue)))
		}

		description.WriteString(locale.Text("commands.economy.wallet_stocks_crypto"))
		embed := utils.GoldEmbed(title, description.String())
		if callerID != "" {
			rank, nw, err := database.GetUserNetWorthAndRank(guildID, callerID)
			if err == nil && rank > 0 {
				embed.Footer = &discordgo.MessageEmbedFooter{
					Text: locale.Text("commands.economy.your_rank_net_worth.formatted", locale.Data{"Rank": rank, "Value2": formatNumber(nw), "Sym": sym}),
				}
			}
		}
		return embed
	}
}

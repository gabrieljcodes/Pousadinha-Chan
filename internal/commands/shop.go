package commands

import (
	"bot/internal/database"
	"bot/internal/games"
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

type voiceMuteTracker struct {
	sync.Mutex
	timers map[string]*time.Timer
}

var voiceMutes = voiceMuteTracker{
	timers: make(map[string]*time.Timer),
}

func scheduleVoiceUnmute(s *discordgo.Session, guildID, targetUserID string, minutes int) {
	key := fmt.Sprintf("%s:%s", guildID, targetUserID)

	voiceMutes.Lock()
	defer voiceMutes.Unlock()

	// If existing timer, stop it and reschedule
	if existing, found := voiceMutes.timers[key]; found && existing != nil {
		existing.Stop()
	}

	voiceMutes.timers[key] = time.AfterFunc(time.Duration(minutes)*time.Minute, func() {
		voiceMutes.Lock()
		delete(voiceMutes.timers, key)
		voiceMutes.Unlock()

		_ = s.GuildMemberMute(guildID, targetUserID, false)
		log.Printf("[VOICE MUTE] Auto-unmuted user %s in guild %s after %d minutes", targetUserID, guildID, minutes)
	})
}

// ExecuteShop generates the shop catalog embed
func ExecuteShop() *discordgo.MessageEmbed {
	sym := config.Bot.CurrencySymbol
	costNicknameSelf := config.Economy.CostNicknameSelf
	if costNicknameSelf <= 0 {
		costNicknameSelf = 500
	}
	costNicknameOther := config.Economy.CostNicknameOther
	if costNicknameOther <= 0 {
		costNicknameOther = 2000
	}
	costTimeout := config.Economy.CostPerMinutePunishment
	if costTimeout <= 0 {
		costTimeout = 500
	}
	costMute := config.Economy.CostPerMinuteMute
	if costMute <= 0 {
		costMute = 100
	}

	desc := fmt.Sprintf(`**Available Items:**

1. **Change Own Nickname**
   Cost: %d %s
   Command: `+"`!buy nickname <new name>` or `/buy nickname`"+`

2. **Change Other's Nickname**
   Cost: %d %s
   Command: `+"`!buy rename @user <new name>` or `/buy rename`"+`

3. **Timeout User (Text & Voice)**
   Cost: %d %s per minute (max 1440 min / 24h)
   Command: `+"`!buy timeout @user <minutes>` or `/buy timeout`"+`

4. **Voice Mute User (Call Only)**
   Cost: %d %s per minute (max 1440 min / 24h)
   Command: `+"`!buy mute @user <minutes>` or `/buy mute`"+`
`, costNicknameSelf, sym, costNicknameOther, sym, costTimeout, sym, costMute, sym)

	return utils.GoldEmbed(fmt.Sprintf("🛒 %s Shop", config.Bot.BotName), desc)
}

// ExecuteBuyNickname handles purchasing own nickname change
func ExecuteBuyNickname(s *discordgo.Session, guildID, userID, newName string) *discordgo.MessageEmbed {
	newName = strings.TrimSpace(newName)
	if len(newName) < 1 || len(newName) > 32 {
		return utils.ErrorEmbed("Nickname must be between 1 and 32 characters.")
	}

	cost := config.Economy.CostNicknameSelf
	if cost <= 0 {
		cost = 500
	}

	// Atomic debit before action
	if err := database.CollectLostBet(guildID, userID, cost); err != nil {
		return utils.ErrorEmbed("Insufficient funds.")
	}

	// Apply change in Discord
	err := s.GuildMemberNickname(guildID, userID, newName)
	if err != nil {
		// Automatic refund on Discord failure
		_ = database.AddCoins(guildID, userID, cost)
		return utils.ErrorEmbed("Could not change nickname. Check my permissions and role hierarchy.")
	}

	return utils.SuccessEmbed("Purchase Successful", fmt.Sprintf("Your nickname has been changed to **%s**!", newName))
}

// ExecuteBuyRename handles purchasing nickname change for another member
func ExecuteBuyRename(s *discordgo.Session, guildID, userID string, targetUser *discordgo.User, newName string) *discordgo.MessageEmbed {
	if targetUser == nil {
		return utils.ErrorEmbed("Target user not found.")
	}
	if targetUser.ID == userID {
		return ExecuteBuyNickname(s, guildID, userID, newName)
	}
	if targetUser.Bot {
		return utils.ErrorEmbed("You cannot rename bot accounts.")
	}

	newName = strings.TrimSpace(newName)
	if len(newName) < 1 || len(newName) > 32 {
		return utils.ErrorEmbed("Nickname must be between 1 and 32 characters.")
	}

	cost := config.Economy.CostNicknameOther
	if cost <= 0 {
		cost = 2000
	}

	// Atomic debit before action
	if err := database.CollectLostBet(guildID, userID, cost); err != nil {
		return utils.ErrorEmbed("Insufficient funds.")
	}

	err := s.GuildMemberNickname(guildID, targetUser.ID, newName)
	if err != nil {
		// Automatic refund on Discord failure
		_ = database.AddCoins(guildID, userID, cost)
		return utils.ErrorEmbed("Could not change nickname. Check my permissions and role hierarchy.")
	}

	return utils.SuccessEmbed("Purchase Successful", fmt.Sprintf("Nickname of **%s** changed to **%s**!", targetUser.Username, newName))
}

// ExecuteBuyTimeout handles purchasing a server-wide timeout (text and voice)
func ExecuteBuyTimeout(s *discordgo.Session, guildID, userID string, targetUser *discordgo.User, minutes int) *discordgo.MessageEmbed {
	if targetUser == nil {
		return utils.ErrorEmbed("Target user not found.")
	}
	if targetUser.ID == userID {
		return utils.ErrorEmbed("You cannot timeout yourself.")
	}
	if targetUser.Bot {
		return utils.ErrorEmbed("You cannot timeout bot accounts.")
	}
	if minutes < 1 || minutes > 1440 {
		return utils.ErrorEmbed("Timeout duration must be between 1 and 1440 minutes (max 24 hours).")
	}

	costPerMin := config.Economy.CostPerMinutePunishment
	if costPerMin <= 0 {
		costPerMin = 500
	}
	cost := minutes * costPerMin

	// Atomic debit before action
	if err := database.CollectLostBet(guildID, userID, cost); err != nil {
		return utils.ErrorEmbed(fmt.Sprintf("Insufficient funds. Cost: %d %s.", cost, config.Bot.CurrencySymbol))
	}

	// Wait if user is currently playing a game
	if games.IsUserInGame(targetUser.ID) {
		games.WaitForGameFinish(targetUser.ID)
	}

	member, err := s.GuildMember(guildID, targetUser.ID)
	if err != nil {
		_ = database.AddCoins(guildID, userID, cost)
		return utils.ErrorEmbed("Member not found in this server.")
	}

	var until time.Time
	if member.CommunicationDisabledUntil != nil && member.CommunicationDisabledUntil.After(time.Now()) {
		until = member.CommunicationDisabledUntil.Add(time.Duration(minutes) * time.Minute)
	} else {
		until = time.Now().Add(time.Duration(minutes) * time.Minute)
	}

	err = s.GuildMemberTimeout(guildID, targetUser.ID, &until)
	if err != nil {
		_ = database.AddCoins(guildID, userID, cost)
		return utils.ErrorEmbed("Could not apply timeout. Check my permissions and role hierarchy.")
	}

	return utils.SuccessEmbed("Punishment Applied!",
		fmt.Sprintf("**%s** has been timed out until %s (%d min).", targetUser.Username, until.Format("15:04:05"), minutes))
}

// ExecuteBuyMute handles purchasing a voice-only server mute
func ExecuteBuyMute(s *discordgo.Session, guildID, userID string, targetUser *discordgo.User, minutes int) *discordgo.MessageEmbed {
	if targetUser == nil {
		return utils.ErrorEmbed("Target user not found.")
	}
	if targetUser.ID == userID {
		return utils.ErrorEmbed("You cannot mute yourself.")
	}
	if targetUser.Bot {
		return utils.ErrorEmbed("You cannot mute bot accounts.")
	}
	if minutes < 1 || minutes > 1440 {
		return utils.ErrorEmbed("Mute duration must be between 1 and 1440 minutes (max 24 hours).")
	}

	costPerMin := config.Economy.CostPerMinuteMute
	if costPerMin <= 0 {
		costPerMin = 100
	}
	cost := minutes * costPerMin

	// Check if target is in a voice channel
	voiceState, err := s.State.VoiceState(guildID, targetUser.ID)
	if err != nil || voiceState == nil || voiceState.ChannelID == "" {
		return utils.ErrorEmbed(fmt.Sprintf("**%s** is not in a voice channel! You can only mute users who are currently in a call.", targetUser.Username))
	}

	// Atomic debit before action
	if err := database.CollectLostBet(guildID, userID, cost); err != nil {
		return utils.ErrorEmbed(fmt.Sprintf("Insufficient funds. Cost: %d %s.", cost, config.Bot.CurrencySymbol))
	}

	// Wait if target is currently playing a game
	if games.IsUserInGame(targetUser.ID) {
		games.WaitForGameFinish(targetUser.ID)
	}

	// Apply voice server mute
	err = s.GuildMemberMute(guildID, targetUser.ID, true)
	if err != nil {
		_ = database.AddCoins(guildID, userID, cost)
		return utils.ErrorEmbed("Could not mute user in voice. Check my permissions and role hierarchy.")
	}

	// Schedule unmute with safe timer tracker
	scheduleVoiceUnmute(s, guildID, targetUser.ID, minutes)

	return utils.SuccessEmbed("User Muted!",
		fmt.Sprintf("**%s** has been muted in voice for %d minute(s).", targetUser.Username, minutes))
}

// CmdShop displays the shop catalog
func CmdShop(s *discordgo.Session, m *discordgo.MessageCreate) {
	s.ChannelMessageSendEmbed(m.ChannelID, ExecuteShop())
}

// CmdBuy processes text purchases
func CmdBuy(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	if len(args) < 1 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.InfoEmbed("Shop", "Use `!shop` to see available items."))
		return
	}

	item := strings.ToLower(args[0])

	switch item {
	case "nickname":
		if len(args) < 2 {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Usage: `!buy nickname <new name>`"))
			return
		}
		newName := strings.Join(args[1:], " ")
		s.ChannelMessageSendEmbed(m.ChannelID, ExecuteBuyNickname(s, m.GuildID, m.Author.ID, newName))

	case "rename":
		if len(m.Mentions) == 0 || len(args) < 3 {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Usage: `!buy rename @user <new name>`"))
			return
		}
		targetUser := m.Mentions[0]
		newName := strings.Join(args[2:], " ")
		s.ChannelMessageSendEmbed(m.ChannelID, ExecuteBuyRename(s, m.GuildID, m.Author.ID, targetUser, newName))

	case "punishment", "timeout":
		if len(m.Mentions) == 0 || len(args) < 3 {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Usage: `!buy timeout @user <minutes>`"))
			return
		}
		targetUser := m.Mentions[0]
		minutes, err := strconv.Atoi(args[len(args)-1])
		if err != nil || minutes <= 0 {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Invalid minutes."))
			return
		}
		s.ChannelMessageSendEmbed(m.ChannelID, ExecuteBuyTimeout(s, m.GuildID, m.Author.ID, targetUser, minutes))

	case "mute":
		if len(m.Mentions) == 0 || len(args) < 3 {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Usage: `!buy mute @user <minutes>`"))
			return
		}
		targetUser := m.Mentions[0]
		minutes, err := strconv.Atoi(args[len(args)-1])
		if err != nil || minutes <= 0 {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Invalid minutes."))
			return
		}
		s.ChannelMessageSendEmbed(m.ChannelID, ExecuteBuyMute(s, m.GuildID, m.Author.ID, targetUser, minutes))

	default:
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Item not found. Use `!shop` to see available items."))
	}
}

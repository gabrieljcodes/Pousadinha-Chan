package commands

import (
	"bot/internal/database"
	"bot/internal/games"
	"bot/internal/locale"
	"bot/pkg/config"
	"bot/pkg/utils"
	"fmt"
	"log"

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

	desc := locale.Text("commands.shop.available_items_change_own_nickname_cost_command.formatted", locale.Data{"CostNicknameSelf": costNicknameSelf, "Sym": sym, "CostNicknameOther": costNicknameOther, "Sym4": sym, "CostTimeout": costTimeout, "Sym6": sym, "CostMute": costMute, "Sym8": sym})

	return utils.GoldEmbed(locale.Text("commands.shop.shop.formatted", locale.Data{"BotName": config.Bot.BotName}), desc)
}

// ExecuteBuyNickname handles purchasing own nickname change
func ExecuteBuyNickname(s *discordgo.Session, guildID, userID, newName string) *discordgo.MessageEmbed {
	newName = strings.TrimSpace(newName)
	if len(newName) < 1 || len(newName) > 32 {
		return utils.ErrorEmbed(locale.Text("commands.shop.nickname_must_be_between_and_characters"))
	}

	cost := config.Economy.CostNicknameSelf
	if cost <= 0 {
		cost = 500
	}

	// Atomic debit before action
	if err := database.CollectLostBet(guildID, userID, cost); err != nil {
		return utils.ErrorEmbed(locale.Text("commands.shop.insufficient_funds"))
	}

	// Apply change in Discord
	err := s.GuildMemberNickname(guildID, userID, newName)
	if err != nil {
		// Automatic refund on Discord failure
		_ = database.AddCoins(guildID, userID, cost)
		return utils.ErrorEmbed(locale.Text("commands.shop.could_not_change_nickname_check_my_permissions"))
	}

	return utils.SuccessEmbed(locale.Text("commands.shop.purchase_successful"), locale.Text("commands.shop.your_nickname_has_been_changed_to.formatted", locale.Data{"NewName": newName}))
}

// ExecuteBuyRename handles purchasing nickname change for another member
func ExecuteBuyRename(s *discordgo.Session, guildID, userID string, targetUser *discordgo.User, newName string) *discordgo.MessageEmbed {
	if targetUser == nil {
		return utils.ErrorEmbed(locale.Text("commands.shop.target_user_not_found"))
	}
	if targetUser.ID == userID {
		return ExecuteBuyNickname(s, guildID, userID, newName)
	}
	if targetUser.Bot {
		return utils.ErrorEmbed(locale.Text("commands.shop.you_cannot_rename_bot_accounts"))
	}

	newName = strings.TrimSpace(newName)
	if len(newName) < 1 || len(newName) > 32 {
		return utils.ErrorEmbed(locale.Text("commands.shop.nickname_must_be_between_and_characters"))
	}

	cost := config.Economy.CostNicknameOther
	if cost <= 0 {
		cost = 2000
	}

	// Atomic debit before action
	if err := database.CollectLostBet(guildID, userID, cost); err != nil {
		return utils.ErrorEmbed(locale.Text("commands.shop.insufficient_funds"))
	}

	err := s.GuildMemberNickname(guildID, targetUser.ID, newName)
	if err != nil {
		// Automatic refund on Discord failure
		_ = database.AddCoins(guildID, userID, cost)
		return utils.ErrorEmbed(locale.Text("commands.shop.could_not_change_nickname_check_my_permissions"))
	}

	return utils.SuccessEmbed(locale.Text("commands.shop.purchase_successful"), locale.Text("commands.shop.nickname_of_changed_to.formatted", locale.Data{"Username": targetUser.Username, "NewName": newName}))
}

// ExecuteBuyTimeout handles purchasing a server-wide timeout (text and voice)
func ExecuteBuyTimeout(s *discordgo.Session, guildID, userID string, targetUser *discordgo.User, minutes int) *discordgo.MessageEmbed {
	if targetUser == nil {
		return utils.ErrorEmbed(locale.Text("commands.shop.target_user_not_found"))
	}
	if targetUser.ID == userID {
		return utils.ErrorEmbed(locale.Text("commands.shop.you_cannot_timeout_yourself"))
	}
	if targetUser.Bot {
		return utils.ErrorEmbed(locale.Text("commands.shop.you_cannot_timeout_bot_accounts"))
	}
	if minutes < 1 || minutes > 1440 {
		return utils.ErrorEmbed(locale.Text("commands.shop.timeout_duration_must_be_between_and_minutes"))
	}

	costPerMin := config.Economy.CostPerMinutePunishment
	if costPerMin <= 0 {
		costPerMin = 500
	}
	cost := minutes * costPerMin

	// Atomic debit before action
	if err := database.CollectLostBet(guildID, userID, cost); err != nil {
		return utils.ErrorEmbed(locale.Text("commands.shop.insufficient_funds_cost.formatted", locale.Data{"Cost": cost, "CurrencySymbol": config.Bot.CurrencySymbol}))
	}

	// Wait if user is currently playing a game
	if games.IsUserInGame(targetUser.ID) {
		games.WaitForGameFinish(targetUser.ID)
	}

	member, err := s.GuildMember(guildID, targetUser.ID)
	if err != nil {
		_ = database.AddCoins(guildID, userID, cost)
		return utils.ErrorEmbed(locale.Text("commands.shop.member_not_found_in_this_server"))
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
		return utils.ErrorEmbed(locale.Text("commands.shop.could_not_apply_timeout_check_my_permissions"))
	}

	return utils.SuccessEmbed(locale.Text("commands.shop.punishment_applied"),
		locale.Text("commands.shop.has_been_timed_out_until_min.formatted", locale.Data{"Username": targetUser.Username, "Until": until.Format("15:04:05"), "Minutes": minutes}))
}

// ExecuteBuyMute handles purchasing a voice-only server mute
func ExecuteBuyMute(s *discordgo.Session, guildID, userID string, targetUser *discordgo.User, minutes int) *discordgo.MessageEmbed {
	if targetUser == nil {
		return utils.ErrorEmbed(locale.Text("commands.shop.target_user_not_found"))
	}
	if targetUser.ID == userID {
		return utils.ErrorEmbed(locale.Text("commands.shop.you_cannot_mute_yourself"))
	}
	if targetUser.Bot {
		return utils.ErrorEmbed(locale.Text("commands.shop.you_cannot_mute_bot_accounts"))
	}
	if minutes < 1 || minutes > 1440 {
		return utils.ErrorEmbed(locale.Text("commands.shop.mute_duration_must_be_between_and_minutes"))
	}

	costPerMin := config.Economy.CostPerMinuteMute
	if costPerMin <= 0 {
		costPerMin = 100
	}
	cost := minutes * costPerMin

	// Check if target is in a voice channel
	voiceState, err := s.State.VoiceState(guildID, targetUser.ID)
	if err != nil || voiceState == nil || voiceState.ChannelID == "" {
		return utils.ErrorEmbed(locale.Text("commands.shop.is_not_in_a_voice_channel_you.formatted", locale.Data{"Username": targetUser.Username}))
	}

	// Atomic debit before action
	if err := database.CollectLostBet(guildID, userID, cost); err != nil {
		return utils.ErrorEmbed(locale.Text("commands.shop.insufficient_funds_cost.formatted", locale.Data{"Cost": cost, "CurrencySymbol": config.Bot.CurrencySymbol}))
	}

	// Wait if target is currently playing a game
	if games.IsUserInGame(targetUser.ID) {
		games.WaitForGameFinish(targetUser.ID)
	}

	// Apply voice server mute
	err = s.GuildMemberMute(guildID, targetUser.ID, true)
	if err != nil {
		_ = database.AddCoins(guildID, userID, cost)
		return utils.ErrorEmbed(locale.Text("commands.shop.could_not_mute_user_in_voice_check"))
	}

	// Schedule unmute with safe timer tracker
	scheduleVoiceUnmute(s, guildID, targetUser.ID, minutes)

	return utils.SuccessEmbed(locale.Text("commands.shop.user_muted"),
		locale.Text("commands.shop.has_been_muted_in_voice_for_minute.formatted", locale.Data{"Username": targetUser.Username, "Minutes": minutes}))
}

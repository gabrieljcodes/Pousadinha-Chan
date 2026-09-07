package events

import (
	"estudocoin/pkg/config"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

func TestIsVoiceStateEligible(t *testing.T) {
	guild := &discordgo.Guild{
		ID:           "guild_123",
		AfkChannelID: "afk_channel_999",
	}

	// Case 1: Nil state
	if IsVoiceStateEligible(nil, guild, nil) {
		t.Errorf("Expected nil voice state to be ineligible")
	}

	// Case 2: Empty channel
	if IsVoiceStateEligible(nil, guild, &discordgo.VoiceState{ChannelID: ""}) {
		t.Errorf("Expected empty channel to be ineligible")
	}

	// Case 3: AFK Channel
	afkState := &discordgo.VoiceState{
		ChannelID: "afk_channel_999",
		UserID:    "user_1",
	}
	if IsVoiceStateEligible(nil, guild, afkState) {
		t.Errorf("Expected AFK channel to be ineligible")
	}

	// Case 4: Muted / Deafened
	muteCases := []struct {
		name  string
		state *discordgo.VoiceState
	}{
		{"SelfMute", &discordgo.VoiceState{ChannelID: "ch_1", UserID: "u1", SelfMute: true}},
		{"ServerMute", &discordgo.VoiceState{ChannelID: "ch_1", UserID: "u2", Mute: true}},
		{"SelfDeaf", &discordgo.VoiceState{ChannelID: "ch_1", UserID: "u3", SelfDeaf: true}},
		{"ServerDeaf", &discordgo.VoiceState{ChannelID: "ch_1", UserID: "u4", Deaf: true}},
	}
	for _, tc := range muteCases {
		if IsVoiceStateEligible(nil, guild, tc.state) {
			t.Errorf("Expected %s to be ineligible", tc.name)
		}
	}

	// Case 5: Bot User
	botCacheMu.Lock()
	botCache["bot_user_id"] = true
	botCache["human_user_id"] = false
	botCacheMu.Unlock()

	botState := &discordgo.VoiceState{
		ChannelID: "ch_1",
		UserID:    "bot_user_id",
	}
	if IsVoiceStateEligible(nil, guild, botState) {
		t.Errorf("Expected bot user to be ineligible")
	}

	// Case 6: Eligible human user
	humanState := &discordgo.VoiceState{
		ChannelID: "ch_1",
		UserID:    "human_user_id",
	}
	if !IsVoiceStateEligible(nil, guild, humanState) {
		t.Errorf("Expected unmuted human in normal channel to be eligible")
	}
}

func TestVoiceTimeAndRewardCalculation(t *testing.T) {
	config.Economy.VoiceCoinsPerMinute = 15

	coinsPerMin := config.Economy.VoiceCoinsPerMinute
	if coinsPerMin != 15 {
		t.Errorf("Expected 15, got %d", coinsPerMin)
	}

	// Test 3 minutes
	minutes := 3
	reward := minutes * coinsPerMin
	if reward != 45 {
		t.Errorf("Expected reward 45 for 3 minutes, got %d", reward)
	}

	// Test elapsed calculation
	startTime := time.Now().Add(-185 * time.Second) // 3 minutes and 5 seconds
	elapsed := time.Since(startTime)
	mins := int(elapsed.Minutes())
	if mins != 3 {
		t.Errorf("Expected 3 full minutes from 185s, got %d", mins)
	}

	// Reset config
	config.Economy.VoiceCoinsPerMinute = 10
}

func TestVoiceStateUpdateImmediateCleanup(t *testing.T) {
	voiceMu.Lock()
	activeVoiceStates["user_test_leave"] = &ActiveUserState{
		GuildID:       "guild_1",
		ChannelID:     "channel_voice_1",
		EligibleSince: time.Now(),
		LastRewarded:  time.Now(),
	}
	voiceMu.Unlock()

	// User leaves channel (ChannelID == "")
	VoiceStateUpdate(nil, &discordgo.VoiceStateUpdate{
		VoiceState: &discordgo.VoiceState{
			UserID:    "user_test_leave",
			ChannelID: "",
		},
	})

	voiceMu.Lock()
	_, exists := activeVoiceStates["user_test_leave"]
	voiceMu.Unlock()

	if exists {
		t.Errorf("Expected session to be cleaned up immediately when user leaves")
	}
}

func TestVoiceStateUpdateMuteCleanup(t *testing.T) {
	voiceMu.Lock()
	activeVoiceStates["user_test_mute"] = &ActiveUserState{
		GuildID:       "guild_1",
		ChannelID:     "channel_voice_1",
		EligibleSince: time.Now(),
		LastRewarded:  time.Now(),
	}
	voiceMu.Unlock()

	// User mutes
	VoiceStateUpdate(nil, &discordgo.VoiceStateUpdate{
		VoiceState: &discordgo.VoiceState{
			UserID:    "user_test_mute",
			ChannelID: "channel_voice_1",
			SelfMute:  true,
		},
	})

	voiceMu.Lock()
	_, exists := activeVoiceStates["user_test_mute"]
	voiceMu.Unlock()

	if exists {
		t.Errorf("Expected session to be cleaned up immediately when user mutes")
	}
}

package commands

import (
	"bot/pkg/config"
	"bot/pkg/utils"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestExecuteShopCatalog(t *testing.T) {
	// Setup test economy configuration
	config.Bot.BotName = "Pousadinha-Chan"
	config.Bot.CurrencySymbol = "EC"
	config.Economy.CostNicknameSelf = 500
	config.Economy.CostNicknameOther = 2000
	config.Economy.CostPerMinutePunishment = 500
	config.Economy.CostPerMinuteMute = 100

	embed := ExecuteShop()
	if embed == nil {
		t.Fatal("ExecuteShop returned nil embed")
	}

	if !strings.Contains(embed.Title, "Pousadinha-Chan Shop") {
		t.Errorf("Expected shop title to contain bot name, got: %s", embed.Title)
	}

	expectedParts := []string{
		"Change Own Nickname",
		"500 EC",
		"Change Other's Nickname",
		"2000 EC",
		"Timeout User (Text & Voice)",
		"500 EC per minute",
		"Voice Mute User (Call Only)",
		"100 EC per minute",
		"!buy nickname",
		"!buy rename",
		"!buy timeout",
		"!buy mute",
		"/buy nickname",
		"/buy rename",
		"/buy timeout",
		"/buy mute",
	}

	for _, part := range expectedParts {
		if !strings.Contains(embed.Description, part) {
			t.Errorf("Expected shop description to contain %q, but it did not.\nFull description:\n%s", part, embed.Description)
		}
	}
}

func TestExecuteBuyNicknameValidation(t *testing.T) {
	tests := []struct {
		name      string
		nickname  string
		wantError bool
		errMsg    string
	}{
		{
			name:      "Empty nickname",
			nickname:  "",
			wantError: true,
			errMsg:    "Nickname must be between 1 and 32 characters.",
		},
		{
			name:      "Whitespace only nickname",
			nickname:  "    ",
			wantError: true,
			errMsg:    "Nickname must be between 1 and 32 characters.",
		},
		{
			name:      "Too long nickname (33 chars)",
			nickname:  strings.Repeat("a", 33),
			wantError: true,
			errMsg:    "Nickname must be between 1 and 32 characters.",
		},
		{
			name:      "Way too long nickname (100 chars)",
			nickname:  strings.Repeat("x", 100),
			wantError: true,
			errMsg:    "Nickname must be between 1 and 32 characters.",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			embed := ExecuteBuyNickname(nil, "guild_123", "user_123", tc.nickname)
			if embed == nil {
				t.Fatal("ExecuteBuyNickname returned nil embed")
			}
			if embed.Color != utils.ColorRed {
				t.Errorf("Expected error embed color %d, got %d", utils.ColorRed, embed.Color)
			}
			if embed.Description != tc.errMsg {
				t.Errorf("Expected error %q, got %q", tc.errMsg, embed.Description)
			}
		})
	}
}

func TestExecuteBuyRenameValidation(t *testing.T) {
	tests := []struct {
		name       string
		targetUser *discordgo.User
		nickname   string
		wantError  bool
		errMsg     string
	}{
		{
			name:       "Nil target user",
			targetUser: nil,
			nickname:   "ValidName",
			wantError:  true,
			errMsg:     "Target user not found.",
		},
		{
			name: "Bot account target",
			targetUser: &discordgo.User{
				ID:  "bot_999",
				Bot: true,
			},
			nickname:  "ValidName",
			wantError: true,
			errMsg:    "You cannot rename bot accounts.",
		},
		{
			name: "Empty nickname",
			targetUser: &discordgo.User{
				ID:  "other_user_456",
				Bot: false,
			},
			nickname:  "",
			wantError: true,
			errMsg:    "Nickname must be between 1 and 32 characters.",
		},
		{
			name: "Nickname exceeds 32 characters",
			targetUser: &discordgo.User{
				ID:  "other_user_456",
				Bot: false,
			},
			nickname:  strings.Repeat("z", 33),
			wantError: true,
			errMsg:    "Nickname must be between 1 and 32 characters.",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			embed := ExecuteBuyRename(nil, "guild_123", "user_123", tc.targetUser, tc.nickname)
			if embed == nil {
				t.Fatal("ExecuteBuyRename returned nil embed")
			}
			if embed.Color != utils.ColorRed {
				t.Errorf("Expected error embed color, got %d", embed.Color)
			}
			if embed.Description != tc.errMsg {
				t.Errorf("Expected error %q, got %q", tc.errMsg, embed.Description)
			}
		})
	}
}

func TestExecuteBuyTimeoutValidation(t *testing.T) {
	tests := []struct {
		name       string
		userID     string
		targetUser *discordgo.User
		minutes    int
		errMsg     string
	}{
		{
			name:       "Nil target user",
			userID:     "caller_123",
			targetUser: nil,
			minutes:    10,
			errMsg:     "Target user not found.",
		},
		{
			name:   "Self targeting",
			userID: "user_same",
			targetUser: &discordgo.User{
				ID:  "user_same",
				Bot: false,
			},
			minutes: 10,
			errMsg:  "You cannot timeout yourself.",
		},
		{
			name:   "Bot targeting",
			userID: "caller_123",
			targetUser: &discordgo.User{
				ID:  "bot_bot",
				Bot: true,
			},
			minutes: 10,
			errMsg:  "You cannot timeout bot accounts.",
		},
		{
			name:   "Zero minutes",
			userID: "caller_123",
			targetUser: &discordgo.User{
				ID:  "victim_456",
				Bot: false,
			},
			minutes: 0,
			errMsg:  "Timeout duration must be between 1 and 1440 minutes (max 24 hours).",
		},
		{
			name:   "Negative minutes",
			userID: "caller_123",
			targetUser: &discordgo.User{
				ID:  "victim_456",
				Bot: false,
			},
			minutes: -5,
			errMsg:  "Timeout duration must be between 1 and 1440 minutes (max 24 hours).",
		},
		{
			name:   "Over 1440 minutes",
			userID: "caller_123",
			targetUser: &discordgo.User{
				ID:  "victim_456",
				Bot: false,
			},
			minutes: 1441,
			errMsg:  "Timeout duration must be between 1 and 1440 minutes (max 24 hours).",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			embed := ExecuteBuyTimeout(nil, "guild_123", tc.userID, tc.targetUser, tc.minutes)
			if embed == nil {
				t.Fatal("ExecuteBuyTimeout returned nil embed")
			}
			if embed.Color != utils.ColorRed {
				t.Errorf("Expected error embed color, got %d", embed.Color)
			}
			if embed.Description != tc.errMsg {
				t.Errorf("Expected error %q, got %q", tc.errMsg, embed.Description)
			}
		})
	}
}

func TestExecuteBuyMuteValidation(t *testing.T) {
	tests := []struct {
		name       string
		userID     string
		targetUser *discordgo.User
		minutes    int
		errMsg     string
	}{
		{
			name:       "Nil target user",
			userID:     "caller_123",
			targetUser: nil,
			minutes:    10,
			errMsg:     "Target user not found.",
		},
		{
			name:   "Self targeting",
			userID: "user_same",
			targetUser: &discordgo.User{
				ID:  "user_same",
				Bot: false,
			},
			minutes: 10,
			errMsg:  "You cannot mute yourself.",
		},
		{
			name:   "Bot targeting",
			userID: "caller_123",
			targetUser: &discordgo.User{
				ID:  "bot_bot",
				Bot: true,
			},
			minutes: 10,
			errMsg:  "You cannot mute bot accounts.",
		},
		{
			name:   "Zero minutes",
			userID: "caller_123",
			targetUser: &discordgo.User{
				ID:  "victim_456",
				Bot: false,
			},
			minutes: 0,
			errMsg:  "Mute duration must be between 1 and 1440 minutes (max 24 hours).",
		},
		{
			name:   "Negative minutes",
			userID: "caller_123",
			targetUser: &discordgo.User{
				ID:  "victim_456",
				Bot: false,
			},
			minutes: -10,
			errMsg:  "Mute duration must be between 1 and 1440 minutes (max 24 hours).",
		},
		{
			name:   "Over 1440 minutes",
			userID: "caller_123",
			targetUser: &discordgo.User{
				ID:  "victim_456",
				Bot: false,
			},
			minutes: 2000,
			errMsg:  "Mute duration must be between 1 and 1440 minutes (max 24 hours).",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			embed := ExecuteBuyMute(nil, "guild_123", tc.userID, tc.targetUser, tc.minutes)
			if embed == nil {
				t.Fatal("ExecuteBuyMute returned nil embed")
			}
			if embed.Color != utils.ColorRed {
				t.Errorf("Expected error embed color, got %d", embed.Color)
			}
			if embed.Description != tc.errMsg {
				t.Errorf("Expected error %q, got %q", tc.errMsg, embed.Description)
			}
		})
	}
}

func TestVoiceMuteTrackerConcurrency(t *testing.T) {
	session := &discordgo.Session{}
	guildID := "test_guild"
	userID := "test_user_789"

	scheduleVoiceUnmute(session, guildID, userID, 60)

	key := guildID + ":" + userID
	voiceMutes.Lock()
	timer, exists := voiceMutes.timers[key]
	voiceMutes.Unlock()

	if !exists || timer == nil {
		t.Fatal("Expected timer to be tracked in voiceMutes")
	}

	// Reschedule with different duration
	scheduleVoiceUnmute(session, guildID, userID, 120)

	voiceMutes.Lock()
	timer2, exists2 := voiceMutes.timers[key]
	voiceMutes.Unlock()

	if !exists2 || timer2 == nil {
		t.Fatal("Expected new timer to be tracked in voiceMutes")
	}

	// Clean up
	timer2.Stop()
	voiceMutes.Lock()
	delete(voiceMutes.timers, key)
	voiceMutes.Unlock()
}

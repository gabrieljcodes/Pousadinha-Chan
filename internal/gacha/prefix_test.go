package gacha

import (
	"context"
	"testing"
)

func TestParsePrefixCommand(t *testing.T) {
	tests := []struct {
		input    string
		wantPool string
		wantCmd  string
		wantOk   bool
	}{
		// Waifu shortcuts
		{"!w", "w", "roll", true},
		{"!W", "w", "roll", true},
		{"!wa", "wa", "roll", true},
		{"!wg", "wg", "roll", true},
		{"!w extra characters ignored", "w", "roll", true},

		// Husbando shortcuts
		{"!h", "h", "roll", true},
		{"!ha", "ha", "roll", true},
		{"!hg", "hg", "roll", true},

		// Mixed / All shortcuts
		{"!m", "roll", "roll", true},
		{"!mu", "roll", "roll", true},
		{"!ma", "ma", "roll", true},
		{"!mg", "mg", "roll", true},

		// Roll command with optional arguments
		{"!roll", "roll", "roll", true},
		{"!r", "roll", "roll", true},
		{"!roll w", "w", "roll", true},
		{"!roll waifu", "w", "roll", true},
		{"!roll female", "w", "roll", true},
		{"!roll h", "h", "roll", true},
		{"!roll husbando", "h", "roll", true},
		{"!roll male", "h", "roll", true},
		{"!roll wa", "wa", "roll", true},
		{"!roll ha", "ha", "roll", true},
		{"!roll wg", "wg", "roll", true},
		{"!roll hg", "hg", "roll", true},
		{"!roll ma", "ma", "roll", true},
		{"!roll mg", "mg", "roll", true},
		{"!roll all", "roll", "roll", true},
		{"!r w", "w", "roll", true},
		{"!r hg", "hg", "roll", true},

		// Status shortcuts
		{"!tu", "", "status", true},
		{"!rolls", "", "status", true},
		{"!quota", "", "status", true},

		// Non-gacha or invalid prefix commands (must be ignored)
		{"!help", "", "", false},
		{"!play song", "", "", false},
		{"!ban @user", "", "", false},
		{"hello", "", "", false},
		{"/roll", "", "", false},
		{"?w", "", "", false},
		{"!", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			cmd, ok := parsePrefixCommand(tt.input)
			if ok != tt.wantOk {
				t.Fatalf("parsePrefixCommand(%q) ok = %v, want %v", tt.input, ok, tt.wantOk)
			}
			if ok {
				if cmd.Pool != tt.wantPool {
					t.Errorf("parsePrefixCommand(%q) Pool = %q, want %q", tt.input, cmd.Pool, tt.wantPool)
				}
				if cmd.SubCmd != tt.wantCmd {
					t.Errorf("parsePrefixCommand(%q) SubCmd = %q, want %q", tt.input, cmd.SubCmd, tt.wantCmd)
				}
			}
		})
	}
}

func TestIsRollAction(t *testing.T) {
	rollActions := []string{"roll", "w", "h", "wa", "ha", "ma", "wg", "hg", "mg"}
	for _, a := range rollActions {
		if !isRollAction(a) {
			t.Errorf("isRollAction(%q) = false, want true", a)
		}
	}

	nonRollActions := []string{"harem", "profile", "top", "wishlist", "wishes", "trade", "gift", "divorce", "alias"}
	for _, a := range nonRollActions {
		if isRollAction(a) {
			t.Errorf("isRollAction(%q) = true, want false", a)
		}
	}
}

func TestChannelValidation(t *testing.T) {
	store := &Store{
		scheduleCache: make(map[string]ResetSchedule),
	}
	ctx := context.Background()
	guildID := "guild-channel-test"

	// 1. Unconfigured channels: should reject both rolls and commands
	store.setCachedSchedule(guildID, ResetSchedule{
		ResetMinute:   0,
		RollsPerHour:  10,
		ClaimHours:    3,
		RollChannelID: "",
		CmdChannelID:  "",
	})

	if err := store.validateChannel(ctx, guildID, "ch-1", "roll"); err == nil {
		t.Errorf("expected error for unconfigured roll channel, got nil")
	}
	if err := store.validateChannel(ctx, guildID, "ch-1", "harem"); err == nil {
		t.Errorf("expected error for unconfigured command channel, got nil")
	}

	// 2. Configured channels
	rollCh := "channel-rolls"
	cmdCh := "channel-commands"
	store.setCachedSchedule(guildID, ResetSchedule{
		ResetMinute:   0,
		RollsPerHour:  10,
		ClaimHours:    3,
		RollChannelID: rollCh,
		CmdChannelID:  cmdCh,
	})

	// Roll in roll channel -> OK
	if err := store.validateChannel(ctx, guildID, rollCh, "roll"); err != nil {
		t.Errorf("expected roll in roll channel to be allowed, got error: %v", err)
	}
	if err := store.validateChannel(ctx, guildID, rollCh, "w"); err != nil {
		t.Errorf("expected !w in roll channel to be allowed, got error: %v", err)
	}

	// Roll in command channel -> Rejected
	if err := store.validateChannel(ctx, guildID, cmdCh, "roll"); err == nil {
		t.Errorf("expected roll in command channel to be rejected, got nil")
	}

	// Command in command channel -> OK
	if err := store.validateChannel(ctx, guildID, cmdCh, "harem"); err != nil {
		t.Errorf("expected harem in command channel to be allowed, got error: %v", err)
	}
	if err := store.validateChannel(ctx, guildID, cmdCh, "top"); err != nil {
		t.Errorf("expected top in command channel to be allowed, got error: %v", err)
	}

	// Command in roll channel -> Rejected
	if err := store.validateChannel(ctx, guildID, rollCh, "harem"); err == nil {
		t.Errorf("expected harem in roll channel to be rejected, got nil")
	}

	// Either in random other channel -> Rejected
	if err := store.validateChannel(ctx, guildID, "random-channel", "roll"); err == nil {
		t.Errorf("expected roll in random channel to be rejected, got nil")
	}
	if err := store.validateChannel(ctx, guildID, "random-channel", "harem"); err == nil {
		t.Errorf("expected harem in random channel to be rejected, got nil")
	}

	// gachaconfig admin command -> always allowed everywhere
	if err := store.validateChannel(ctx, guildID, "random-channel", "gachaconfig"); err != nil {
		t.Errorf("expected gachaconfig to be allowed anywhere, got error: %v", err)
	}
}

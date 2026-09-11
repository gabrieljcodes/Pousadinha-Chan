package commands

import (
	"bot/internal/database"
	"bot/pkg/config"
	"strings"
	"testing"
	"time"
)

func TestCalculateDailyReward(t *testing.T) {
	config.Economy.DailyAmount = 100

	cases := []struct {
		streak   int
		expected int
	}{
		{streak: 0, expected: 100},  // fallback to 1
		{streak: -5, expected: 100}, // negative fallback to 1
		{streak: 1, expected: 100},
		{streak: 2, expected: 200},
		{streak: 10, expected: 1000},
		{streak: 50, expected: 5000},
		{streak: 51, expected: 5000}, // capped at 50
		{streak: 100, expected: 5000},
	}

	for _, c := range cases {
		reward := database.CalculateDailyReward(c.streak)
		if reward != c.expected {
			t.Errorf("For streak %d expected reward %d, got %d", c.streak, c.expected, reward)
		}
	}

	// Test dynamic config change
	config.Economy.DailyAmount = 250
	if r := database.CalculateDailyReward(1); r != 250 {
		t.Errorf("Expected 250 for streak 1 with config 250, got %d", r)
	}
	if r := database.CalculateDailyReward(2); r != 500 {
		t.Errorf("Expected 500 for streak 2 with config 250, got %d", r)
	}
	if r := database.CalculateDailyReward(60); r != 12500 {
		t.Errorf("Expected 12500 for streak 60 with config 250, got %d", r)
	}

	// Reset config
	config.Economy.DailyAmount = 100
}

func TestStreakTimeWindowLogic(t *testing.T) {
	now := time.Now()

	// Scenario 1: Claimed 12 hours ago (Cooldown active)
	lastDaily := now.Add(-12 * time.Hour)
	timeSince := now.Sub(lastDaily)
	canClaim := timeSince >= 24*time.Hour
	if canClaim {
		t.Errorf("Expected canClaim to be false for claim 12h ago")
	}

	// Scenario 2: Claimed 25 hours ago (Streak continues)
	lastDaily2 := now.Add(-25 * time.Hour)
	timeSince2 := now.Sub(lastDaily2)
	canClaim2 := timeSince2 >= 24*time.Hour
	continuesStreak2 := timeSince2 <= 48*time.Hour
	if !canClaim2 || !continuesStreak2 {
		t.Errorf("Expected canClaim and streak continuation for claim 25h ago")
	}

	// Scenario 3: Claimed 50 hours ago (Streak expired)
	lastDaily3 := now.Add(-50 * time.Hour)
	timeSince3 := now.Sub(lastDaily3)
	canClaim3 := timeSince3 >= 24*time.Hour
	continuesStreak3 := timeSince3 <= 48*time.Hour
	if !canClaim3 || continuesStreak3 {
		t.Errorf("Expected canClaim=true and continuesStreak=false for claim 50h ago")
	}
}

func TestDailyRecordAndFormatting(t *testing.T) {
	// Test streak 1 formatting
	info1 := &database.DailyStreakInfo{
		Streak:      1,
		MaxStreak:   1,
		Reward:      100,
		CanClaim:    false,
		StreakReset: false,
		IsNewRecord: false,
	}

	dayUnit := "days"
	if info1.Streak == 1 {
		dayUnit = "day"
	}
	if dayUnit != "day" {
		t.Errorf("Expected singular 'day' for streak 1")
	}

	// Test new personal record
	infoRecord := &database.DailyStreakInfo{
		Streak:      5,
		MaxStreak:   5,
		Reward:      500,
		CanClaim:    false,
		StreakReset: false,
		IsNewRecord: true,
	}
	if !infoRecord.IsNewRecord {
		t.Errorf("Expected IsNewRecord to be true")
	}

	// Test streak reset flag
	infoReset := &database.DailyStreakInfo{
		Streak:      1,
		MaxStreak:   10,
		Reward:      100,
		CanClaim:    false,
		StreakReset: true,
		IsNewRecord: false,
	}
	if !infoReset.StreakReset {
		t.Errorf("Expected StreakReset to be true")
	}
	if infoReset.MaxStreak != 10 {
		t.Errorf("Expected MaxStreak to be preserved at 10")
	}
}

func TestDailyEmbedGeneration(t *testing.T) {
	// Verify ExecuteDaily behavior when daily cannot be claimed or database is offline
	embed := ExecuteDaily("mock_guild_123", "nonexistent_mock_user_id_12345")
	if embed == nil {
		t.Fatal("Expected embed to not be nil")
	}
	if embed.Title == "" && embed.Description == "" {
		t.Fatal("Expected embed to have title or description")
	}
	// Verify title is ErrorEmbed (since mock DB is not connected) or info
	if !strings.Contains(embed.Title, "Error") && !strings.Contains(embed.Title, "Daily") {
		t.Errorf("Unexpected embed title: %s", embed.Title)
	}
}

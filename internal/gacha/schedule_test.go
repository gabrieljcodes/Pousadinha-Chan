package gacha

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestResetScheduleRollWindow(t *testing.T) {
	sch := ResetSchedule{
		ResetMinute:  35,
		RollsPerHour: 10,
		ClaimHours:   3,
	}

	tests := []struct {
		name         string
		now          time.Time
		wantStart    time.Time
		wantNextReset time.Time
	}{
		{
			name:         "exact reset moment :35:00",
			now:          time.Date(2026, 9, 14, 4, 35, 0, 0, time.UTC),
			wantStart:    time.Date(2026, 9, 14, 4, 35, 0, 0, time.UTC),
			wantNextReset: time.Date(2026, 9, 14, 5, 35, 0, 0, time.UTC),
		},
		{
			name:         "inside window before next reset at :34:59",
			now:          time.Date(2026, 9, 14, 4, 34, 59, 0, time.UTC),
			wantStart:    time.Date(2026, 9, 14, 3, 35, 0, 0, time.UTC),
			wantNextReset: time.Date(2026, 9, 14, 4, 35, 0, 0, time.UTC),
		},
		{
			name:         "inside window mid-hour at :50:00",
			now:          time.Date(2026, 9, 14, 4, 50, 0, 0, time.UTC),
			wantStart:    time.Date(2026, 9, 14, 4, 35, 0, 0, time.UTC),
			wantNextReset: time.Date(2026, 9, 14, 5, 35, 0, 0, time.UTC),
		},
		{
			name:         "midnight boundary :00:15",
			now:          time.Date(2026, 9, 14, 0, 15, 0, 0, time.UTC),
			wantStart:    time.Date(2026, 9, 13, 23, 35, 0, 0, time.UTC),
			wantNextReset: time.Date(2026, 9, 14, 0, 35, 0, 0, time.UTC),
		},
		{
			name:         "new year boundary 2026-01-01 00:10:00",
			now:          time.Date(2026, 1, 1, 0, 10, 0, 0, time.UTC),
			wantStart:    time.Date(2025, 12, 31, 23, 35, 0, 0, time.UTC),
			wantNextReset: time.Date(2026, 1, 1, 0, 35, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			win := sch.RollWindow(tt.now)
			if !win.CurrentStart.Equal(tt.wantStart) {
				t.Errorf("CurrentStart = %v, want %v", win.CurrentStart, tt.wantStart)
			}
			if !win.NextReset.Equal(tt.wantNextReset) {
				t.Errorf("NextReset = %v, want %v", win.NextReset, tt.wantNextReset)
			}
			if !tt.now.Before(win.NextReset) || tt.now.Before(win.CurrentStart) {
				t.Errorf("now (%v) is not within [CurrentStart %v, NextReset %v)", tt.now, win.CurrentStart, win.NextReset)
			}
		})
	}
}

func TestResetScheduleClaimWindow(t *testing.T) {
	sch := ResetSchedule{
		ResetMinute:  35,
		RollsPerHour: 10,
		ClaimHours:   3,
	}

	tests := []struct {
		name          string
		now           time.Time
		wantStart     time.Time
		wantNextReset time.Time
	}{
		{
			name:          "exact claim reset at 03:35:00",
			now:           time.Date(2026, 9, 14, 3, 35, 0, 0, time.UTC),
			wantStart:     time.Date(2026, 9, 14, 3, 35, 0, 0, time.UTC),
			wantNextReset: time.Date(2026, 9, 14, 6, 35, 0, 0, time.UTC),
		},
		{
			name:          "mid-cycle at 04:35:00 (roll reset, but not claim reset)",
			now:           time.Date(2026, 9, 14, 4, 35, 0, 0, time.UTC),
			wantStart:     time.Date(2026, 9, 14, 3, 35, 0, 0, time.UTC),
			wantNextReset: time.Date(2026, 9, 14, 6, 35, 0, 0, time.UTC),
		},
		{
			name:          "just before next claim reset at 06:34:59",
			now:           time.Date(2026, 9, 14, 6, 34, 59, 0, time.UTC),
			wantStart:     time.Date(2026, 9, 14, 3, 35, 0, 0, time.UTC),
			wantNextReset: time.Date(2026, 9, 14, 6, 35, 0, 0, time.UTC),
		},
		{
			name:          "at next claim reset at 06:35:00",
			now:           time.Date(2026, 9, 14, 6, 35, 0, 0, time.UTC),
			wantStart:     time.Date(2026, 9, 14, 6, 35, 0, 0, time.UTC),
			wantNextReset: time.Date(2026, 9, 14, 9, 35, 0, 0, time.UTC),
		},
		{
			name:          "claim window across midnight: 00:20:00",
			now:           time.Date(2026, 9, 14, 0, 20, 0, 0, time.UTC),
			wantStart:     time.Date(2026, 9, 13, 21, 35, 0, 0, time.UTC),
			wantNextReset: time.Date(2026, 9, 14, 0, 35, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			win := sch.ClaimWindow(tt.now)
			if !win.CurrentStart.Equal(tt.wantStart) {
				t.Errorf("CurrentStart = %v, want %v", win.CurrentStart, tt.wantStart)
			}
			if !win.NextReset.Equal(tt.wantNextReset) {
				t.Errorf("NextReset = %v, want %v", win.NextReset, tt.wantNextReset)
			}
			if !tt.now.Before(win.NextReset) || tt.now.Before(win.CurrentStart) {
				t.Errorf("now (%v) is not within [CurrentStart %v, NextReset %v)", tt.now, win.CurrentStart, win.NextReset)
			}
		})
	}
}

func TestResetScheduleDefaultMinute0(t *testing.T) {
	sch := ResetSchedule{
		ResetMinute:  0,
		RollsPerHour: 10,
		ClaimHours:   3,
	}

	now := time.Date(2026, 9, 14, 4, 15, 0, 0, time.UTC)
	rWin := sch.RollWindow(now)
	if !rWin.CurrentStart.Equal(time.Date(2026, 9, 14, 4, 0, 0, 0, time.UTC)) {
		t.Errorf("Roll CurrentStart = %v, want 04:00", rWin.CurrentStart)
	}
	if !rWin.NextReset.Equal(time.Date(2026, 9, 14, 5, 0, 0, 0, time.UTC)) {
		t.Errorf("Roll NextReset = %v, want 05:00", rWin.NextReset)
	}

	cWin := sch.ClaimWindow(now)
	if !cWin.CurrentStart.Equal(time.Date(2026, 9, 14, 3, 0, 0, 0, time.UTC)) {
		t.Errorf("Claim CurrentStart = %v, want 03:00", cWin.CurrentStart)
	}
	if !cWin.NextReset.Equal(time.Date(2026, 9, 14, 6, 0, 0, 0, time.UTC)) {
		t.Errorf("Claim NextReset = %v, want 06:00", cWin.NextReset)
	}
}

func testScheduleIntegration(t *testing.T, s *Store) {
	ctx := context.Background()

	// 1. Default schedule
	def := s.GuildSchedule(ctx, "test-guild-default")
	if def.ResetMinute != s.Config.ResetMinute || def.RollsPerHour != s.Config.RollsPerHour || def.ClaimHours != s.Config.ClaimHours {
		t.Fatalf("unexpected default schedule: %+v", def)
	}

	// 2. Set custom schedule
	if err := s.SetGuildSchedule(ctx, "test-guild-custom", 35, 12, 4); err != nil {
		t.Fatalf("SetGuildSchedule failed: %v", err)
	}

	// Read from cache
	cached := s.GuildSchedule(ctx, "test-guild-custom")
	if cached.ResetMinute != 35 || cached.RollsPerHour != 12 || cached.ClaimHours != 4 {
		t.Fatalf("unexpected cached schedule: %+v", cached)
	}

	// Clear cache and read from DB directly
	s.clearCachedSchedule("test-guild-custom")
	fromDB := s.GuildSchedule(ctx, "test-guild-custom")
	if fromDB.ResetMinute != 35 || fromDB.RollsPerHour != 12 || fromDB.ClaimHours != 4 {
		t.Fatalf("unexpected fromDB schedule: %+v", fromDB)
	}

	// 3. Validation errors
	if err := s.SetGuildSchedule(ctx, "test-guild-custom", 60, 10, 3); err == nil {
		t.Fatal("expected error on minute 60")
	}
	if err := s.SetGuildSchedule(ctx, "test-guild-custom", 35, 0, 3); err == nil {
		t.Fatal("expected error on rolls 0")
	}
	if err := s.SetGuildSchedule(ctx, "test-guild-custom", 35, 10, 0); err == nil {
		t.Fatal("expected error on claim hours 0")
	}

	// 4. Reset schedule back to default
	if err := s.ResetGuildSchedule(ctx, "test-guild-custom"); err != nil {
		t.Fatalf("ResetGuildSchedule failed: %v", err)
	}
	afterReset := s.GuildSchedule(ctx, "test-guild-custom")
	if afterReset.ResetMinute != s.Config.ResetMinute || afterReset.RollsPerHour != s.Config.RollsPerHour {
		t.Fatalf("expected reset to default, got: %+v", afterReset)
	}

	// 5. Fixed-window roll reset behavior
	sch := s.GuildSchedule(ctx, "guild-roll-win")
	rWin := sch.RollWindow(time.Now())

	// Perform rolls up to limit
	for i := 0; i < sch.RollsPerHour; i++ {
		_, err := s.Roll(ctx, "guild-roll-win", "channel", "bob", fmt.Sprintf("req-%d", i))
		if err != nil {
			t.Fatalf("roll %d failed: %v", i, err)
		}
	}
	// Next roll must exceed limit
	_, err := s.Roll(ctx, "guild-roll-win", "channel", "bob", "req-limit")
	if err != ErrLimit {
		t.Fatalf("expected ErrLimit, got: %v", err)
	}

	// Simulate passage into previous window: set player window_start to before current window
	_, err = s.DB.ExecContext(ctx, `UPDATE gacha_players SET window_start=$1 WHERE guild_id='guild-roll-win' AND user_id='bob'`, rWin.CurrentStart.Add(-2*time.Hour))
	if err != nil {
		t.Fatalf("simulating previous window failed: %v", err)
	}

	// Next roll must succeed because a new window began, resetting rolls_used
	_, err = s.Roll(ctx, "guild-roll-win", "channel", "bob", "req-new-win")
	if err != nil {
		t.Fatalf("roll in new window should succeed, got: %v", err)
	}

	var rollsUsed int
	_ = s.DB.QueryRowContext(ctx, `SELECT rolls_used FROM gacha_players WHERE guild_id='guild-roll-win' AND user_id='bob'`).Scan(&rollsUsed)
	if rollsUsed != 1 {
		t.Fatalf("rolls_used in new window should be 1, got %d", rollsUsed)
	}

	// 6. Test Gem Claim in new window should NOT block rolling (bug fix verification)
	// Simulate: player used all rolls, then next window arrives. Player claims a gem (or window_start is advanced).
	// Set rolls_used=10, window_start=rWin.CurrentStart (as happened in the bug)
	_, err = s.DB.ExecContext(ctx, `UPDATE gacha_players SET rolls_used=10, window_start=$1 WHERE guild_id='guild-roll-win' AND user_id='bob'`, rWin.CurrentStart)
	if err != nil {
		t.Fatalf("failed setting up bug scenario: %v", err)
	}

	// Check PlayerRollStatus: since actual rolls in current window is only 1 (from req-new-win),
	// PlayerRollStatus must reconcile rolls_used to 1, leaving sch.RollsPerHour-1 rolls left!
	rLeft, maxR, _, _, err := s.PlayerRollStatus(ctx, "guild-roll-win", "bob", time.Now())
	if err != nil {
		t.Fatalf("PlayerRollStatus failed: %v", err)
	}
	if maxR != sch.RollsPerHour {
		t.Fatalf("expected maxRolls=%d, got %d", sch.RollsPerHour, maxR)
	}
	if rLeft != sch.RollsPerHour-1 {
		t.Fatalf("expected rollsLeft=%d after reconciliation, got %d", sch.RollsPerHour-1, rLeft)
	}

	// Roll must succeed and NOT return ErrLimit!
	_, err = s.Roll(ctx, "guild-roll-win", "channel", "bob", "req-healed-roll")
	if err != nil {
		t.Fatalf("roll after gem claim in new window must succeed, got: %v", err)
	}

	// 7. Test PlayerMaxRollsPerHour and PlayerRollStatus with lootbox extra_permanent_rolls and stored rolls
	_, err = s.DB.ExecContext(ctx, `UPDATE gacha_players SET extra_permanent_rolls=2, stored_extra_rolls=3 WHERE guild_id='guild-roll-win' AND user_id='bob'`)
	if err != nil {
		t.Fatalf("failed setting extra_permanent_rolls: %v", err)
	}

	maxRollsPerHour := s.PlayerMaxRollsPerHour(ctx, "guild-roll-win", "bob")
	if maxRollsPerHour != sch.RollsPerHour+2 {
		t.Fatalf("expected PlayerMaxRollsPerHour=%d, got %d", sch.RollsPerHour+2, maxRollsPerHour)
	}

	rLeft2, maxR2, stored2, _, err := s.PlayerRollStatus(ctx, "guild-roll-win", "bob", time.Now())
	if err != nil {
		t.Fatalf("PlayerRollStatus failed: %v", err)
	}
	if maxR2 != sch.RollsPerHour+2 {
		t.Fatalf("expected maxRolls=%d, got %d", sch.RollsPerHour+2, maxR2)
	}
	if stored2 != 3 {
		t.Fatalf("expected storedRolls=3, got %d", stored2)
	}
	// actual rolls in window so far: 2 (req-new-win, req-healed-roll)
	expectedLeft := (maxR2 - 2) + 3
	if rLeft2 != expectedLeft {
		t.Fatalf("expected rollsLeft=%d, got %d", expectedLeft, rLeft2)
	}
}


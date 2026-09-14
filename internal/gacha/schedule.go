package gacha

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// ResetSchedule holds the synchronized reset configuration and channel settings for a guild.
type ResetSchedule struct {
	ResetMinute   int    // 0..59: minute past the hour when rolls reset
	RollsPerHour  int    // 1..100: rolls allocated per hourly window
	ClaimHours    int    // 1..24: interval in hours/resets between claims
	RollChannelID string // channel where rolls are allowed
	CmdChannelID  string // channel where other gacha commands are allowed
}

// WindowInfo contains the exact boundaries of a reset window.
type WindowInfo struct {
	CurrentStart time.Time
	NextReset    time.Time
}

// RollWindow returns the start of the current roll window and the exact next reset time.
// All calculations are done in UTC for universal consistency across leap years and day boundaries.
func (s ResetSchedule) RollWindow(now time.Time) WindowInfo {
	now = now.UTC()
	m := s.ResetMinute
	if m < 0 || m > 59 {
		m = 0
	}
	unixSec := now.Unix() - int64(m*60)
	unixHour := unixSec / 3600
	if unixSec < 0 && unixSec%3600 != 0 {
		unixHour--
	}
	startSec := unixHour*3600 + int64(m*60)
	currentStart := time.Unix(startSec, 0).UTC()
	nextReset := currentStart.Add(1 * time.Hour)
	return WindowInfo{
		CurrentStart: currentStart,
		NextReset:    nextReset,
	}
}

// ClaimWindow returns the start of the current claim window and the exact next claim reset time.
func (s ResetSchedule) ClaimWindow(now time.Time) WindowInfo {
	now = now.UTC()
	m := s.ResetMinute
	if m < 0 || m > 59 {
		m = 0
	}
	h := int64(s.ClaimHours)
	if h < 1 {
		h = 3
	}
	unixSec := now.Unix() - int64(m*60)
	unixHour := unixSec / 3600
	if unixSec < 0 && unixSec%3600 != 0 {
		unixHour--
	}
	blockHour := (unixHour / h) * h
	if unixHour < 0 && unixHour%h != 0 {
		blockHour -= h
	}
	startSec := blockHour*3600 + int64(m*60)
	currentStart := time.Unix(startSec, 0).UTC()
	nextReset := currentStart.Add(time.Duration(h) * time.Hour)
	return WindowInfo{
		CurrentStart: currentStart,
		NextReset:    nextReset,
	}
}

func (s *Store) getCachedSchedule(guildID string) (ResetSchedule, bool) {
	s.scheduleMu.RLock()
	defer s.scheduleMu.RUnlock()
	if s.scheduleCache == nil {
		return ResetSchedule{}, false
	}
	sch, ok := s.scheduleCache[guildID]
	return sch, ok
}

func (s *Store) setCachedSchedule(guildID string, sch ResetSchedule) {
	s.scheduleMu.Lock()
	defer s.scheduleMu.Unlock()
	if s.scheduleCache == nil {
		s.scheduleCache = make(map[string]ResetSchedule)
	}
	s.scheduleCache[guildID] = sch
}

func (s *Store) clearCachedSchedule(guildID string) {
	s.scheduleMu.Lock()
	defer s.scheduleMu.Unlock()
	if s.scheduleCache != nil {
		delete(s.scheduleCache, guildID)
	}
}

// DefaultSchedule returns the fallback schedule based on Store config.
func (s *Store) DefaultSchedule() ResetSchedule {
	rolls := s.Config.RollsPerHour
	if rolls <= 0 {
		rolls = 10
	}
	claims := s.Config.ClaimHours
	if claims <= 0 {
		claims = 3
	}
	minute := s.Config.ResetMinute
	if minute < 0 || minute > 59 {
		minute = 0
	}
	return ResetSchedule{
		ResetMinute:  minute,
		RollsPerHour: rolls,
		ClaimHours:   claims,
	}
}

// GuildSchedule retrieves the schedule for a given guild from cache or database.
func (s *Store) GuildSchedule(ctx context.Context, guildID string) ResetSchedule {
	if sch, ok := s.getCachedSchedule(guildID); ok {
		return sch
	}

	def := s.DefaultSchedule()
	if s.DB == nil || guildID == "" {
		return def
	}

	var minute, rolls, claimHours int
	var rollChannel, cmdChannel sql.NullString
	err := s.DB.QueryRowContext(ctx, `SELECT reset_minute, rolls_per_hour, claim_hours, roll_channel_id, cmd_channel_id FROM gacha_guild_settings WHERE guild_id=$1`, guildID).Scan(&minute, &rolls, &claimHours, &rollChannel, &cmdChannel)
	if err == nil {
		sch := ResetSchedule{
			ResetMinute:   minute,
			RollsPerHour:  rolls,
			ClaimHours:    claimHours,
			RollChannelID: rollChannel.String,
			CmdChannelID:  cmdChannel.String,
		}
		s.setCachedSchedule(guildID, sch)
		return sch
	}

	s.setCachedSchedule(guildID, def)
	return def
}

// SetGuildSchedule persists custom reset settings for a guild while preserving channel configurations.
func (s *Store) SetGuildSchedule(ctx context.Context, guildID string, resetMinute, rollsPerHour, claimHours int) error {
	if guildID == "" {
		return fmt.Errorf("guild ID cannot be empty")
	}
	if resetMinute < 0 || resetMinute > 59 {
		return fmt.Errorf("reset minute must be between 0 and 59")
	}
	if rollsPerHour < 1 || rollsPerHour > 100 {
		return fmt.Errorf("rolls per hour must be between 1 and 100")
	}
	if claimHours < 1 || claimHours > 24 {
		return fmt.Errorf("claim interval must be between 1 and 24 hours")
	}

	current := s.GuildSchedule(ctx, guildID)
	_, err := s.DB.ExecContext(ctx, `
INSERT INTO gacha_guild_settings(guild_id, reset_minute, rolls_per_hour, claim_hours, roll_channel_id, cmd_channel_id, updated_at)
VALUES($1, $2, $3, $4, $5, $6, now())
ON CONFLICT(guild_id) DO UPDATE SET
    reset_minute = EXCLUDED.reset_minute,
    rolls_per_hour = EXCLUDED.rolls_per_hour,
    claim_hours = EXCLUDED.claim_hours,
    updated_at = now()
`, guildID, resetMinute, rollsPerHour, claimHours, current.RollChannelID, current.CmdChannelID)
	if err != nil {
		return err
	}

	s.setCachedSchedule(guildID, ResetSchedule{
		ResetMinute:   resetMinute,
		RollsPerHour:  rollsPerHour,
		ClaimHours:    claimHours,
		RollChannelID: current.RollChannelID,
		CmdChannelID:  current.CmdChannelID,
	})
	return nil
}

// SetGuildChannels persists custom dedicated channels for rolls and gacha commands.
func (s *Store) SetGuildChannels(ctx context.Context, guildID, rollChannelID, cmdChannelID string) error {
	if guildID == "" {
		return fmt.Errorf("guild ID cannot be empty")
	}

	current := s.GuildSchedule(ctx, guildID)
	newRoll := current.RollChannelID
	if rollChannelID != "" {
		newRoll = rollChannelID
	}
	newCmd := current.CmdChannelID
	if cmdChannelID != "" {
		newCmd = cmdChannelID
	}

	_, err := s.DB.ExecContext(ctx, `
INSERT INTO gacha_guild_settings(guild_id, reset_minute, rolls_per_hour, claim_hours, roll_channel_id, cmd_channel_id, updated_at)
VALUES($1, $2, $3, $4, $5, $6, now())
ON CONFLICT(guild_id) DO UPDATE SET
    roll_channel_id = EXCLUDED.roll_channel_id,
    cmd_channel_id = EXCLUDED.cmd_channel_id,
    updated_at = now()
`, guildID, current.ResetMinute, current.RollsPerHour, current.ClaimHours, newRoll, newCmd)
	if err != nil {
		return err
	}

	s.setCachedSchedule(guildID, ResetSchedule{
		ResetMinute:   current.ResetMinute,
		RollsPerHour:  current.RollsPerHour,
		ClaimHours:    current.ClaimHours,
		RollChannelID: newRoll,
		CmdChannelID:  newCmd,
	})
	return nil
}

// ResetGuildSchedule deletes custom settings for a guild, restoring server defaults.
func (s *Store) ResetGuildSchedule(ctx context.Context, guildID string) error {
	if guildID == "" {
		return fmt.Errorf("guild ID cannot be empty")
	}

	_, err := s.DB.ExecContext(ctx, `DELETE FROM gacha_guild_settings WHERE guild_id=$1`, guildID)
	if err != nil && err != sql.ErrNoRows {
		return err
	}

	def := s.DefaultSchedule()
	s.setCachedSchedule(guildID, def)
	return nil
}

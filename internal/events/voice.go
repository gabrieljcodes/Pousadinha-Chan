package events

import (
	"bot/internal/database"
	"bot/pkg/config"
	"log"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// ActiveUserState tracks when a user became active and when they were last rewarded
type ActiveUserState struct {
	GuildID       string
	ChannelID     string
	EligibleSince time.Time
	LastRewarded  time.Time
}

var (
	activeVoiceStates = make(map[string]*ActiveUserState)
	voiceMu           sync.Mutex

	botCache   = make(map[string]bool)
	botCacheMu sync.RWMutex

	workerStopChan chan struct{}
	workerRunning  bool
)

// IsBot checks whether a user is a Discord bot, with in-memory caching
func IsBot(s *discordgo.Session, guildID, userID string) bool {
	botCacheMu.RLock()
	isBot, found := botCache[userID]
	botCacheMu.RUnlock()
	if found {
		return isBot
	}

	isBot = false
	if s != nil && s.State != nil {
		if member, err := s.State.Member(guildID, userID); err == nil && member != nil && member.User != nil {
			isBot = member.User.Bot
		} else if member, err := s.GuildMember(guildID, userID); err == nil && member != nil && member.User != nil {
			isBot = member.User.Bot
		}
	}

	botCacheMu.Lock()
	botCache[userID] = isBot
	botCacheMu.Unlock()
	return isBot
}

// IsVoiceStateEligible checks if a voice state is valid for earning voice rewards:
// Must be unmuted, undeafened, not in the guild's AFK channel, and not a bot.
func IsVoiceStateEligible(s *discordgo.Session, guild *discordgo.Guild, vs *discordgo.VoiceState) bool {
	if vs == nil || vs.ChannelID == "" {
		return false
	}

	// Exclude server AFK channel
	if guild != nil && guild.AfkChannelID != "" && vs.ChannelID == guild.AfkChannelID {
		return false
	}

	// Exclude self-mute, server-mute, self-deaf, server-deaf
	if vs.SelfMute || vs.Mute || vs.SelfDeaf || vs.Deaf {
		return false
	}

	// Exclude bot accounts (e.g. music bots)
	guildID := ""
	if guild != nil {
		guildID = guild.ID
	}
	if IsBot(s, guildID, vs.UserID) {
		return false
	}

	return true
}

// ProcessVoiceHeartbeat performs an evaluation pass across all voice channels in all guilds:
// Rewarding channels that contain at least 2 eligible, active human participants.
func ProcessVoiceHeartbeat(s *discordgo.Session) {
	if s == nil || s.State == nil {
		return
	}

	voiceMu.Lock()
	defer voiceMu.Unlock()

	coinsPerMinute := config.Economy.VoiceCoinsPerMinute
	if coinsPerMinute <= 0 {
		coinsPerMinute = 10
	}

	now := time.Now()
	currentlyEligibleUsers := make(map[string]struct{})

	for _, guild := range s.State.Guilds {
		if guild == nil {
			continue
		}

		// Group eligible users by voice channel
		channelEligibleMap := make(map[string][]string)

		for _, vs := range guild.VoiceStates {
			if IsVoiceStateEligible(s, guild, vs) {
				channelEligibleMap[vs.ChannelID] = append(channelEligibleMap[vs.ChannelID], vs.UserID)
			}
		}

		// Reward channels with 2 or more eligible human participants
		for channelID, usersInChannel := range channelEligibleMap {
			if len(usersInChannel) < 2 {
				continue
			}

			for _, userID := range usersInChannel {
				currentlyEligibleUsers[userID] = struct{}{}

				state, exists := activeVoiceStates[userID]
				if !exists || state.ChannelID != channelID {
					// User newly eligible in this channel
					activeVoiceStates[userID] = &ActiveUserState{
						GuildID:       guild.ID,
						ChannelID:     channelID,
						EligibleSince: now,
						LastRewarded:  now,
					}
					log.Printf("[VOICE] User %s became eligible in channel %s (group size: %d)",
						userID, channelID, len(usersInChannel))
					continue
				}

				// Check elapsed time since last reward
				elapsed := now.Sub(state.LastRewarded)
				if elapsed >= 60*time.Second {
					minutes := int(elapsed.Minutes())
					if minutes > 0 {
						reward := minutes * coinsPerMinute
						err := database.AddCoins(userID, reward)
						if err != nil {
							log.Printf("[VOICE ERROR] Failed to add %d coins to %s: %v", reward, userID, err)
						} else {
							log.Printf("[VOICE REWARD] User %s earned %d coins for %d minute(s) in channel %s",
								userID, reward, minutes, channelID)
						}
						// Advance last rewarded by exact credited minutes
						state.LastRewarded = state.LastRewarded.Add(time.Duration(minutes) * time.Minute)
					}
				}
			}
		}
	}

	// Clean up any users who are no longer eligible (disconnected, left channel, alone, or muted)
	for userID, state := range activeVoiceStates {
		if _, eligible := currentlyEligibleUsers[userID]; !eligible {
			log.Printf("[VOICE] User %s is no longer eligible in channel %s (alone, muted, or disconnected)",
				userID, state.ChannelID)
			delete(activeVoiceStates, userID)
		}
	}
}

// VoiceStateUpdate handles immediate voice state events to keep cache fresh
func VoiceStateUpdate(s *discordgo.Session, v *discordgo.VoiceStateUpdate) {
	if v == nil {
		return
	}

	userID := v.UserID

	voiceMu.Lock()
	state, exists := activeVoiceStates[userID]
	if exists {
		// If user disconnected, muted, or changed channel, clean up active state immediately
		if v.ChannelID == "" || v.ChannelID != state.ChannelID || v.SelfMute || v.Mute || v.SelfDeaf || v.Deaf {
			delete(activeVoiceStates, userID)
			log.Printf("[VOICE] State updated for user %s: session cleared (channel: %s)", userID, v.ChannelID)
		}
	}
	voiceMu.Unlock()
}

// StartVoiceWorker starts the background 30-second heartbeat ticker
func StartVoiceWorker(s *discordgo.Session) {
	voiceMu.Lock()
	if workerRunning {
		voiceMu.Unlock()
		return
	}
	workerRunning = true
	workerStopChan = make(chan struct{})
	voiceMu.Unlock()

	go func() {
		log.Println("[VOICE] Voice heartbeat worker started (30s interval)")
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				ProcessVoiceHeartbeat(s)
			case <-workerStopChan:
				log.Println("[VOICE] Voice heartbeat worker stopped")
				return
			}
		}
	}()
}

// InitializeVoiceSessions starts the background voice worker and performs initial scan
func InitializeVoiceSessions(s *discordgo.Session) {
	StartVoiceWorker(s)
	// Run initial evaluation asynchronously after gateway is ready
	go func() {
		time.Sleep(3 * time.Second)
		ProcessVoiceHeartbeat(s)
	}()
}

// CloseAllVoiceSessions cleanly terminates the worker and flushes sessions on shutdown
func CloseAllVoiceSessions() {
	voiceMu.Lock()
	defer voiceMu.Unlock()

	if workerRunning && workerStopChan != nil {
		close(workerStopChan)
		workerRunning = false
	}

	coinsPerMinute := config.Economy.VoiceCoinsPerMinute
	if coinsPerMinute <= 0 {
		coinsPerMinute = 10
	}

	now := time.Now()
	for userID, state := range activeVoiceStates {
		elapsed := now.Sub(state.LastRewarded)
		minutes := int(elapsed.Minutes())
		if minutes > 0 {
			reward := minutes * coinsPerMinute
			_ = database.AddCoins(userID, reward)
			log.Printf("[VOICE SHUTDOWN] Paid user %s %d coins for %d min", userID, reward, minutes)
		}
		delete(activeVoiceStates, userID)
	}
	log.Println("[VOICE] All voice sessions closed")
}

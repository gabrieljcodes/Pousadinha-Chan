package events

import (
	"estudocoin/internal/database"
	"estudocoin/pkg/config"
	"log"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

type VoiceSession struct {
	StartTime time.Time
	ChannelID string
}

var (
	// Map UserID -> Session
	sessions = make(map[string]VoiceSession)
	mu       sync.Mutex
)

// VoiceStateUpdate handles voice state changes
func VoiceStateUpdate(s *discordgo.Session, v *discordgo.VoiceStateUpdate) {
	// We need to handle state changes for the user who triggered the event,
	// AND potentially for others in the old/new channels (because user counts change).

	userID := v.UserID
	guildID := v.GuildID

	// 1. Identify impacted channels
	// We need the OLD channel ID to update neighbors there.
	// We can look it up in our map before processing the user.
	mu.Lock()
	var oldChannelID string
	if sess, ok := sessions[userID]; ok {
		oldChannelID = sess.ChannelID
	}
	mu.Unlock()

	// 2. Process the main user
	processUser(s, guildID, userID)

	// 3. Process neighbors in Old Channel (if any)
	if oldChannelID != "" && oldChannelID != v.ChannelID {
		updateChannelNeighbors(s, guildID, oldChannelID)
	}

	// 4. Process neighbors in New Channel (if any)
	if v.ChannelID != "" {
		updateChannelNeighbors(s, guildID, v.ChannelID)
	}
}

func updateChannelNeighbors(s *discordgo.Session, guildID, channelID string) {
	// Find all users in this channel
	guild, err := s.State.Guild(guildID)
	if err != nil {
		return
	}

	for _, vs := range guild.VoiceStates {
		if vs.ChannelID == channelID {
			processUser(s, guildID, vs.UserID)
		}
	}
}

func processUser(s *discordgo.Session, guildID, userID string) {
	mu.Lock()
	defer mu.Unlock()

	// 1. Payout and Close Existing Session (Always safe to do)
	if sess, ok := sessions[userID]; ok {
		duration := time.Since(sess.StartTime)
		minutes := int(duration.Minutes())

		if minutes > 0 {
			reward := minutes * config.Economy.VoiceCoinsPerMinute
			// Run DB op in goroutine to avoid blocking the lock? 
			// No, it's fast enough or we accept small block. 
			// Better: Release lock before DB? 
			// Let's keep it simple for now, but be aware.
			// Ideally, we shouldn't hold lock during DB.
			go func(uid string, rew int, mins int) {
				database.AddCoins(uid, rew)
				log.Printf("User %s earned %d coins for %d minutes.", uid, rew, mins)
			}(userID, reward, minutes)
		}
		delete(sessions, userID)
	}

	// 2. Check Eligibility for New Session
	// Get Current State from Cache
	vs, err := s.State.VoiceState(guildID, userID)
	if err != nil || vs.ChannelID == "" {
		// User not in voice or error
		return
	}

	// Check Mute/Deaf
	if vs.SelfMute || vs.Mute || vs.SelfDeaf || vs.Deaf {
		return // Ineligible
	}

	// Check User Count in Channel
	count := 0
	guild, err := s.State.Guild(guildID)
	if err == nil {
		for _, state := range guild.VoiceStates {
			if state.ChannelID == vs.ChannelID {
				count++
			}
		}
	}

	if count < 2 {
		return // Alone
	}

	// 3. Start New Session
	sessions[userID] = VoiceSession{
		StartTime: time.Now(),
		ChannelID: vs.ChannelID,
	}
}

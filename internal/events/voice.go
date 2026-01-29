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
	StartTime          time.Time
	ChannelID          string
	AccumulatedSeconds int
}

var (
	sessions = make(map[string]VoiceSession)
	mu       sync.Mutex
)

// VoiceStateUpdate handles voice state changes
func VoiceStateUpdate(s *discordgo.Session, v *discordgo.VoiceStateUpdate) {
	userID := v.UserID
	guildID := v.GuildID

	beforeChannel := ""
	if v.BeforeUpdate != nil {
		beforeChannel = v.BeforeUpdate.ChannelID
	}

	mu.Lock()
	defer mu.Unlock()

	sess, hasSession := sessions[userID]

	log.Printf("[VOICE] Event: user=%s, before=%s, after=%s, mute=%v, hasSession=%v",
		userID, beforeChannel, v.ChannelID, v.SelfMute, hasSession)

	// CASO 1: Usuário saiu completamente do Discord (after vazio)
	if v.ChannelID == "" {
		if hasSession {
			payAndDelete(userID, sess)
		}
		return
	}

	// CASO 2: Usuário mudou de canal (before diferente de after)
	if hasSession && beforeChannel != "" && beforeChannel != v.ChannelID {
		payAndDelete(userID, sess)
		// Continua para verificar se pode iniciar nova sessão no novo canal
	}

	// CASO 3: Usuário mutou/desmutou no mesmo canal
	if hasSession && sess.ChannelID == v.ChannelID {
		if v.SelfMute || v.Mute || v.SelfDeaf || v.Deaf {
			// Mutou - fecha sessão mas guarda segundos acumulados
			_, remaining := payAndDelete(userID, sess)
			// Guarda segundos restantes em memória temporária
			if remaining > 0 {
				sessions[userID] = VoiceSession{
					StartTime:          time.Now(), // placeholder
					ChannelID:          "",         // marcador de "mutado"
					AccumulatedSeconds: remaining,
				}
			}
			return
		}
		// Desmutou - verificar se é elegível
		// (sessão continua aberta)
	}

	// Verificar elegibilidade para nova sessão
	if v.SelfMute || v.Mute || v.SelfDeaf || v.Deaf {
		return
	}

	count := countUsersInChannel(s, guildID, v.ChannelID)
	if count < 2 {
		return
	}

	// Iniciar nova sessão
	accumulated := 0
	if hasSession && sess.ChannelID == "" {
		// Estava mutado com segundos acumulados
		accumulated = sess.AccumulatedSeconds
		delete(sessions, userID)
	}

	sessions[userID] = VoiceSession{
		StartTime:          time.Now(),
		ChannelID:          v.ChannelID,
		AccumulatedSeconds: accumulated,
	}
	log.Printf("[VOICE] Started session for user %s in channel %s (acc=%d, count=%d)",
		userID, v.ChannelID, accumulated, count)
}

// payAndDelete paga o tempo acumulado e remove a sessão
// Retorna minutos pagos e segundos restantes
func payAndDelete(userID string, sess VoiceSession) (minutes int, remaining int) {
	duration := time.Since(sess.StartTime)
	totalSecs := int(duration.Seconds()) + sess.AccumulatedSeconds
	minutes = totalSecs / 60
	remaining = totalSecs % 60

	if minutes > 0 {
		reward := minutes * config.Economy.VoiceCoinsPerMinute
		go func(uid string, rew int, mins int) {
			database.AddCoins(uid, rew)
			log.Printf("[VOICE REWARD] User %s earned %d coins for %d minutes", uid, rew, mins)
		}(userID, reward, minutes)
	}

	delete(sessions, userID)
	log.Printf("[VOICE] Paid and closed session for user %s (%d min, %d sec remaining)",
		userID, minutes, remaining)
	return minutes, remaining
}

func countUsersInChannel(s *discordgo.Session, guildID, channelID string) int {
	guild, err := s.State.Guild(guildID)
	if err != nil {
		guild, err = s.Guild(guildID)
		if err != nil {
			return 0
		}
	}

	count := 0
	for _, vs := range guild.VoiceStates {
		if vs.ChannelID == channelID && !vs.SelfMute && !vs.Mute && !vs.SelfDeaf && !vs.Deaf {
			count++
		}
	}
	return count
}

func InitializeVoiceSessions(s *discordgo.Session) {
	time.Sleep(2 * time.Second)
	log.Println("[VOICE] Initializing voice sessions...")

	for _, guild := range s.State.Guilds {
		channelCounts := make(map[string]int)
		channelUsers := make(map[string][]*discordgo.VoiceState)

		for _, vs := range guild.VoiceStates {
			if vs.SelfMute || vs.Mute || vs.SelfDeaf || vs.Deaf {
				continue
			}
			channelCounts[vs.ChannelID]++
			channelUsers[vs.ChannelID] = append(channelUsers[vs.ChannelID], vs)
		}

		for channelID, count := range channelCounts {
			if count < 2 {
				continue
			}
			for _, vs := range channelUsers[channelID] {
				mu.Lock()
				sessions[vs.UserID] = VoiceSession{
					StartTime: time.Now(),
					ChannelID: channelID,
				}
				mu.Unlock()
				log.Printf("[VOICE] Init session for %s in %s", vs.UserID, channelID)
			}
		}
	}
	log.Println("[VOICE] Initialization complete")
}

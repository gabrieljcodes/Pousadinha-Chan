package games

import (
	"estudocoin/internal/database"
	"estudocoin/pkg/config"
	"estudocoin/pkg/utils"
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Active games map: UserID -> Control Channel
var (
	activeGames = make(map[string]chan bool)
	mutex       sync.Mutex
)

// Constants
const (
	MinBet          = 100
	MultiplierSpeed = 1000 * time.Millisecond
	Increment       = 0.1
)

type MessageUpdater func(embed *discordgo.MessageEmbed, finished bool)

// --- INTERACTION (SLASH) START ---

func StartAviatorInteraction(s *discordgo.Session, i *discordgo.InteractionCreate, bet int) {
	userID := i.Member.User.ID

	if !validatePreQueue(userID, bet) {
		respondPrivate(s, i, utils.ErrorEmbed(fmt.Sprintf("Cannot queue game (Min bet: %d, Check balance/active games).", MinBet)))
		return
	}

	// Define the Job
	job := GameJob{
		UserID: userID,
		OnQueue: func(pos int) {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Embeds: []*discordgo.MessageEmbed{utils.InfoEmbed("⏳ Queued", fmt.Sprintf("You are position **#%d** in the queue.", pos))},
					Flags:  discordgo.MessageFlagsEphemeral,
				},
			})
		},
		Run: func(finishChan chan struct{}) {
			// This runs when it's the user's turn
			defer close(finishChan) // Signal manager when done

			// Re-validate balance (user might have spent coins while waiting)
			if database.GetBalance(userID) < bet {
				s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseChannelMessageWithSource,
					Data: &discordgo.InteractionResponseData{
						Content: "You ran out of money while waiting in queue!",
						Flags:   discordgo.MessageFlagsEphemeral,
					},
				})
				return
			}

			// Setup Game State
			controlChan := setupGame(userID, bet)
			embed, btn := getInitialState(bet, userID)

			// Try to Edit original response (if token valid) or Send New
			// Interaction tokens last 15 mins. Queue might take longer? Unlikely for small bots.
			// Ideally we send a NEW message for the game to be safe and visible.
			
			// Let's try sending a NEW message to the channel
			msg, err := s.ChannelMessageSendComplex(i.ChannelID, &discordgo.MessageSend{
				Content: fmt.Sprintf("<@%s> Your Aviator game is starting!", userID),
				Embeds: []*discordgo.MessageEmbed{embed},
				Components: []discordgo.MessageComponent{
					discordgo.ActionsRow{Components: []discordgo.MessageComponent{btn}},
				},
			})

			if err != nil {
				cleanup(userID)
				return
			}

			// Updater for Slash Flow (updates the new message)
			updater := func(embed *discordgo.MessageEmbed, finished bool) {
				comps := []discordgo.MessageComponent{}
				if !finished {
					comps = []discordgo.MessageComponent{
						discordgo.ActionsRow{Components: []discordgo.MessageComponent{btn}},
					}
				} else {
					btn.Label = "GAME OVER"
					btn.Style = discordgo.SecondaryButton
					btn.Disabled = true
					comps = []discordgo.MessageComponent{
						discordgo.ActionsRow{Components: []discordgo.MessageComponent{btn}},
					}
				}

				embeds := []*discordgo.MessageEmbed{embed}
				s.ChannelMessageEditComplex(&discordgo.MessageEdit{
					ID: msg.ID,
					Channel: i.ChannelID,
					Embeds: &embeds,
					Components: &comps,
				})
			}

			runGameLoop(userID, bet, controlChan, updater)
		},
	}

	Enqueue(job)
}

// --- TEXT COMMAND START ---

func StartAviatorText(s *discordgo.Session, m *discordgo.MessageCreate, bet int) {
	userID := m.Author.ID

	if !validatePreQueue(userID, bet) {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed(fmt.Sprintf("Cannot queue game (Min bet: %d, Check balance/active games).", MinBet)))
		return
	}

	job := GameJob{
		UserID: userID,
		OnQueue: func(pos int) {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.InfoEmbed("⏳ Queued", fmt.Sprintf("You are position **#%d** in the queue.", pos)))
		},
		Run: func(finishChan chan struct{}) {
			defer close(finishChan)

			if database.GetBalance(userID) < bet {
				s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed(fmt.Sprintf("<@%s> You ran out of money while waiting.", userID)))
				return
			}

			controlChan := setupGame(userID, bet)
			embed, btn := getInitialState(bet, userID)

			msg, err := s.ChannelMessageSendComplex(m.ChannelID, &discordgo.MessageSend{
				Content: fmt.Sprintf("<@%s> Your Aviator game is starting!", userID),
				Embeds: []*discordgo.MessageEmbed{embed},
				Components: []discordgo.MessageComponent{
					discordgo.ActionsRow{Components: []discordgo.MessageComponent{btn}},
				},
			})

			if err != nil {
				cleanup(userID)
				return
			}

			updater := func(embed *discordgo.MessageEmbed, finished bool) {
				comps := []discordgo.MessageComponent{}
				if !finished {
					comps = []discordgo.MessageComponent{
						discordgo.ActionsRow{Components: []discordgo.MessageComponent{btn}},
					}
				} else {
					btn.Label = "GAME OVER"
					btn.Style = discordgo.SecondaryButton
					btn.Disabled = true
					comps = []discordgo.MessageComponent{
						discordgo.ActionsRow{Components: []discordgo.MessageComponent{btn}},
					}
				}
				
				embeds := []*discordgo.MessageEmbed{embed}
				s.ChannelMessageEditComplex(&discordgo.MessageEdit{
					ID: msg.ID,
					Channel: m.ChannelID,
					Embeds: &embeds,
					Components: &comps,
				})
			}

			runGameLoop(userID, bet, controlChan, updater)
		},
	}

	Enqueue(job)
}

// --- HELPERS ---

func validatePreQueue(userID string, bet int) bool {
	// Simple checks before queuing
	if bet < MinBet { return false }
	if database.GetBalance(userID) < bet { return false }
	return true
}

func setupGame(userID string, bet int) chan bool {
	mutex.Lock()
	controlChan := make(chan bool, 1)
	activeGames[userID] = controlChan
	mutex.Unlock()
	database.CollectLostBet(userID, bet)
	return controlChan
}

func getInitialState(bet int, userID string) (*discordgo.MessageEmbed, discordgo.Button) {
	embed := utils.NewEmbed()
	embed.Title = "✈️ Aviator Starting..."
	embed.Description = fmt.Sprintf("Bet: **%d**\nPreparing for takeoff...", bet)
	embed.Color = utils.ColorBlue

	btn := discordgo.Button{
		Label:    "🛑 CASH OUT",
		Style:    discordgo.SuccessButton,
		CustomID: "aviator_stop_" + userID,
	}
	return embed, btn
}

func runGameLoop(userID string, bet int, controlChan chan bool, update MessageUpdater) {
	defer cleanup(userID)

	var crashPoint float64
	if rand.Float64() < 0.40 {
		crashPoint = 1.0 + (rand.Float64() * 0.5) 
	} else {
		r := rand.Float64()
		crashPoint = 0.96 / (1.0 - r)
	}
	if crashPoint < 1.0 { crashPoint = 1.0 }
	if crashPoint > 100.0 { crashPoint = 100.0 }

	startTime := time.Now()
	ticker := time.NewTicker(1000 * time.Millisecond)
	defer ticker.Stop()

	time.Sleep(1 * time.Second)
	startTime = time.Now()

	for {
		select {
		case <-controlChan:
			elapsed := time.Since(startTime).Seconds()
			multiplier := 1.0 + (elapsed * 0.1)
			
			if multiplier >= crashPoint {
				update(utils.ErrorEmbed(fmt.Sprintf("💥 CRASHED at x%.2f", crashPoint)), true)
				return
			}

			winAmount := int(float64(bet) * multiplier)
			err := database.AddCoins(userID, winAmount)
			if err != nil {
				log.Printf("[AVIATOR ERROR] Failed to add coins for user %s: %v", userID, err)
			}
			log.Printf("[AVIATOR WIN] User %s won %d %s (bet: %d, multiplier: %.2f)", userID, winAmount, config.Bot.CurrencySymbol, bet, multiplier)
			update(utils.SuccessEmbed("✅ CASHED OUT!", fmt.Sprintf("You jumped at **x%.2f**\nProfit: **+%d %s**", multiplier, winAmount, config.Bot.CurrencySymbol)), true)
			return

		case <-ticker.C:
			elapsed := time.Since(startTime).Seconds()
			multiplier := 1.0 + (elapsed * 0.1)

			if multiplier >= crashPoint {
				update(utils.ErrorEmbed(fmt.Sprintf("💥 CRASHED at x%.2f", crashPoint)), true)
				return
			}
			
			embed := utils.NewEmbed()
			embed.Title = "✈️ Aviator Flying..."
			embed.Description = fmt.Sprintf("Multiplier: **x%.2f**\nPotential Win: **%d**", multiplier, int(float64(bet)*multiplier))
			embed.Color = utils.ColorBlue
			
			dots := int(elapsed)
			if dots > 15 { dots = 15 }
			graph := "🛫" + string(repeatRune('.', dots)) + "✈️"
			embed.Fields = []*discordgo.MessageEmbedField{{Name: "Altitude", Value: graph}}
			
			update(embed, false)
		}
	}
}

func HandleButton(s *discordgo.Session, i *discordgo.InteractionCreate) {
	userID := i.Member.User.ID
	mutex.Lock()
	ch, exists := activeGames[userID]
	mutex.Unlock()

	if !exists {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{Content: "⚠️ Inactive game.", Flags: discordgo.MessageFlagsEphemeral},
		})
		return
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{Type: discordgo.InteractionResponseDeferredMessageUpdate})

	select {
	case ch <- true:
	default:
	}
}

func cleanup(userID string) {
	mutex.Lock()
	delete(activeGames, userID)
	mutex.Unlock()
}

func respondPrivate(s *discordgo.Session, i *discordgo.InteractionCreate, embed *discordgo.MessageEmbed) {
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{embed}, Flags: discordgo.MessageFlagsEphemeral},
	})
}

func repeatRune(r rune, n int) []rune {
	if n > 15 { n = 15 }
	b := make([]rune, n)
	for i := range b { b[i] = r }
	return b
}
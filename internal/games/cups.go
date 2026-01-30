package games

import (
	"estudocoin/internal/database"
	"estudocoin/pkg/config"
	"estudocoin/pkg/utils"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

const MinCupBet = 50

// Map to send interactions (button clicks) to the running game loop
var (
	activeCupGames = make(map[string]chan *discordgo.InteractionCreate)
	cupMutex       sync.Mutex
)

// --- ENTRY POINTS ---

func StartCupGameInteraction(s *discordgo.Session, i *discordgo.InteractionCreate, bet int) {
	startCupGame(s, i.Member.User.ID, bet, i.ChannelID, func(msg *discordgo.MessageSend) {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content:    msg.Content,
				Embeds:     msg.Embeds,
				Components: msg.Components,
			},
		})
	})
}

func StartCupGameText(s *discordgo.Session, m *discordgo.MessageCreate, bet int) {
	startCupGame(s, m.Author.ID, bet, m.ChannelID, func(msg *discordgo.MessageSend) {
		s.ChannelMessageSendComplex(m.ChannelID, msg)
	})
}

// --- CORE LOGIC ---

func startCupGame(s *discordgo.Session, userID string, bet int, channelID string, initialResponder func(*discordgo.MessageSend)) {
	// Validation
	if bet < MinCupBet {
		// Use a simpler direct response for errors pre-queue
		s.ChannelMessageSend(channelID, fmt.Sprintf("❌ Minimum bet is %d %s", MinCupBet, config.Bot.CurrencySymbol))
		return
	}
	if database.GetBalance(userID) < bet {
		s.ChannelMessageSend(channelID, "❌ Insufficient funds.")
		return
	}

	// Queue Job
	job := GameJob{
		UserID: userID,
		OnQueue: func(pos int) {
			s.ChannelMessageSend(channelID, fmt.Sprintf("⏳ <@%s> Queued for Cup Game (Pos: #%d)", userID, pos))
		},
		Run: func(finishChan chan struct{}) {
			defer close(finishChan)
			defer cleanupCup(userID)

			// Re-check funds
			if database.GetBalance(userID) < bet {
				s.ChannelMessageSend(channelID, fmt.Sprintf("❌ <@%s> You ran out of funds while waiting.", userID))
				return
			}

			// Deduct initial bet (goes to bot)
			database.CollectLostBet(userID, bet)

			// Setup Input Channel
			gameChan := make(chan *discordgo.InteractionCreate) // Unbuffered block
			cupMutex.Lock()
			activeCupGames[userID] = gameChan
			cupMutex.Unlock()

			// Game State
			currentPot := bet
			round := 1
			gameMsgID := ""

			// --- ROUND LOOP ---
			for {
				winningCup := rand.Intn(6) + 1 // 1 to 6

				// Prepare UI
				embed := utils.NewEmbed()
				embed.Title = fmt.Sprintf("🥤 Cup Game - Round %d", round)
				embed.Description = fmt.Sprintf("Current Pot: **%d %s**\n\n**Guess where the coin is!**", currentPot, config.Bot.CurrencySymbol)
			embed.Color = utils.ColorGold
				
				// Buttons 1-6
			
rows := []discordgo.MessageComponent{
					discordgo.ActionsRow{Components: makeCupButtons(userID, 1, 3)},
					discordgo.ActionsRow{Components: makeCupButtons(userID, 4, 6)},
				}

				// Send or Edit
				msgSend := &discordgo.MessageSend{
					Content:    fmt.Sprintf("<@%s> It's your turn!", userID),
					Embeds:     []*discordgo.MessageEmbed{embed},
					Components: rows,
				}

				if round == 1 {
					// Using the callback for the very first message might act differently depending on slash/text
					// but for simplicity in the loop, we might want to just store the ID after the first send.
					// However, the 'initialResponder' is abstract. 
					// Let's just use ChannelMessageSendComplex for the loop updates, 
					// and use initialResponder ONLY if we haven't sent a message yet?
					// Actually, for Slash commands, we MUST use InteractionEdit after the first response.
					// To simplify: The queue system already sends a "Starting" message. 
					// Let's just send a NEW message for the game board to avoid complexity with ephemeral/slash tokens expiring.
					
					m, err := s.ChannelMessageSendComplex(channelID, msgSend)
					if err != nil { return }
					gameMsgID = m.ID
				} else {
					// Edit existing
					embeds := msgSend.Embeds
					s.ChannelMessageEditComplex(&discordgo.MessageEdit{
						ID: gameMsgID,
						Channel: channelID,
						Embeds: &embeds,
						Components: &msgSend.Components,
					})
				}

				// Wait for Input
				var choice int
				select {
				case interaction := <-gameChan:
					// Parse choice from CustomID: cup_pick_X_USERID
					parts := strings.Split(interaction.MessageComponentData().CustomID, "_")
					if len(parts) >= 3 {
						choice, _ = strconv.Atoi(parts[2])
					}
					// Acknowledge click
					s.InteractionRespond(interaction.Interaction, &discordgo.InteractionResponse{
						Type: discordgo.InteractionResponseDeferredMessageUpdate,
					})
				case <-time.After(2 * time.Minute):
					// Timeout
					s.ChannelMessageEdit(channelID, gameMsgID, "⏰ Game timed out. You lost your bet.")
					return
				}

				// Check Result
				if choice == winningCup {
					// WIN - First round 5x, subsequent rounds 2x (5x, 10x, 20x, 40x...)
					if round == 1 {
						currentPot *= 5
					} else {
						currentPot *= 2
					}
					
					// Ask to Continue
					embed.Title = "✅ CORRECT!"
					nextMultiplier := 2
					if round == 1 {
						nextMultiplier = 10
					}
					embed.Description = fmt.Sprintf("The coin was in **Cup %d**.\n\nYou have **%d %s**.\n\nDo you want to **Cash Out** or continue for **%dx**?", winningCup, currentPot, config.Bot.CurrencySymbol, nextMultiplier)
					embed.Color = utils.ColorGreen

					actionRow := discordgo.ActionsRow{
						Components: []discordgo.MessageComponent{
							discordgo.Button{
								Label: "💰 Cash Out",
								Style: discordgo.SuccessButton,
								CustomID: fmt.Sprintf("cup_cashout_%s", userID),
							},
							discordgo.Button{
								Label: func() string {
								if round == 1 {
									return "🎲 Continue (10x or Nothing)"
								}
								return "🎲 Continue (Double or Nothing)"
							}(),
								Style: discordgo.PrimaryButton,
								CustomID: fmt.Sprintf("cup_continue_%s", userID),
							},
						},
					}

					embeds := []*discordgo.MessageEmbed{embed}
					s.ChannelMessageEditComplex(&discordgo.MessageEdit{
						ID: gameMsgID,
						Channel: channelID,
						Embeds: &embeds,
						Components: &[]discordgo.MessageComponent{actionRow},
					})

					// Wait for Decision
					select {
					case interaction := <-gameChan:
						id := interaction.MessageComponentData().CustomID
						s.InteractionRespond(interaction.Interaction, &discordgo.InteractionResponse{
							Type: discordgo.InteractionResponseDeferredMessageUpdate,
						})

						if strings.Contains(id, "cashout") {
							// Cash Out
							database.AddCoins(userID, currentPot)
							s.ChannelMessageEdit(channelID, gameMsgID, fmt.Sprintf("🎉 **Congratulatios!**\n<@%s> walked away with **%d %s**!", userID, currentPot, config.Bot.CurrencySymbol))
							return
						}
						// Continue -> Loop repeats with new round
						round++

					case <-time.After(1 * time.Minute):
						// Auto Cashout on timeout
						database.AddCoins(userID, currentPot)
						s.ChannelMessageSend(channelID, fmt.Sprintf("⏰ Timeout. Auto-cashing out **%d %s**.", currentPot, config.Bot.CurrencySymbol))
						return
					}

				} else {
					// LOSE
					embed.Title = "❌ WRONG!"
					embed.Description = fmt.Sprintf("You picked Cup %d, but the coin was in **Cup %d**.\n\n📉 You lost **%d %s**.", choice, winningCup, bet, config.Bot.CurrencySymbol)
					embed.Color = utils.ColorRed
					
					// Disable everything
					embeds := []*discordgo.MessageEmbed{embed}
					s.ChannelMessageEditComplex(&discordgo.MessageEdit{
						ID: gameMsgID,
						Channel: channelID,
						Embeds: &embeds,
						Components: &[]discordgo.MessageComponent{}, // No buttons
					})
					return
				}
			}
		},
	}

	Enqueue(job)
}

// --- HELPERS ---

func HandleCupInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	userID := i.Member.User.ID
	cupMutex.Lock()
	ch, exists := activeCupGames[userID]
	cupMutex.Unlock()

	if !exists {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{Content: "⚠️ This is not your game.", Flags: discordgo.MessageFlagsEphemeral},
		})
		return
	}

	// Send interaction to game loop
	// Non-blocking try
	select {
	case ch <- i:
	default:
	}
}

func cleanupCup(userID string) {
	cupMutex.Lock()
	delete(activeCupGames, userID)
	cupMutex.Unlock()
}

func makeCupButtons(userID string, start, end int) []discordgo.MessageComponent {
	btns := []discordgo.MessageComponent{}
	for i := start; i <= end; i++ {
		btns = append(btns, discordgo.Button{
			Label:    fmt.Sprintf("%d", i),
			Style:    discordgo.SecondaryButton,
			Emoji:    &discordgo.ComponentEmoji{Name: "🥤"},
			CustomID: fmt.Sprintf("cup_pick_%d_%s", i, userID),
		})
	}
	return btns
}

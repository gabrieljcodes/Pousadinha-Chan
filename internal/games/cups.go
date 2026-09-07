package games

import (
	"crypto/rand"
	"encoding/binary"
	"estudocoin/internal/database"
	"estudocoin/pkg/config"
	"estudocoin/pkg/utils"
	"fmt"
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

// pickWinningCup picks a winning cup with uniform cryptographically secure randomness
func pickWinningCup(totalCups int) int {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return int(binary.LittleEndian.Uint32(b[:])%uint32(totalCups)) + 1
}

// --- ENTRY POINTS ---

func StartCupGameInteraction(s *discordgo.Session, i *discordgo.InteractionCreate, bet int) {
	userID := i.Member.User.ID

	if bet < MinCupBet {
		respondPrivate(s, i, utils.ErrorEmbed(fmt.Sprintf("Minimum bet is %d %s", MinCupBet, config.Bot.CurrencySymbol)))
		return
	}
	if database.GetBalance(userID) < bet {
		respondPrivate(s, i, utils.ErrorEmbed("Insufficient funds."))
		return
	}
	if IsUserInGame(userID) {
		respondPrivate(s, i, utils.ErrorEmbed("You already have an active game in progress! Finish it first."))
		return
	}

	job := GameJob{
		UserID: userID,
		OnQueue: func(pos int) {
			if pos == -1 {
				respondPrivate(s, i, utils.ErrorEmbed("You already have an active game in progress! Finish it first."))
			}
		},
		Run: func(finishChan chan struct{}) {
			defer close(finishChan)
			defer cleanupCup(userID)

			if database.GetBalance(userID) < bet {
				respondPrivate(s, i, utils.ErrorEmbed("You ran out of funds before starting."))
				return
			}

			// Deduct initial bet atomically
			if err := database.CollectLostBet(userID, bet); err != nil {
				respondPrivate(s, i, utils.ErrorEmbed("Error processing bet."))
				return
			}

			embed, rows := buildCupRoundUI(1, bet, 6, userID)
			err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content:    fmt.Sprintf("<@%s> It's your turn!", userID),
					Embeds:     []*discordgo.MessageEmbed{embed},
					Components: rows,
				},
			})

			if err != nil {
				// Refund immediately on failure
				_ = database.AddCoins(userID, bet)
				return
			}

			msg, err := s.InteractionResponse(i.Interaction)
			if err != nil || msg == nil {
				_ = database.AddCoins(userID, bet)
				return
			}

			runCupGameLoop(s, userID, bet, i.ChannelID, msg.ID)
		},
	}

	Enqueue(job)
}

func StartCupGameText(s *discordgo.Session, m *discordgo.MessageCreate, bet int) {
	userID := m.Author.ID

	if bet < MinCupBet {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed(fmt.Sprintf("Minimum bet is %d %s", MinCupBet, config.Bot.CurrencySymbol)))
		return
	}
	if database.GetBalance(userID) < bet {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Insufficient funds."))
		return
	}
	if IsUserInGame(userID) {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("You already have an active game in progress! Finish it first."))
		return
	}

	job := GameJob{
		UserID: userID,
		OnQueue: func(pos int) {
			if pos == -1 {
				s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("You already have an active game in progress! Finish it first."))
			}
		},
		Run: func(finishChan chan struct{}) {
			defer close(finishChan)
			defer cleanupCup(userID)

			if database.GetBalance(userID) < bet {
				s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed(fmt.Sprintf("<@%s> You ran out of funds.", userID)))
				return
			}

			// Deduct initial bet atomically
			if err := database.CollectLostBet(userID, bet); err != nil {
				s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Error processing bet."))
				return
			}

			embed, rows := buildCupRoundUI(1, bet, 6, userID)
			msg, err := s.ChannelMessageSendComplex(m.ChannelID, &discordgo.MessageSend{
				Content:    fmt.Sprintf("<@%s> It's your turn!", userID),
				Embeds:     []*discordgo.MessageEmbed{embed},
				Components: rows,
			})

			if err != nil {
				_ = database.AddCoins(userID, bet)
				return
			}

			runCupGameLoop(s, userID, bet, m.ChannelID, msg.ID)
		},
	}

	Enqueue(job)
}

// --- CORE GAME LOOP ---

func buildCupRoundUI(round int, currentPot int, numCups int, userID string) (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
	embed := utils.NewEmbed()
	embed.Title = fmt.Sprintf("🥤 Cup Game — Round %d", round)

	multiplierText := "5x"
	if round > 1 {
		multiplierText = "2x (Double or Nothing)"
	}

	embed.Description = fmt.Sprintf(
		"Current Pot: **%d %s**\nRound Multiplier: **%s**\n\n**Guess which cup contains the coin!** (1 to %d)",
		currentPot, config.Bot.CurrencySymbol, multiplierText, numCups,
	)
	embed.Color = utils.ColorGold

	var rows []discordgo.MessageComponent
	if numCups == 6 {
		rows = []discordgo.MessageComponent{
			discordgo.ActionsRow{Components: makeCupButtons(userID, 1, 3)},
			discordgo.ActionsRow{Components: makeCupButtons(userID, 4, 6)},
		}
	} else {
		// 2 cups for authentic Double or Nothing rounds
		rows = []discordgo.MessageComponent{
			discordgo.ActionsRow{Components: makeCupButtons(userID, 1, 2)},
		}
	}

	return embed, rows
}

func runCupGameLoop(s *discordgo.Session, userID string, bet int, channelID string, gameMsgID string) {
	// Buffered channel to prevent dropped button clicks
	gameChan := make(chan *discordgo.InteractionCreate, 2)
	cupMutex.Lock()
	activeCupGames[userID] = gameChan
	cupMutex.Unlock()

	currentPot := bet
	round := 1

	for {
		// Round 1 has 6 cups (pays 5x); Round 2+ has 2 cups (true 50/50 Double or Nothing)
		numCups := 6
		if round > 1 {
			numCups = 2
		}

		winningCup := pickWinningCup(numCups)

		// Wait for player to pick a cup
		var choice int
		select {
		case interaction := <-gameChan:
			parts := strings.Split(interaction.MessageComponentData().CustomID, "_")
			if len(parts) >= 3 {
				choice, _ = strconv.Atoi(parts[2])
			}
			_ = s.InteractionRespond(interaction.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseDeferredMessageUpdate,
			})

		case <-time.After(2 * time.Minute):
			// Timeout while guessing
			timeoutEmbed := utils.ErrorEmbed(fmt.Sprintf("⏰ **Game Timed Out!**\n<@%s> took too long to pick a cup. Bet lost.", userID))
			_, _ = s.ChannelMessageEditComplex(&discordgo.MessageEdit{
				ID:         gameMsgID,
				Channel:    channelID,
				Embeds:     &[]*discordgo.MessageEmbed{timeoutEmbed},
				Components: &[]discordgo.MessageComponent{},
			})
			return
		}

		// Check if player picked the correct cup
		if choice == winningCup {
			// WIN: Round 1 pays 5x; subsequent Double or Nothing rounds double the pot (2x)
			if round == 1 {
				currentPot *= 5
			} else {
				currentPot *= 2
			}

			netProfit := currentPot - bet

			// Ask to Cash Out or Continue
			embed := utils.NewEmbed()
			embed.Title = "✅ CORRECT!"
			embed.Color = utils.ColorGreen

			embed.Description = fmt.Sprintf(
				"The coin was in **Cup %d**!\n\n"+
					"💰 **Current Pot:** %d %s *(+%d profit)*\n\n"+
					"Do you want to **Cash Out** now or **Continue** for Double or Nothing (50%% chance on 2 cups)?",
				winningCup, currentPot, config.Bot.CurrencySymbol, netProfit,
			)

			actionRow := discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					discordgo.Button{
						Label:    "💰 Cash Out",
						Style:    discordgo.SuccessButton,
						CustomID: fmt.Sprintf("cup_cashout_%s", userID),
					},
					discordgo.Button{
						Label:    "🎲 Continue (Double or Nothing)",
						Style:    discordgo.PrimaryButton,
						CustomID: fmt.Sprintf("cup_continue_%s", userID),
					},
				},
			}

			_, _ = s.ChannelMessageEditComplex(&discordgo.MessageEdit{
				ID:         gameMsgID,
				Channel:    channelID,
				Embeds:     &[]*discordgo.MessageEmbed{embed},
				Components: &[]discordgo.MessageComponent{actionRow},
			})

			// Wait for Cash Out or Continue decision
			select {
			case interaction := <-gameChan:
				id := interaction.MessageComponentData().CustomID
				_ = s.InteractionRespond(interaction.Interaction, &discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseDeferredMessageUpdate,
				})

				if strings.Contains(id, "cashout") {
					// Cash Out
					_ = database.AddCoins(userID, currentPot)
					winEmbed := utils.SuccessEmbed("CASHED OUT!",
						fmt.Sprintf("🎉 **Congratulations!**\n<@%s> walked away with **%d %s**! *(Net Profit: +%d %s)*",
							userID, currentPot, config.Bot.CurrencySymbol, netProfit, config.Bot.CurrencySymbol))

					_, _ = s.ChannelMessageEditComplex(&discordgo.MessageEdit{
						ID:         gameMsgID,
						Channel:    channelID,
						Embeds:     &[]*discordgo.MessageEmbed{winEmbed},
						Components: &[]discordgo.MessageComponent{},
					})
					return
				}

				// Continue to next round
				round++
				nextRoundEmbed, nextRows := buildCupRoundUI(round, currentPot, 2, userID)
				_, _ = s.ChannelMessageEditComplex(&discordgo.MessageEdit{
					ID:         gameMsgID,
					Channel:    channelID,
					Embeds:     &[]*discordgo.MessageEmbed{nextRoundEmbed},
					Components: &nextRows,
				})

			case <-time.After(1 * time.Minute):
				// Auto Cashout on timeout
				_ = database.AddCoins(userID, currentPot)
				autoEmbed := utils.SuccessEmbed("AUTO CASH-OUT",
					fmt.Sprintf("⏰ Time expired! Automatically cashed out **%d %s** for <@%s>.",
						currentPot, config.Bot.CurrencySymbol, userID))

				_, _ = s.ChannelMessageEditComplex(&discordgo.MessageEdit{
					ID:         gameMsgID,
					Channel:    channelID,
					Embeds:     &[]*discordgo.MessageEmbed{autoEmbed},
					Components: &[]discordgo.MessageComponent{},
				})
				return
			}

		} else {
			// LOSE
			embed := utils.NewEmbed()
			embed.Title = "❌ WRONG CUP!"
			embed.Color = utils.ColorRed
			embed.Description = fmt.Sprintf(
				"You picked Cup %d, but the coin was hiding in **Cup %d**!\n\n📉 You lost your initial bet of **%d %s**.",
				choice, winningCup, bet, config.Bot.CurrencySymbol,
			)

			_, _ = s.ChannelMessageEditComplex(&discordgo.MessageEdit{
				ID:         gameMsgID,
				Channel:    channelID,
				Embeds:     &[]*discordgo.MessageEmbed{embed},
				Components: &[]discordgo.MessageComponent{},
			})
			return
		}
	}
}

// --- HELPERS ---

// HandleCupInteraction validates that the user clicking is the owner of this game
func HandleCupInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	customID := i.MessageComponentData().CustomID
	parts := strings.Split(customID, "_")
	if len(parts) < 3 {
		return
	}
	expectedUserID := parts[len(parts)-1]

	if i.Member.User.ID != expectedUserID {
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: fmt.Sprintf("❌ This is not your game! Only <@%s> can make choices here.", expectedUserID),
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	cupMutex.Lock()
	ch, exists := activeCupGames[expectedUserID]
	cupMutex.Unlock()

	if !exists {
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "⚠️ This game has already ended.",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

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
			Label:    fmt.Sprintf("Cup %d", i),
			Style:    discordgo.SecondaryButton,
			Emoji:    &discordgo.ComponentEmoji{Name: "🥤"},
			CustomID: fmt.Sprintf("cup_pick_%d_%s", i, userID),
		})
	}
	return btns
}

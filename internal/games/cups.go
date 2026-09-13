package games

import (
	"bot/internal/database"
	"bot/internal/locale"
	"bot/pkg/config"
	"bot/pkg/utils"
	"crypto/rand"
	"encoding/binary"
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

func StartCupGameInteraction(s *discordgo.Session, i *discordgo.InteractionCreate, bet int) {
	if i.GuildID == "" {
		respondPrivate(s, i, utils.ErrorEmbed(locale.Text("games.aviator.this_game_can_only_be_played_within")))
		return
	}
	guildID := i.GuildID
	userID := i.Member.User.ID

	if bet < MinCupBet {
		respondPrivate(s, i, utils.ErrorEmbed(locale.Text("games.cups.minimum_bet_is.formatted", locale.Data{"MinCupBet": MinCupBet, "CurrencySymbol": config.Bot.CurrencySymbol})))
		return
	}
	if database.GetBalance(guildID, userID) < bet {
		respondPrivate(s, i, utils.ErrorEmbed(locale.Text("games.cups.insufficient_funds")))
		return
	}
	if IsUserInGame(userID) {
		respondPrivate(s, i, utils.ErrorEmbed(locale.Text("games.aviator.you_already_have_an_active_game_in")))
		return
	}

	job := GameJob{
		UserID: userID,
		OnQueue: func(pos int) {
			if pos == -1 {
				respondPrivate(s, i, utils.ErrorEmbed(locale.Text("games.aviator.you_already_have_an_active_game_in")))
			}
		},
		Run: func(finishChan chan struct{}) {
			defer close(finishChan)
			defer cleanupCup(userID)

			if database.GetBalance(guildID, userID) < bet {
				respondPrivate(s, i, utils.ErrorEmbed(locale.Text("games.cups.you_ran_out_of_funds_before_starting")))
				return
			}

			// Deduct initial bet atomically
			if err := database.CollectLostBet(guildID, userID, bet); err != nil {
				respondPrivate(s, i, utils.ErrorEmbed(locale.Text("games.cups.error_processing_bet")))
				return
			}

			embed, rows := buildCupRoundUI(1, bet, 6, userID)
			err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content:    locale.Text("games.cups.it_s_your_turn.formatted", locale.Data{"UserID": userID}),
					Embeds:     []*discordgo.MessageEmbed{embed},
					Components: rows,
				},
			})

			if err != nil {
				// Refund immediately on failure
				_ = database.AddCoins(guildID, userID, bet)
				return
			}

			msg, err := s.InteractionResponse(i.Interaction)
			if err != nil || msg == nil {
				_ = database.AddCoins(guildID, userID, bet)
				return
			}

			runCupGameLoop(s, guildID, userID, bet, i.ChannelID, msg.ID)
		},
	}

	Enqueue(job)
}

func buildCupRoundUI(round int, currentPot int, numCups int, userID string) (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
	embed := utils.NewEmbed()
	embed.Title = locale.Text("games.cups.cup_game_round.formatted", locale.Data{"Round": round})

	multiplierText := "5x"
	if round > 1 {
		multiplierText = locale.Text("games.cups.x_double_or_nothing")
	}

	embed.Description = locale.Text("games.cups.current_pot_round_multiplier_guess_which_cup.formatted", locale.Data{"CurrentPot": currentPot, "CurrencySymbol": config.Bot.CurrencySymbol, "MultiplierText": multiplierText, "NumCups": numCups})
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

func runCupGameLoop(s *discordgo.Session, guildID, userID string, bet int, channelID string, gameMsgID string) {
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
			timeoutEmbed := utils.ErrorEmbed(locale.Text("games.cups.game_timed_out_took_too_long_to.formatted", locale.Data{"UserID": userID}))
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
			embed.Title = locale.Text("games.cups.correct")
			embed.Color = utils.ColorGreen

			embed.Description = locale.Text("games.cups.the_coin_was_in_cup_current_pot.formatted", locale.Data{"WinningCup": winningCup, "CurrentPot": currentPot, "CurrencySymbol": config.Bot.CurrencySymbol, "NetProfit": netProfit})

			actionRow := discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					discordgo.Button{
						Label:    locale.Text("games.cups.cash_out"),
						Style:    discordgo.SuccessButton,
						CustomID: fmt.Sprintf("cup_cashout_%s", userID),
					},
					discordgo.Button{
						Label:    locale.Text("games.cups.continue_double_or_nothing"),
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
					_ = database.AddCoins(guildID, userID, currentPot)
					winEmbed := utils.SuccessEmbed(locale.Text("games.aviator.cashed_out"),
						locale.Text("games.cups.congratulations_walked_away_with_net_profit.formatted", locale.Data{"UserID": userID, "CurrentPot": currentPot, "CurrencySymbol": config.Bot.CurrencySymbol, "NetProfit": netProfit, "CurrencySymbol5": config.Bot.CurrencySymbol}))

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
				_ = database.AddCoins(guildID, userID, currentPot)
				autoEmbed := utils.SuccessEmbed(locale.Text("games.cups.auto_cash_out"),
					locale.Text("games.cups.time_expired_automatically_cashed_out_for.formatted", locale.Data{"CurrentPot": currentPot, "CurrencySymbol": config.Bot.CurrencySymbol, "UserID": userID}))

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
			embed.Title = locale.Text("games.cups.wrong_cup")
			embed.Color = utils.ColorRed
			embed.Description = locale.Text("games.cups.you_picked_cup_but_the_coin_was.formatted", locale.Data{"Choice": choice, "WinningCup": winningCup, "Bet": bet, "CurrencySymbol": config.Bot.CurrencySymbol})

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
				Content: locale.Text("games.cups.this_is_not_your_game_only_can.formatted", locale.Data{"ExpectedUserID": expectedUserID}),
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
				Content: locale.Text("games.cups.this_game_has_already_ended"),
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
			Label:    locale.Text("games.cups.cup.formatted", locale.Data{"I": i}),
			Style:    discordgo.SecondaryButton,
			Emoji:    &discordgo.ComponentEmoji{Name: "🥤"},
			CustomID: fmt.Sprintf("cup_pick_%d_%s", i, userID),
		})
	}
	return btns
}

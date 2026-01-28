package games

import (
	"estudocoin/internal/database"
	"estudocoin/pkg/config"
	"estudocoin/pkg/utils"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

const MinBlackjackBet = 100

type Card struct {
	Rank  string
	Suit  string
	Value int
}

type deck []Card

var (
	activeBlackjackGames = make(map[string]chan *discordgo.InteractionCreate)
	blackjackMutex       sync.Mutex
)

func newDeck() deck {
	ranks := []string{"2", "3", "4", "5", "6", "7", "8", "9", "10", "J", "Q", "K", "A"}
	suits := []string{"♠️", "♦️", "♣️", "♥️"}
	values := []int{2, 3, 4, 5, 6, 7, 8, 9, 10, 10, 10, 10, 11}
	d := make(deck, 0)

	for _, suit := range suits {
		for i, rank := range ranks {
			d = append(d, Card{Rank: rank, Suit: suit, Value: values[i]})
		}
	}
	return d
}

func (d deck) shuffle() {
	rand.Seed(time.Now().UnixNano())
	rand.Shuffle(len(d), func(i, j int) {
		d[i], d[j] = d[j], d[i]
	})
}

func deal(d *deck) Card {
	card := (*d)[0]
	*d = (*d)[1:]
	return card
}

func calculateHand(hand []Card) int {
	value := 0
	aces := 0
	for _, card := range hand {
		value += card.Value
		if card.Rank == "A" {
			aces++
		}
	}
	for value > 21 && aces > 0 {
		value -= 10
		aces--
	}
	return value
}

func renderHand(hand []Card) string {
	var s strings.Builder
	for _, card := range hand {
		s.WriteString(fmt.Sprintf("`%s%s` ", card.Rank, card.Suit))
	}
	return s.String()
}

func StartBlackjackTextMessage(s *discordgo.Session, m *discordgo.MessageCreate, bet int) {
	startBlackjackGame(s, m.Author.ID, bet, m.ChannelID, func(msg *discordgo.MessageSend) (*discordgo.Message, error) {
		return s.ChannelMessageSendComplex(m.ChannelID, msg)
	})
}

func StartBlackjackInteraction(s *discordgo.Session, i *discordgo.InteractionCreate, bet int) {
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: "Starting your blackjack game...",
		},
	})
	startBlackjackGame(s, i.Member.User.ID, bet, i.ChannelID, func(msg *discordgo.MessageSend) (*discordgo.Message, error) {
		return s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content:    &msg.Content,
			Embeds:     &msg.Embeds,
			Components: &msg.Components,
		})
	})
}

func startBlackjackGame(s *discordgo.Session, userID string, bet int, channelID string, initialResponder func(*discordgo.MessageSend) (*discordgo.Message, error)) {
	if bet < MinBlackjackBet {
		s.ChannelMessageSend(channelID, fmt.Sprintf("❌ Minimum bet is %d %s", MinBlackjackBet, config.Bot.CurrencySymbol))
		return
	}
	if database.GetBalance(userID) < bet {
		s.ChannelMessageSend(channelID, "❌ Insufficient funds.")
		return
	}

	database.RemoveCoins(userID, bet)

	gameChan := make(chan *discordgo.InteractionCreate)
	blackjackMutex.Lock()
	activeBlackjackGames[userID] = gameChan
	blackjackMutex.Unlock()

	defer func() {
		blackjackMutex.Lock()
		delete(activeBlackjackGames, userID)
		blackjackMutex.Unlock()
	}()

	d := newDeck()
	d.shuffle()

	playerHand := []Card{deal(&d), deal(&d)}
	dealerHand := []Card{deal(&d), deal(&d)}

	playerScore := calculateHand(playerHand)

	embed := utils.NewEmbed()
	embed.Title = "♦️ Blackjack"
	embed.Description = fmt.Sprintf("Bet: **%d %s**", bet, config.Bot.CurrencySymbol)
	embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
		Name:   "Your Hand",
		Value:  fmt.Sprintf("%s\nValue: **%d**", renderHand(playerHand), playerScore),
		Inline: true,
	})
	embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
		Name:   "Dealer's Hand",
		Value:  fmt.Sprintf("`%s%s` `?`", dealerHand[0].Rank, dealerHand[0].Suit),
		Inline: true,
	})
	embed.Color = utils.ColorGold

	buttons := []discordgo.MessageComponent{
		discordgo.Button{
			Label:    "Hit",
			Style:    discordgo.PrimaryButton,
			CustomID: "blackjack_hit_" + userID,
		},
		discordgo.Button{
			Label:    "Stand",
			Style:    discordgo.SuccessButton,
			CustomID: "blackjack_stand_" + userID,
		},
	}

	actionRow := discordgo.ActionsRow{Components: buttons}

	msg, err := initialResponder(&discordgo.MessageSend{
		Content:    fmt.Sprintf("<@%s>, your turn!", userID),
		Embeds:     []*discordgo.MessageEmbed{embed},
		Components: []discordgo.MessageComponent{actionRow},
	})
	if err != nil {
		return
	}

	if playerScore == 21 {
		endGame(s, msg.ID, channelID, userID, playerHand, dealerHand, bet, d, true)
		return
	}

	for {
		select {
		case interaction := <-gameChan:
			s.InteractionRespond(interaction.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseDeferredMessageUpdate,
			})

			if strings.Contains(interaction.MessageComponentData().CustomID, "hit") {
				playerHand = append(playerHand, deal(&d))
				playerScore = calculateHand(playerHand)

				embed.Fields[0].Value = fmt.Sprintf("%s\nValue: **%d**", renderHand(playerHand), playerScore)

				if playerScore > 21 {
					embed.Color = utils.ColorRed
					embed.Title = "♦️ Blackjack - BUST!"
					s.ChannelMessageEditComplex(&discordgo.MessageEdit{
						ID:         msg.ID,
						Channel:    channelID,
						Embeds:     &[]*discordgo.MessageEmbed{embed},
						Components: &[]discordgo.MessageComponent{},
					})
					return
				}

				s.ChannelMessageEditComplex(&discordgo.MessageEdit{
					ID:      msg.ID,
					Channel: channelID,
					Embeds:  &[]*discordgo.MessageEmbed{embed},
				})

			} else if strings.Contains(interaction.MessageComponentData().CustomID, "stand") {
				endGame(s, msg.ID, channelID, userID, playerHand, dealerHand, bet, d, false)
				return
			}

		case <-time.After(2 * time.Minute):
			s.ChannelMessageEdit(channelID, msg.ID, "⏰ Game timed out. You lost your bet.")
			return
		}
	}
}

func endGame(s *discordgo.Session, msgID, channelID, userID string, playerHand, dealerHand []Card, bet int, d deck, isBlackjack bool) {
	playerScore := calculateHand(playerHand)
	dealerScore := calculateHand(dealerHand)

	for dealerScore < 17 {
		dealerHand = append(dealerHand, deal(&d))
		dealerScore = calculateHand(dealerHand)
	}

	embed := utils.NewEmbed()
	embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
		Name:   "Your Hand",
		Value:  fmt.Sprintf("%s\nValue: **%d**", renderHand(playerHand), playerScore),
		Inline: true,
	})
	embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
		Name:   "Dealer's Hand",
		Value:  fmt.Sprintf("%s\nValue: **%d**", renderHand(dealerHand), dealerScore),
		Inline: true,
	})

	if isBlackjack {
		winnings := int(float64(bet) * 2.5)
		database.AddCoins(userID, winnings)
		embed.Title = "♠️ BLACKJACK!"
		embed.Description = fmt.Sprintf("You win **%d %s**!", winnings, config.Bot.CurrencySymbol)
		embed.Color = utils.ColorGold
	} else if playerScore > 21 {
		embed.Title = "❌ BUST! You Lose"
		embed.Description = fmt.Sprintf("You lost **%d %s**.", bet, config.Bot.CurrencySymbol)
		embed.Color = utils.ColorRed
	} else if dealerScore > 21 || playerScore > dealerScore {
		winnings := bet * 2
		database.AddCoins(userID, winnings)
		embed.Title = "🎉 You Win!"
		embed.Description = fmt.Sprintf("You win **%d %s**!", winnings, config.Bot.CurrencySymbol)
		embed.Color = utils.ColorGreen
	} else if playerScore < dealerScore {
		embed.Title = "❌ You Lose"
		embed.Description = fmt.Sprintf("You lost **%d %s**.", bet, config.Bot.CurrencySymbol)
		embed.Color = utils.ColorRed
	} else {
		database.AddCoins(userID, bet) // Push
		embed.Title = " PUSH"
		embed.Description = "It's a tie. Your bet has been returned."
		embed.Color = 0x808080
	}

	s.ChannelMessageEditComplex(&discordgo.MessageEdit{
		ID:         msgID,
		Channel:    channelID,
		Embeds:     &[]*discordgo.MessageEmbed{embed},
		Components: &[]discordgo.MessageComponent{},
	})
}

func HandleBlackjackInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	userID := i.Member.User.ID
	blackjackMutex.Lock()
	ch, exists := activeBlackjackGames[userID]
	blackjackMutex.Unlock()

	if !exists {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{Content: "⚠️ This is not your game.", Flags: discordgo.MessageFlagsEphemeral},
		})
		return
	}

	select {
	case ch <- i:
	default:
	}
}
package games

import (
	"crypto/rand"
	"encoding/binary"
	"bot/internal/database"
	"bot/pkg/config"
	"bot/pkg/utils"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

type Card struct {
	Suit  string
	Value string
	Score int
}

type Hand struct {
	Cards []Card
	Score int
	Aces  int
}

type BlackjackGame struct {
	UserID           string
	Bet              int
	PlayerHand       Hand
	SplitHand        Hand
	IsSplit          bool
	ActiveHand       int // 0 for PlayerHand, 1 for SplitHand
	SplitBet         int
	SplitDoubledDown bool
	DealerHand       Hand
	Deck             []Card
	Status           string // "playing", "player_bust", "dealer_bust", "player_win", "dealer_win", "push", "blackjack", "surrender", "split_ended"
	MessageID        string
	ChannelID        string
	Insurance        bool
	InsuranceBet     int
	DoubledDown      bool
	Timer            *time.Timer
	mu               sync.Mutex
}

var (
	activeBlackjackGames = make(map[string]*BlackjackGame)
	blackjackMu          sync.Mutex
)

// Card suits and values
var (
	suits  = []string{"♠️", "♥️", "♦️", "♣️"}
	values = []string{"A", "2", "3", "4", "5", "6", "7", "8", "9", "10", "J", "Q", "K"}
)

// createDeck creates a shuffled 4-deck casino shoe (208 cards) using cryptographic randomness
func createDeck() []Card {
	const numDecks = 4
	deck := make([]Card, 0, 52*numDecks)

	for d := 0; d < numDecks; d++ {
		for _, suit := range suits {
			for _, value := range values {
				score := 0
				switch value {
				case "A":
					score = 11 // Ace starts at 11
				case "J", "Q", "K":
					score = 10
				default:
					score = parseInt(value)
				}

				deck = append(deck, Card{
					Suit:  suit,
					Value: value,
					Score: score,
				})
			}
		}
	}

	// Cryptographic Fisher-Yates shuffle
	for i := len(deck) - 1; i > 0; i-- {
		var b [4]byte
		_, _ = rand.Read(b[:])
		j := int(binary.LittleEndian.Uint32(b[:]) % uint32(i+1))
		deck[i], deck[j] = deck[j], deck[i]
	}

	return deck
}

func parseInt(s string) int {
	switch s {
	case "2":
		return 2
	case "3":
		return 3
	case "4":
		return 4
	case "5":
		return 5
	case "6":
		return 6
	case "7":
		return 7
	case "8":
		return 8
	case "9":
		return 9
	case "10":
		return 10
	default:
		return 0
	}
}

// Deal a card from the deck with safety replenishment to prevent index out of range panics
func (g *BlackjackGame) dealCard() Card {
	if len(g.Deck) == 0 {
		g.Deck = createDeck()
	}
	card := g.Deck[0]
	g.Deck = g.Deck[1:]
	return card
}

// Calculate hand score considering soft and hard aces
func calculateScore(hand *Hand) {
	hand.Score = 0
	hand.Aces = 0

	for _, card := range hand.Cards {
		hand.Score += card.Score
		if card.Value == "A" {
			hand.Aces++
		}
	}

	// Adjust for aces if busting
	for hand.Score > 21 && hand.Aces > 0 {
		hand.Score -= 10 // Convert ace from 11 to 1
		hand.Aces--
	}
}

// Format hand as string for display
func formatHand(hand Hand, hideFirst bool) string {
	var cards []string
	for i, card := range hand.Cards {
		if hideFirst && i == 0 {
			cards = append(cards, "🂠")
		} else {
			cards = append(cards, fmt.Sprintf("%s%s", card.Value, card.Suit))
		}
	}
	return strings.Join(cards, " ")
}

// Check if hand is blackjack
func isBlackjack(hand Hand) bool {
	return len(hand.Cards) == 2 && hand.Score == 21
}

// StartBlackjackGame starts a blackjack game from a slash command
func StartBlackjackGame(s *discordgo.Session, i *discordgo.InteractionCreate, bet int) {
	userID := i.Member.User.ID

	// Check if user already has an active game
	blackjackMu.Lock()
	if _, exists := activeBlackjackGames[userID]; exists {
		blackjackMu.Unlock()
		respondEmbed(s, i, utils.ErrorEmbed("You already have an active Blackjack game! Finish it before starting another."))
		return
	}
	blackjackMu.Unlock()

	// Validate bet
	if bet < 10 {
		respondEmbed(s, i, utils.ErrorEmbed(fmt.Sprintf("Minimum bet is 10 %s", config.Bot.CurrencySymbol)))
		return
	}

	balance := database.GetBalance(userID)
	if balance < bet {
		respondEmbed(s, i, utils.ErrorEmbed(fmt.Sprintf("Insufficient balance! You have %d %s", balance, config.Bot.CurrencySymbol)))
		return
	}

	// Deduct bet atomically
	if err := database.CollectLostBet(userID, bet); err != nil {
		respondEmbed(s, i, utils.ErrorEmbed("Error deducting bet."))
		return
	}

	// Initialize game
	game := &BlackjackGame{
		UserID:    userID,
		Bet:       bet,
		Deck:      createDeck(),
		Status:    "playing",
		ChannelID: i.ChannelID,
	}

	// Deal initial cards
	game.PlayerHand.Cards = append(game.PlayerHand.Cards, game.dealCard())
	game.DealerHand.Cards = append(game.DealerHand.Cards, game.dealCard())
	game.PlayerHand.Cards = append(game.PlayerHand.Cards, game.dealCard())
	game.DealerHand.Cards = append(game.DealerHand.Cards, game.dealCard())

	calculateScore(&game.PlayerHand)
	calculateScore(&game.DealerHand)

	playerBJ := isBlackjack(game.PlayerHand)
	dealerShowsTen := game.DealerHand.Cards[1].Score == 10
	dealerBJ := isBlackjack(game.DealerHand)

	// Immediate resolution rules:
	// 1. Natural Blackjack always resolves immediately:
	//    - Push if dealer also has Blackjack
	//    - 3:2 payout if dealer does not have Blackjack (never plays or pushes against non-BJ)
	// 2. If dealer shows 10 and has Blackjack, dealer wins immediately.
	// 3. If dealer shows Ace and player has no BJ, game continues to offer insurance.
	if playerBJ {
		if dealerBJ {
			game.Status = "push"
		} else {
			game.Status = "blackjack"
		}
		game.settleInitialSlashEnd(s, i)
		return
	} else if dealerShowsTen && dealerBJ {
		game.Status = "dealer_win"
		game.settleInitialSlashEnd(s, i)
		return
	}

	// Store game and attach inactivity timer (2 minutes)
	blackjackMu.Lock()
	activeBlackjackGames[userID] = game
	blackjackMu.Unlock()

	game.Timer = time.AfterFunc(2*time.Minute, func() {
		game.handleInactivityTimeout(s)
	})

	// Send initial game state
	embed := game.createGameEmbed(false)
	components := game.createActionButtons()

	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
		},
	})

	if err != nil {
		// Cleanup and refund on error
		game.stopTimer()
		blackjackMu.Lock()
		delete(activeBlackjackGames, userID)
		blackjackMu.Unlock()
		_ = database.AddCoins(userID, bet)
	}
}

// StartBlackjackText starts a blackjack game from a text command
func StartBlackjackText(s *discordgo.Session, m *discordgo.MessageCreate, bet int) {
	userID := m.Author.ID

	// Check if user already has an active game
	blackjackMu.Lock()
	if _, exists := activeBlackjackGames[userID]; exists {
		blackjackMu.Unlock()
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("You already have an active Blackjack game! Finish it before starting another."))
		return
	}
	blackjackMu.Unlock()

	// Validate bet
	if bet < 10 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed(fmt.Sprintf("Minimum bet is 10 %s", config.Bot.CurrencySymbol)))
		return
	}

	balance := database.GetBalance(userID)
	if balance < bet {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed(fmt.Sprintf("Insufficient balance! You have %d %s", balance, config.Bot.CurrencySymbol)))
		return
	}

	// Deduct bet atomically
	if err := database.CollectLostBet(userID, bet); err != nil {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Error deducting bet."))
		return
	}

	// Initialize game
	game := &BlackjackGame{
		UserID:    userID,
		Bet:       bet,
		Deck:      createDeck(),
		Status:    "playing",
		ChannelID: m.ChannelID,
	}

	// Deal initial cards
	game.PlayerHand.Cards = append(game.PlayerHand.Cards, game.dealCard())
	game.DealerHand.Cards = append(game.DealerHand.Cards, game.dealCard())
	game.PlayerHand.Cards = append(game.PlayerHand.Cards, game.dealCard())
	game.DealerHand.Cards = append(game.DealerHand.Cards, game.dealCard())

	calculateScore(&game.PlayerHand)
	calculateScore(&game.DealerHand)

	playerBJ := isBlackjack(game.PlayerHand)
	dealerShowsTen := game.DealerHand.Cards[1].Score == 10
	dealerBJ := isBlackjack(game.DealerHand)

	// Immediate resolution rules:
	// 1. Natural Blackjack always resolves immediately:
	//    - Push if dealer also has Blackjack
	//    - 3:2 payout if dealer does not have Blackjack (never plays or pushes against non-BJ)
	// 2. If dealer shows 10 and has Blackjack, dealer wins immediately.
	// 3. If dealer shows Ace and player has no BJ, game continues to offer insurance.
	if playerBJ {
		if dealerBJ {
			game.Status = "push"
		} else {
			game.Status = "blackjack"
		}
		game.endGameText(s, m.ChannelID)
		return
	} else if dealerShowsTen && dealerBJ {
		game.Status = "dealer_win"
		game.endGameText(s, m.ChannelID)
		return
	}

	// Store game and attach inactivity timer (2 minutes)
	blackjackMu.Lock()
	activeBlackjackGames[userID] = game
	blackjackMu.Unlock()

	game.Timer = time.AfterFunc(2*time.Minute, func() {
		game.handleInactivityTimeout(s)
	})

	// Send initial game state
	embed := game.createGameEmbed(false)
	embed.Footer.Text = fmt.Sprintf("Use: !bj hit | !bj stand | !bj double | !bj surrender (User: %s)", m.Author.Username)
	components := game.createActionButtons()

	msg, err := s.ChannelMessageSendComplex(m.ChannelID, &discordgo.MessageSend{
		Content:    fmt.Sprintf("<@%s> Your Blackjack game started!", userID),
		Embeds:     []*discordgo.MessageEmbed{embed},
		Components: components,
	})

	if err != nil {
		game.stopTimer()
		blackjackMu.Lock()
		delete(activeBlackjackGames, userID)
		blackjackMu.Unlock()
		_ = database.AddCoins(userID, bet)
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Failed to start game."))
		return
	}

	game.MessageID = msg.ID
}

func (g *BlackjackGame) stopTimer() {
	if g.Timer != nil {
		g.Timer.Stop()
	}
}

func (g *BlackjackGame) resetTimer(s *discordgo.Session) {
	if g.Timer != nil {
		g.Timer.Stop()
	}
	g.Timer = time.AfterFunc(2*time.Minute, func() {
		g.handleInactivityTimeout(s)
	})
}

func (g *BlackjackGame) handleInactivityTimeout(s *discordgo.Session) {
	g.mu.Lock()
	if g.Status != "playing" {
		g.mu.Unlock()
		return
	}

	// Auto-stand
	if g.IsSplit {
		if g.PlayerHand.Score > 21 && g.SplitHand.Score > 21 {
			g.Status = "split_ended"
		} else {
			g.playDealer()
		}
	} else {
		g.playDealer()
	}
	embed := g.buildGameOverEmbed()
	channelID := g.ChannelID
	messageID := g.MessageID
	g.mu.Unlock()

	// Clean up game
	blackjackMu.Lock()
	delete(activeBlackjackGames, g.UserID)
	blackjackMu.Unlock()

	if s != nil && channelID != "" {
		timeoutNotice := fmt.Sprintf("⏰ <@%s> Inactivity timeout (2 min). Auto-stand executed!", g.UserID)
		if messageID != "" {
			_, _ = s.ChannelMessageEditComplex(&discordgo.MessageEdit{
				ID:         messageID,
				Channel:    channelID,
				Content:    &timeoutNotice,
				Embeds:     &[]*discordgo.MessageEmbed{embed},
				Components: &[]discordgo.MessageComponent{},
			})
		} else {
			_, _ = s.ChannelMessageSend(channelID, timeoutNotice)
			_, _ = s.ChannelMessageSendEmbed(channelID, embed)
		}
	}
}

// Settle immediate slash end correctly using ChannelMessageWithSource
func (g *BlackjackGame) settleInitialSlashEnd(s *discordgo.Session, i *discordgo.InteractionCreate) {
	embed := g.buildGameOverEmbed()

	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: []discordgo.MessageComponent{},
		},
	})
}

// Create game embed
func (g *BlackjackGame) createGameEmbed(showDealer bool) *discordgo.MessageEmbed {
	dealerScore := "?"
	if showDealer {
		dealerScore = fmt.Sprintf("%d", g.DealerHand.Score)
	}

	totalBetStr := fmt.Sprintf("%d %s", g.Bet, config.Bot.CurrencySymbol)
	if g.IsSplit {
		totalBetStr = fmt.Sprintf("%d %s (%d + %d)", g.Bet+g.SplitBet, config.Bot.CurrencySymbol, g.Bet, g.SplitBet)
	}
	if g.Insurance {
		totalBetStr += fmt.Sprintf(" *(+ %d %s insurance)*", g.InsuranceBet, config.Bot.CurrencySymbol)
	}

	var fields []*discordgo.MessageEmbedField
	fields = append(fields, &discordgo.MessageEmbedField{
		Name:   "💰 Bet",
		Value:  totalBetStr,
		Inline: true,
	})
	fields = append(fields, &discordgo.MessageEmbedField{
		Name:   "🎰 Dealer's Hand",
		Value:  fmt.Sprintf("%s\nScore: **%s**", formatHand(g.DealerHand, !showDealer), dealerScore),
		Inline: false,
	})

	if g.IsSplit {
		h1Tag := ""
		h2Tag := ""
		if g.ActiveHand == 0 {
			h1Tag = " 🎯 **[ACTIVE]**"
		} else {
			h2Tag = " 🎯 **[ACTIVE]**"
		}

		h1Val := fmt.Sprintf("%s\nScore: **%d**", formatHand(g.PlayerHand, false), g.PlayerHand.Score)
		if g.PlayerHand.Score > 21 {
			h1Val += " *(Bust)*"
		}
		if g.DoubledDown {
			h1Val += " *(Doubled)*"
		}

		h2Val := fmt.Sprintf("%s\nScore: **%d**", formatHand(g.SplitHand, false), g.SplitHand.Score)
		if g.SplitHand.Score > 21 {
			h2Val += " *(Bust)*"
		}
		if g.SplitDoubledDown {
			h2Val += " *(Doubled)*"
		}

		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   fmt.Sprintf("🎴 Hand 1%s", h1Tag),
			Value:  h1Val,
			Inline: true,
		})
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   fmt.Sprintf("🎴 Hand 2%s", h2Tag),
			Value:  h2Val,
			Inline: true,
		})
	} else {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   "🎴 Your Hand",
			Value:  fmt.Sprintf("%s\nScore: **%d**", formatHand(g.PlayerHand, false), g.PlayerHand.Score),
			Inline: false,
		})
	}

	footerText := "Choose your action • Timeout in 2 min"
	if g.IsSplit {
		footerText = fmt.Sprintf("Playing Hand %d • Timeout in 2 min", g.ActiveHand+1)
	}

	embed := &discordgo.MessageEmbed{
		Title:  "🃏 Blackjack",
		Color:  0x2F3136,
		Fields: fields,
		Footer: &discordgo.MessageEmbedFooter{
			Text: footerText,
		},
	}

	return embed
}

// Create action buttons
func (g *BlackjackGame) createActionButtons() []discordgo.MessageComponent {
	buttons := []discordgo.MessageComponent{
		discordgo.Button{
			Label:    "Hit",
			Style:    discordgo.SuccessButton,
			CustomID: fmt.Sprintf("bj_hit_%s", g.UserID),
			Emoji:    &discordgo.ComponentEmoji{Name: "🎯"},
		},
		discordgo.Button{
			Label:    "Stand",
			Style:    discordgo.PrimaryButton,
			CustomID: fmt.Sprintf("bj_stand_%s", g.UserID),
			Emoji:    &discordgo.ComponentEmoji{Name: "✋"},
		},
	}

	if g.IsSplit {
		var activeHand Hand
		doubled := false
		betAmount := g.Bet
		if g.ActiveHand == 0 {
			activeHand = g.PlayerHand
			doubled = g.DoubledDown
		} else {
			activeHand = g.SplitHand
			doubled = g.SplitDoubledDown
			betAmount = g.SplitBet
		}

		if len(activeHand.Cards) == 2 && !doubled {
			balance := database.GetBalance(g.UserID)
			if balance >= betAmount {
				buttons = append(buttons, discordgo.Button{
					Label:    "Double Down",
					Style:    discordgo.SecondaryButton,
					CustomID: fmt.Sprintf("bj_double_%s", g.UserID),
					Emoji:    &discordgo.ComponentEmoji{Name: "💎"},
				})
			}
		}

		return []discordgo.MessageComponent{
			discordgo.ActionsRow{
				Components: buttons,
			},
		}
	}

	// Only allow double down on first two cards
	if len(g.PlayerHand.Cards) == 2 && !g.DoubledDown {
		balance := database.GetBalance(g.UserID)
		if balance >= g.Bet {
			buttons = append(buttons, discordgo.Button{
				Label:    "Double Down",
				Style:    discordgo.SecondaryButton,
				CustomID: fmt.Sprintf("bj_double_%s", g.UserID),
				Emoji:    &discordgo.ComponentEmoji{Name: "💎"},
			})
		}
	}

	// Offer split if opening two cards match in value or score
	if len(g.PlayerHand.Cards) == 2 && !g.DoubledDown && !g.Insurance {
		c0 := g.PlayerHand.Cards[0]
		c1 := g.PlayerHand.Cards[1]
		if c0.Value == c1.Value || c0.Score == c1.Score {
			balance := database.GetBalance(g.UserID)
			if balance >= g.Bet {
				buttons = append(buttons, discordgo.Button{
					Label:    "Split",
					Style:    discordgo.SecondaryButton,
					CustomID: fmt.Sprintf("bj_split_%s", g.UserID),
					Emoji:    &discordgo.ComponentEmoji{Name: "🔀"},
				})
			}
		}
	}

	// Offer insurance if dealer shows an Ace and player hasn't bought it yet
	if len(g.PlayerHand.Cards) == 2 && g.DealerHand.Cards[1].Value == "A" && !g.Insurance {
		insuranceAmount := g.Bet / 2
		balance := database.GetBalance(g.UserID)
		if balance >= insuranceAmount {
			buttons = append(buttons, discordgo.Button{
				Label:    "Insurance",
				Style:    discordgo.SecondaryButton,
				CustomID: fmt.Sprintf("bj_insurance_%s", g.UserID),
				Emoji:    &discordgo.ComponentEmoji{Name: "🛡️"},
			})
		}
	}

	// Offer surrender on opening two cards
	if len(g.PlayerHand.Cards) == 2 && !g.DoubledDown && !g.Insurance {
		buttons = append(buttons, discordgo.Button{
			Label:    "Surrender",
			Style:    discordgo.DangerButton,
			CustomID: fmt.Sprintf("bj_surrender_%s", g.UserID),
			Emoji:    &discordgo.ComponentEmoji{Name: "🏳️"},
		})
	}

	return []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: buttons,
		},
	}
}

// checkDealerBlackjackOnAction peeks at dealer hole card after player acts or buys insurance
func (g *BlackjackGame) checkDealerBlackjackOnAction() bool {
	if isBlackjack(g.DealerHand) {
		if isBlackjack(g.PlayerHand) {
			g.Status = "push"
		} else {
			g.Status = "dealer_win"
		}
		return true
	}
	return false
}

// Handle Hit action
func HandleBlackjackHit(s *discordgo.Session, i *discordgo.InteractionCreate, userID string) {
	if i.Member.User.ID != userID {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "❌ This is not your game!",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	blackjackMu.Lock()
	game, exists := activeBlackjackGames[userID]
	blackjackMu.Unlock()

	if !exists {
		respondEmbed(s, i, utils.ErrorEmbed("No active game found!"))
		return
	}

	game.mu.Lock()
	defer game.mu.Unlock()

	// If player skips insurance when dealer shows Ace, peek dealer BJ first
	if len(game.PlayerHand.Cards) == 2 && game.DealerHand.Cards[1].Value == "A" && !game.Insurance && !game.IsSplit {
		if game.checkDealerBlackjackOnAction() {
			game.stopTimer()
			game.endGameInteraction(s, i)
			return
		}
	}

	game.resetTimer(s)

	if game.IsSplit {
		if game.ActiveHand == 0 {
			card := game.dealCard()
			game.PlayerHand.Cards = append(game.PlayerHand.Cards, card)
			calculateScore(&game.PlayerHand)
			if game.PlayerHand.Score >= 21 {
				// Hand 1 done (bust or 21), switch to Hand 2
				game.ActiveHand = 1
			}
		} else {
			card := game.dealCard()
			game.SplitHand.Cards = append(game.SplitHand.Cards, card)
			calculateScore(&game.SplitHand)
			if game.SplitHand.Score >= 21 {
				// Hand 2 done! Both hands completed.
				game.stopTimer()
				if game.PlayerHand.Score > 21 && game.SplitHand.Score > 21 {
					game.Status = "split_ended"
				} else {
					game.playDealer()
				}
				game.endGameInteraction(s, i)
				return
			}
		}

		embed := game.createGameEmbed(false)
		components := game.createActionButtons()
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Embeds:     []*discordgo.MessageEmbed{embed},
				Components: components,
			},
		})
		return
	}

	// Deal card to player
	card := game.dealCard()
	game.PlayerHand.Cards = append(game.PlayerHand.Cards, card)
	calculateScore(&game.PlayerHand)

	// Check for bust
	if game.PlayerHand.Score > 21 {
		game.Status = "player_bust"
		game.stopTimer()
		game.endGameInteraction(s, i)
		return
	}

	// Update game state
	embed := game.createGameEmbed(false)
	components := game.createActionButtons()

	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
		},
	})
}

// Handle Stand action
func HandleBlackjackStand(s *discordgo.Session, i *discordgo.InteractionCreate, userID string) {
	if i.Member.User.ID != userID {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "❌ This is not your game!",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	blackjackMu.Lock()
	game, exists := activeBlackjackGames[userID]
	blackjackMu.Unlock()

	if !exists {
		respondEmbed(s, i, utils.ErrorEmbed("No active game found!"))
		return
	}

	game.mu.Lock()
	defer game.mu.Unlock()

	// If dealer shows Ace and player skipped insurance, check dealer BJ
	if len(game.PlayerHand.Cards) == 2 && game.DealerHand.Cards[1].Value == "A" && !game.Insurance && !game.IsSplit {
		if game.checkDealerBlackjackOnAction() {
			game.stopTimer()
			game.endGameInteraction(s, i)
			return
		}
	}

	if game.IsSplit {
		if game.ActiveHand == 0 {
			// Hand 1 stands, switch to Hand 2
			game.ActiveHand = 1
			game.resetTimer(s)
			embed := game.createGameEmbed(false)
			components := game.createActionButtons()
			_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseUpdateMessage,
				Data: &discordgo.InteractionResponseData{
					Embeds:     []*discordgo.MessageEmbed{embed},
					Components: components,
				},
			})
			return
		} else {
			// Hand 2 stands! Both done.
			game.stopTimer()
			if game.PlayerHand.Score > 21 && game.SplitHand.Score > 21 {
				game.Status = "split_ended"
			} else {
				game.playDealer()
			}
			game.endGameInteraction(s, i)
			return
		}
	}

	game.stopTimer()

	// Dealer plays
	game.playDealer()
	game.endGameInteraction(s, i)
}

// Handle Double Down action
func HandleBlackjackDouble(s *discordgo.Session, i *discordgo.InteractionCreate, userID string) {
	if i.Member.User.ID != userID {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "❌ This is not your game!",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	blackjackMu.Lock()
	game, exists := activeBlackjackGames[userID]
	blackjackMu.Unlock()

	if !exists {
		respondEmbed(s, i, utils.ErrorEmbed("No active game found!"))
		return
	}

	game.mu.Lock()
	defer game.mu.Unlock()

	if game.IsSplit {
		var activeHand *Hand
		var activeDoubled *bool
		betAmount := game.Bet
		if game.ActiveHand == 0 {
			activeHand = &game.PlayerHand
			activeDoubled = &game.DoubledDown
		} else {
			activeHand = &game.SplitHand
			activeDoubled = &game.SplitDoubledDown
			betAmount = game.SplitBet
		}

		if len(activeHand.Cards) != 2 || *activeDoubled {
			respondEmbed(s, i, utils.ErrorEmbed("Cannot double down on this hand."))
			return
		}

		balance := database.GetBalance(userID)
		if balance < betAmount {
			respondEmbed(s, i, utils.ErrorEmbed("Insufficient balance to double down!"))
			return
		}

		// Deduct additional bet atomically
		if err := database.CollectLostBet(userID, betAmount); err != nil {
			respondEmbed(s, i, utils.ErrorEmbed("Failed to deduct double down bet."))
			return
		}

		if game.ActiveHand == 0 {
			game.Bet *= 2
		} else {
			game.SplitBet *= 2
		}
		*activeDoubled = true

		card := game.dealCard()
		activeHand.Cards = append(activeHand.Cards, card)
		calculateScore(activeHand)

		if game.ActiveHand == 0 {
			// Hand 1 finished after double, switch to Hand 2
			game.ActiveHand = 1
			game.resetTimer(s)
			embed := game.createGameEmbed(false)
			components := game.createActionButtons()
			_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseUpdateMessage,
				Data: &discordgo.InteractionResponseData{
					Embeds:     []*discordgo.MessageEmbed{embed},
					Components: components,
				},
			})
			return
		} else {
			// Hand 2 finished after double!
			game.stopTimer()
			if game.PlayerHand.Score > 21 && game.SplitHand.Score > 21 {
				game.Status = "split_ended"
			} else {
				game.playDealer()
			}
			game.endGameInteraction(s, i)
			return
		}
	}

	if len(game.PlayerHand.Cards) != 2 || game.DoubledDown {
		respondEmbed(s, i, utils.ErrorEmbed("Cannot double down now."))
		return
	}

	balance := database.GetBalance(userID)
	if balance < game.Bet {
		respondEmbed(s, i, utils.ErrorEmbed("Insufficient balance to double down!"))
		return
	}

	// If dealer shows Ace and player skipped insurance, check dealer BJ FIRST (protect double bet)
	if game.DealerHand.Cards[1].Value == "A" && !game.Insurance {
		if game.checkDealerBlackjackOnAction() {
			game.stopTimer()
			game.endGameInteraction(s, i)
			return
		}
	}

	// Deduct additional bet atomically
	if err := database.CollectLostBet(userID, game.Bet); err != nil {
		respondEmbed(s, i, utils.ErrorEmbed("Failed to deduct double down bet."))
		return
	}

	game.Bet *= 2
	game.DoubledDown = true
	game.stopTimer()

	// Deal one card and stand
	card := game.dealCard()
	game.PlayerHand.Cards = append(game.PlayerHand.Cards, card)
	calculateScore(&game.PlayerHand)

	// Check for bust
	if game.PlayerHand.Score > 21 {
		game.Status = "player_bust"
		game.endGameInteraction(s, i)
		return
	}

	// Dealer plays
	game.playDealer()
	game.endGameInteraction(s, i)
}

// Handle Split action
func HandleBlackjackSplit(s *discordgo.Session, i *discordgo.InteractionCreate, userID string) {
	if i.Member.User.ID != userID {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "❌ This is not your game!",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	blackjackMu.Lock()
	game, exists := activeBlackjackGames[userID]
	blackjackMu.Unlock()

	if !exists {
		respondEmbed(s, i, utils.ErrorEmbed("No active game found!"))
		return
	}

	game.mu.Lock()
	defer game.mu.Unlock()

	if game.IsSplit || len(game.PlayerHand.Cards) != 2 || game.DoubledDown || game.Insurance {
		respondEmbed(s, i, utils.ErrorEmbed("Cannot split now."))
		return
	}

	// Check if cards have equal value or score
	c0 := game.PlayerHand.Cards[0]
	c1 := game.PlayerHand.Cards[1]
	if c0.Value != c1.Value && c0.Score != c1.Score {
		respondEmbed(s, i, utils.ErrorEmbed("You can only split cards of the same rank or value!"))
		return
	}

	balance := database.GetBalance(userID)
	if balance < game.Bet {
		respondEmbed(s, i, utils.ErrorEmbed("Insufficient balance to split!"))
		return
	}

	// If dealer shows Ace and player skipped insurance, check dealer BJ first
	if game.DealerHand.Cards[1].Value == "A" && !game.Insurance {
		if game.checkDealerBlackjackOnAction() {
			game.stopTimer()
			game.endGameInteraction(s, i)
			return
		}
	}

	// Deduct split bet atomically
	if err := database.CollectLostBet(userID, game.Bet); err != nil {
		respondEmbed(s, i, utils.ErrorEmbed("Failed to deduct split bet."))
		return
	}

	game.IsSplit = true
	game.ActiveHand = 0
	game.SplitBet = game.Bet

	// Deal 1 card to each split hand
	game.PlayerHand.Cards = []Card{c0, game.dealCard()}
	game.SplitHand.Cards = []Card{c1, game.dealCard()}
	calculateScore(&game.PlayerHand)
	calculateScore(&game.SplitHand)

	// If Hand 1 immediately has 21, advance to Hand 2
	if game.PlayerHand.Score == 21 {
		game.ActiveHand = 1
		// If Hand 2 also immediately has 21, both hands are finished!
		if game.SplitHand.Score == 21 {
			game.stopTimer()
			game.playDealer()
			game.endGameInteraction(s, i)
			return
		}
	}

	game.resetTimer(s)

	embed := game.createGameEmbed(false)
	components := game.createActionButtons()

	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
		},
	})
}

// Handle Insurance action
func HandleBlackjackInsurance(s *discordgo.Session, i *discordgo.InteractionCreate, userID string) {
	if i.Member.User.ID != userID {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "❌ This is not your game!",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	blackjackMu.Lock()
	game, exists := activeBlackjackGames[userID]
	blackjackMu.Unlock()

	if !exists {
		respondEmbed(s, i, utils.ErrorEmbed("No active game found!"))
		return
	}

	game.mu.Lock()
	defer game.mu.Unlock()

	if game.Insurance || game.DealerHand.Cards[1].Value != "A" {
		respondEmbed(s, i, utils.ErrorEmbed("Insurance is not available."))
		return
	}

	insuranceAmount := game.Bet / 2
	balance := database.GetBalance(userID)
	if balance < insuranceAmount {
		respondEmbed(s, i, utils.ErrorEmbed("Insufficient balance for insurance!"))
		return
	}

	if err := database.CollectLostBet(userID, insuranceAmount); err != nil {
		respondEmbed(s, i, utils.ErrorEmbed("Failed to process insurance bet."))
		return
	}

	game.Insurance = true
	game.InsuranceBet = insuranceAmount
	game.resetTimer(s)

	// Now peek at dealer hole card
	if isBlackjack(game.DealerHand) {
		// Dealer has Blackjack! Insurance pays 2:1 and hand ends
		if isBlackjack(game.PlayerHand) {
			game.Status = "push"
		} else {
			game.Status = "dealer_win"
		}
		game.stopTimer()
		game.endGameInteraction(s, i)
		return
	}

	// Dealer does not have BJ, hand continues without dealer hole card revealed
	embed := game.createGameEmbed(false)
	embed.Footer.Text = fmt.Sprintf("🛡️ Insurance purchased (-%d %s). Dealer does NOT have Blackjack!", insuranceAmount, config.Bot.CurrencySymbol)
	components := game.createActionButtons()

	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
		},
	})
}

// Handle Surrender action
func HandleBlackjackSurrender(s *discordgo.Session, i *discordgo.InteractionCreate, userID string) {
	if i.Member.User.ID != userID {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "❌ This is not your game!",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	blackjackMu.Lock()
	game, exists := activeBlackjackGames[userID]
	blackjackMu.Unlock()

	if !exists {
		respondEmbed(s, i, utils.ErrorEmbed("No active game found!"))
		return
	}

	game.mu.Lock()
	defer game.mu.Unlock()

	if len(game.PlayerHand.Cards) != 2 || game.DoubledDown || game.Insurance {
		respondEmbed(s, i, utils.ErrorEmbed("Surrender is only allowed on your opening two cards."))
		return
	}

	game.Status = "surrender"
	game.stopTimer()
	game.endGameInteraction(s, i)
}

// Handle text action (!bj hit, !bj stand, !bj double, !bj insurance, !bj surrender)
func HandleBlackjackTextAction(s *discordgo.Session, m *discordgo.MessageCreate, action string) {
	userID := m.Author.ID

	blackjackMu.Lock()
	game, exists := activeBlackjackGames[userID]
	blackjackMu.Unlock()

	if !exists {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("You don't have an active Blackjack game! Use `!bj <amount>` to start."))
		return
	}

	game.mu.Lock()
	defer game.mu.Unlock()

	switch strings.ToLower(action) {
	case "hit":
		if len(game.PlayerHand.Cards) == 2 && game.DealerHand.Cards[1].Value == "A" && !game.Insurance && !game.IsSplit {
			if game.checkDealerBlackjackOnAction() {
				game.stopTimer()
				game.endGameText(s, m.ChannelID)
				return
			}
		}

		game.resetTimer(s)

		if game.IsSplit {
			if game.ActiveHand == 0 {
				card := game.dealCard()
				game.PlayerHand.Cards = append(game.PlayerHand.Cards, card)
				calculateScore(&game.PlayerHand)
				if game.PlayerHand.Score >= 21 {
					game.ActiveHand = 1
				}
			} else {
				card := game.dealCard()
				game.SplitHand.Cards = append(game.SplitHand.Cards, card)
				calculateScore(&game.SplitHand)
				if game.SplitHand.Score >= 21 {
					game.stopTimer()
					if game.PlayerHand.Score > 21 && game.SplitHand.Score > 21 {
						game.Status = "split_ended"
					} else {
						game.playDealer()
					}
					game.endGameText(s, m.ChannelID)
					return
				}
			}

			embed := game.createGameEmbed(false)
			embed.Footer.Text = fmt.Sprintf("Use: !bj hit | !bj stand | !bj double (Playing Hand %d - %s)", game.ActiveHand+1, m.Author.Username)
			components := game.createActionButtons()

			if game.MessageID != "" {
				_, _ = s.ChannelMessageEditComplex(&discordgo.MessageEdit{
					ID:         game.MessageID,
					Channel:    game.ChannelID,
					Embeds:     &[]*discordgo.MessageEmbed{embed},
					Components: &components,
				})
			} else {
				_, _ = s.ChannelMessageSendEmbed(m.ChannelID, embed)
			}
			return
		}

		card := game.dealCard()
		game.PlayerHand.Cards = append(game.PlayerHand.Cards, card)
		calculateScore(&game.PlayerHand)

		if game.PlayerHand.Score > 21 {
			game.Status = "player_bust"
			game.stopTimer()
			game.endGameText(s, m.ChannelID)
			return
		}

		embed := game.createGameEmbed(false)
		embed.Footer.Text = fmt.Sprintf("Use: !bj hit | !bj stand | !bj double (User: %s)", m.Author.Username)
		components := game.createActionButtons()

		if game.MessageID != "" {
			_, _ = s.ChannelMessageEditComplex(&discordgo.MessageEdit{
				ID:         game.MessageID,
				Channel:    game.ChannelID,
				Embeds:     &[]*discordgo.MessageEmbed{embed},
				Components: &components,
			})
		} else {
			_, _ = s.ChannelMessageSendEmbed(m.ChannelID, embed)
		}

	case "stand":
		game.stopTimer()
		if len(game.PlayerHand.Cards) == 2 && game.DealerHand.Cards[1].Value == "A" && !game.Insurance && !game.IsSplit {
			if game.checkDealerBlackjackOnAction() {
				game.endGameText(s, m.ChannelID)
				return
			}
		}

		if game.IsSplit {
			if game.ActiveHand == 0 {
				game.ActiveHand = 1
				game.resetTimer(s)
				embed := game.createGameEmbed(false)
				embed.Footer.Text = fmt.Sprintf("Use: !bj hit | !bj stand | !bj double (Playing Hand %d - %s)", game.ActiveHand+1, m.Author.Username)
				components := game.createActionButtons()

				if game.MessageID != "" {
					_, _ = s.ChannelMessageEditComplex(&discordgo.MessageEdit{
						ID:         game.MessageID,
						Channel:    game.ChannelID,
						Embeds:     &[]*discordgo.MessageEmbed{embed},
						Components: &components,
					})
				} else {
					_, _ = s.ChannelMessageSendEmbed(m.ChannelID, embed)
				}
				return
			} else {
				if game.PlayerHand.Score > 21 && game.SplitHand.Score > 21 {
					game.Status = "split_ended"
				} else {
					game.playDealer()
				}
				game.endGameText(s, m.ChannelID)
				return
			}
		}

		game.playDealer()
		game.endGameText(s, m.ChannelID)

	case "double":
		if game.IsSplit {
			var activeHand *Hand
			var activeDoubled *bool
			betAmount := game.Bet
			if game.ActiveHand == 0 {
				activeHand = &game.PlayerHand
				activeDoubled = &game.DoubledDown
			} else {
				activeHand = &game.SplitHand
				activeDoubled = &game.SplitDoubledDown
				betAmount = game.SplitBet
			}

			if len(activeHand.Cards) != 2 || *activeDoubled {
				s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Cannot double down on this hand."))
				return
			}

			balance := database.GetBalance(userID)
			if balance < betAmount {
				s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Insufficient balance to double down!"))
				return
			}

			if err := database.CollectLostBet(userID, betAmount); err != nil {
				s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Failed to deduct double down bet."))
				return
			}

			if game.ActiveHand == 0 {
				game.Bet *= 2
			} else {
				game.SplitBet *= 2
			}
			*activeDoubled = true

			card := game.dealCard()
			activeHand.Cards = append(activeHand.Cards, card)
			calculateScore(activeHand)

			if game.ActiveHand == 0 {
				game.ActiveHand = 1
				game.resetTimer(s)
				embed := game.createGameEmbed(false)
				embed.Footer.Text = fmt.Sprintf("Use: !bj hit | !bj stand | !bj double (Playing Hand %d - %s)", game.ActiveHand+1, m.Author.Username)
				components := game.createActionButtons()

				if game.MessageID != "" {
					_, _ = s.ChannelMessageEditComplex(&discordgo.MessageEdit{
						ID:         game.MessageID,
						Channel:    game.ChannelID,
						Embeds:     &[]*discordgo.MessageEmbed{embed},
						Components: &components,
					})
				} else {
					_, _ = s.ChannelMessageSendEmbed(m.ChannelID, embed)
				}
				return
			} else {
				game.stopTimer()
				if game.PlayerHand.Score > 21 && game.SplitHand.Score > 21 {
					game.Status = "split_ended"
				} else {
					game.playDealer()
				}
				game.endGameText(s, m.ChannelID)
				return
			}
		}

		if len(game.PlayerHand.Cards) != 2 || game.DoubledDown {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Cannot double down now."))
			return
		}

		balance := database.GetBalance(userID)
		if balance < game.Bet {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Insufficient balance to double down!"))
			return
		}

		// Check dealer BJ BEFORE deducting double down bet (protecting player from losing 2x)
		if game.DealerHand.Cards[1].Value == "A" && !game.Insurance {
			if game.checkDealerBlackjackOnAction() {
				game.stopTimer()
				game.endGameText(s, m.ChannelID)
				return
			}
		}

		if err := database.CollectLostBet(userID, game.Bet); err != nil {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Failed to deduct double down bet."))
			return
		}

		game.Bet *= 2
		game.DoubledDown = true
		game.stopTimer()

		card := game.dealCard()
		game.PlayerHand.Cards = append(game.PlayerHand.Cards, card)
		calculateScore(&game.PlayerHand)

		if game.PlayerHand.Score > 21 {
			game.Status = "player_bust"
			game.endGameText(s, m.ChannelID)
			return
		}

		game.playDealer()
		game.endGameText(s, m.ChannelID)

	case "split", "dividir":
		if game.IsSplit || len(game.PlayerHand.Cards) != 2 || game.DoubledDown || game.Insurance {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Cannot split now."))
			return
		}

		c0 := game.PlayerHand.Cards[0]
		c1 := game.PlayerHand.Cards[1]
		if c0.Value != c1.Value && c0.Score != c1.Score {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("You can only split cards of the same rank or value!"))
			return
		}

		balance := database.GetBalance(userID)
		if balance < game.Bet {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Insufficient balance to split!"))
			return
		}

		if game.DealerHand.Cards[1].Value == "A" && !game.Insurance {
			if game.checkDealerBlackjackOnAction() {
				game.stopTimer()
				game.endGameText(s, m.ChannelID)
				return
			}
		}

		if err := database.CollectLostBet(userID, game.Bet); err != nil {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Failed to deduct split bet."))
			return
		}

		game.IsSplit = true
		game.ActiveHand = 0
		game.SplitBet = game.Bet

		game.PlayerHand.Cards = []Card{c0, game.dealCard()}
		game.SplitHand.Cards = []Card{c1, game.dealCard()}
		calculateScore(&game.PlayerHand)
		calculateScore(&game.SplitHand)

		if game.PlayerHand.Score == 21 {
			game.ActiveHand = 1
			if game.SplitHand.Score == 21 {
				game.stopTimer()
				game.playDealer()
				game.endGameText(s, m.ChannelID)
				return
			}
		}

		game.resetTimer(s)

		embed := game.createGameEmbed(false)
		embed.Footer.Text = fmt.Sprintf("Use: !bj hit | !bj stand | !bj double (Playing Hand %d - %s)", game.ActiveHand+1, m.Author.Username)
		components := game.createActionButtons()

		if game.MessageID != "" {
			_, _ = s.ChannelMessageEditComplex(&discordgo.MessageEdit{
				ID:         game.MessageID,
				Channel:    game.ChannelID,
				Embeds:     &[]*discordgo.MessageEmbed{embed},
				Components: &components,
			})
		} else {
			_, _ = s.ChannelMessageSendEmbed(m.ChannelID, embed)
		}

	case "insurance":
		if game.Insurance || game.DealerHand.Cards[1].Value != "A" {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Insurance is not available."))
			return
		}

		insuranceAmount := game.Bet / 2
		balance := database.GetBalance(userID)
		if balance < insuranceAmount {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Insufficient balance for insurance!"))
			return
		}

		if err := database.CollectLostBet(userID, insuranceAmount); err != nil {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Failed to process insurance bet."))
			return
		}

		game.Insurance = true
		game.InsuranceBet = insuranceAmount
		game.resetTimer(s)

		if isBlackjack(game.DealerHand) {
			if isBlackjack(game.PlayerHand) {
				game.Status = "push"
			} else {
				game.Status = "dealer_win"
			}
			game.stopTimer()
			game.endGameText(s, m.ChannelID)
			return
		}

		embed := game.createGameEmbed(false)
		embed.Footer.Text = fmt.Sprintf("🛡️ Insurance purchased (-%d %s). Dealer does NOT have Blackjack!", insuranceAmount, config.Bot.CurrencySymbol)
		components := game.createActionButtons()

		if game.MessageID != "" {
			_, _ = s.ChannelMessageEditComplex(&discordgo.MessageEdit{
				ID:         game.MessageID,
				Channel:    game.ChannelID,
				Embeds:     &[]*discordgo.MessageEmbed{embed},
				Components: &components,
			})
		} else {
			_, _ = s.ChannelMessageSendEmbed(m.ChannelID, embed)
		}

	case "surrender":
		if len(game.PlayerHand.Cards) != 2 || game.DoubledDown || game.Insurance {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Surrender is only allowed on your opening two cards."))
			return
		}

		game.Status = "surrender"
		game.stopTimer()
		game.endGameText(s, m.ChannelID)
	}
}

// Dealer plays according to rules (hits on 16 or less, stands on 17+)
func (g *BlackjackGame) playDealer() {
	for g.DealerHand.Score < 17 {
		card := g.dealCard()
		g.DealerHand.Cards = append(g.DealerHand.Cards, card)
		calculateScore(&g.DealerHand)
	}

	if g.IsSplit {
		g.Status = "split_ended"
		return
	}

	// Determine winner
	if g.DealerHand.Score > 21 {
		g.Status = "dealer_bust"
	} else if g.DealerHand.Score > g.PlayerHand.Score {
		g.Status = "dealer_win"
	} else if g.DealerHand.Score < g.PlayerHand.Score {
		g.Status = "player_win"
	} else {
		g.Status = "push"
	}
}

// buildSplitGameOverEmbed handles payouts and creates embed for split games
func (g *BlackjackGame) buildSplitGameOverEmbed() *discordgo.MessageEmbed {
	winnings := 0
	totalSpent := g.Bet + g.SplitBet

	evalHand := func(hand Hand, bet int) (string, int) {
		if hand.Score > 21 {
			return "💥 **BUST - YOU LOSE**", 0
		}
		if g.DealerHand.Score > 21 {
			return "💥 **DEALER BUST - YOU WIN!**", bet * 2
		}
		if hand.Score > g.DealerHand.Score {
			return "✅ **YOU WIN!**", bet * 2
		}
		if hand.Score < g.DealerHand.Score {
			return "❌ **DEALER WINS**", 0
		}
		return "🤝 **PUSH - TIE**", bet
	}

	h1Result, h1Win := evalHand(g.PlayerHand, g.Bet)
	h2Result, h2Win := evalHand(g.SplitHand, g.SplitBet)
	winnings = h1Win + h2Win

	if winnings > 0 {
		_ = database.AddCoins(g.UserID, winnings)
	}

	netProfit := winnings - totalSpent
	resultColor := 0x888888
	profitText := ""
	if netProfit > 0 {
		resultColor = 0x00FF00
		profitText = fmt.Sprintf("💰 **Net Profit:** +%d %s", netProfit, config.Bot.CurrencySymbol)
	} else if netProfit < 0 {
		resultColor = 0xFF0000
		profitText = fmt.Sprintf("💸 **Net Loss:** %d %s", netProfit, config.Bot.CurrencySymbol)
	} else {
		resultColor = 0xFFA500
		profitText = fmt.Sprintf("⚖️ **Net Result:** Even (0 %s)", config.Bot.CurrencySymbol)
	}

	newBalance := database.GetBalance(g.UserID)

	h1Title := "🎴 Hand 1"
	if g.DoubledDown {
		h1Title += " *(Doubled)*"
	}
	h2Title := "🎴 Hand 2"
	if g.SplitDoubledDown {
		h2Title += " *(Doubled)*"
	}

	dealerScore := fmt.Sprintf("%d", g.DealerHand.Score)
	if g.DealerHand.Score > 21 {
		dealerScore = fmt.Sprintf("%d (Bust)", g.DealerHand.Score)
	}

	return &discordgo.MessageEmbed{
		Title:       "🃏 Blackjack - Game Over (Split)",
		Description: profitText,
		Color:       resultColor,
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   "🎰 Dealer's Hand",
				Value:  fmt.Sprintf("%s\nScore: **%s**", formatHand(g.DealerHand, false), dealerScore),
				Inline: false,
			},
			{
				Name:   h1Title,
				Value:  fmt.Sprintf("%s\nScore: **%d**\nResult: %s", formatHand(g.PlayerHand, false), g.PlayerHand.Score, h1Result),
				Inline: true,
			},
			{
				Name:   h2Title,
				Value:  fmt.Sprintf("%s\nScore: **%d**\nResult: %s", formatHand(g.SplitHand, false), g.SplitHand.Score, h2Result),
				Inline: true,
			},
			{
				Name:   "💵 New Balance",
				Value:  fmt.Sprintf("**%d %s**", newBalance, config.Bot.CurrencySymbol),
				Inline: false,
			},
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: "Game ended (Split)",
		},
		Timestamp: time.Now().Format(time.RFC3339),
	}
}

// buildGameOverEmbed handles payouts and creates the final game over embed
func (g *BlackjackGame) buildGameOverEmbed() *discordgo.MessageEmbed {
	if g.IsSplit {
		return g.buildSplitGameOverEmbed()
	}
	var resultText string
	var resultColor int
	winnings := 0

	switch g.Status {
	case "blackjack":
		winnings = int(float64(g.Bet) * 2.5) // Blackjack pays 3:2
		resultText = "🎉 **BLACKJACK!**"
		resultColor = 0xFFD700

	case "player_win":
		winnings = g.Bet * 2
		resultText = "✅ **YOU WIN!**"
		resultColor = 0x00FF00

	case "dealer_bust":
		winnings = g.Bet * 2
		resultText = "💥 **DEALER BUST - YOU WIN!**"
		resultColor = 0x00FF00

	case "dealer_win":
		resultText = "❌ **DEALER WINS**"
		resultColor = 0xFF0000

	case "player_bust":
		resultText = "💥 **BUST - YOU LOSE**"
		resultColor = 0xFF0000

	case "push":
		winnings = g.Bet
		resultText = "🤝 **PUSH - TIE**"
		resultColor = 0xFFA500

	case "surrender":
		winnings = g.Bet / 2 // Surrender refunds 50%
		resultText = "🏳️ **SURRENDER - 50% REFUNDED**"
		resultColor = 0x888888
	}

	// Handle insurance payout
	insuranceText := ""
	if g.Insurance {
		if isBlackjack(g.DealerHand) {
			insurancePayout := g.InsuranceBet * 3 // Insurance pays 2:1 (stake + 2x profit)
			winnings += insurancePayout
			insuranceText = fmt.Sprintf("\n🛡️ **Insurance paid:** +%d %s", insurancePayout, config.Bot.CurrencySymbol)
		} else {
			insuranceText = fmt.Sprintf("\n🛡️ **Insurance lost:** -%d %s", g.InsuranceBet, config.Bot.CurrencySymbol)
		}
	}

	// Credit user winnings
	if winnings > 0 {
		_ = database.AddCoins(g.UserID, winnings)
	}

	totalSpent := g.Bet
	if g.Insurance {
		totalSpent += g.InsuranceBet
	}

	netProfit := winnings - totalSpent
	profitText := ""
	if netProfit > 0 {
		profitText = fmt.Sprintf("\n💰 **Net Profit:** +%d %s", netProfit, config.Bot.CurrencySymbol)
	} else if netProfit < 0 {
		profitText = fmt.Sprintf("\n💸 **Net Loss:** %d %s", netProfit, config.Bot.CurrencySymbol)
	} else {
		profitText = fmt.Sprintf("\n⚖️ **Net Result:** Even (0 %s)", config.Bot.CurrencySymbol)
	}

	newBalance := database.GetBalance(g.UserID)

	return &discordgo.MessageEmbed{
		Title:       "🃏 Blackjack - Game Over",
		Description: resultText + insuranceText + profitText,
		Color:       resultColor,
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   "🎰 Dealer's Hand",
				Value:  fmt.Sprintf("%s\nScore: **%d**", formatHand(g.DealerHand, false), g.DealerHand.Score),
				Inline: false,
			},
			{
				Name:   "🎴 Your Hand",
				Value:  fmt.Sprintf("%s\nScore: **%d**", formatHand(g.PlayerHand, false), g.PlayerHand.Score),
				Inline: false,
			},
			{
				Name:   "💵 New Balance",
				Value:  fmt.Sprintf("**%d %s**", newBalance, config.Bot.CurrencySymbol),
				Inline: true,
			},
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: "Game ended",
		},
		Timestamp: time.Now().Format(time.RFC3339),
	}
}

// endGameInteraction ends the game for button interactions
func (g *BlackjackGame) endGameInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	embed := g.buildGameOverEmbed()

	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: []discordgo.MessageComponent{}, // Remove buttons
		},
	})

	blackjackMu.Lock()
	delete(activeBlackjackGames, g.UserID)
	blackjackMu.Unlock()
}

// endGameText ends the game for text commands
func (g *BlackjackGame) endGameText(s *discordgo.Session, channelID string) {
	embed := g.buildGameOverEmbed()

	if g.MessageID != "" {
		_, _ = s.ChannelMessageEditComplex(&discordgo.MessageEdit{
			ID:         g.MessageID,
			Channel:    channelID,
			Embeds:     &[]*discordgo.MessageEmbed{embed},
			Components: &[]discordgo.MessageComponent{},
		})
	} else {
		_, _ = s.ChannelMessageSendEmbed(channelID, embed)
	}

	blackjackMu.Lock()
	delete(activeBlackjackGames, g.UserID)
	blackjackMu.Unlock()
}

func respondEmbed(s *discordgo.Session, i *discordgo.InteractionCreate, embed *discordgo.MessageEmbed) {
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds: []*discordgo.MessageEmbed{embed},
			Flags:  discordgo.MessageFlagsEphemeral,
		},
	})
}
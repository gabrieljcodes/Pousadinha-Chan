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
	UserID      string
	Bet         int
	PlayerHand  Hand
	DealerHand  Hand
	Deck        []Card
	Status      string // "playing", "player_bust", "dealer_bust", "player_win", "dealer_win", "push", "blackjack"
	MessageID   string
	ChannelID   string
	Insurance   bool
	InsuranceBet int
	DoubledDown bool
	mu          sync.Mutex
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

// CreateDeck creates a shuffled deck of cards
func createDeck() []Card {
	deck := []Card{}
	
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
	
	// Shuffle deck
	rand.Seed(time.Now().UnixNano())
	rand.Shuffle(len(deck), func(i, j int) {
		deck[i], deck[j] = deck[j], deck[i]
	})
	
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

// Deal a card from the deck
func (g *BlackjackGame) dealCard() Card {
	card := g.Deck[0]
	g.Deck = g.Deck[1:]
	return card
}

// Calculate hand score considering aces
func calculateScore(hand *Hand) {
	hand.Score = 0
	hand.Aces = 0
	
	for _, card := range hand.Cards {
		hand.Score += card.Score
		if card.Value == "A" {
			hand.Aces++
		}
	}
	
	// Adjust for aces
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

// Start Blackjack Game
func StartBlackjackGame(s *discordgo.Session, i *discordgo.InteractionCreate, bet int) {
	userID := i.Member.User.ID
	
	// Check if user already has an active game
	blackjackMu.Lock()
	if _, exists := activeBlackjackGames[userID]; exists {
		blackjackMu.Unlock()
		respondEmbed(s, i, utils.ErrorEmbed("You already have an active Blackjack game!"))
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
	
	// Deduct bet
	database.AddCoins(userID, -bet)
	
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
	
	// Check for immediate blackjack
	playerBJ := isBlackjack(game.PlayerHand)
	dealerBJ := isBlackjack(game.DealerHand)
	
	if playerBJ && dealerBJ {
		game.Status = "push"
		game.endGame(s, i)
		return
	} else if playerBJ {
		game.Status = "blackjack"
		game.endGame(s, i)
		return
	} else if dealerBJ {
		game.Status = "dealer_win"
		game.endGame(s, i)
		return
	}
	
	// Store game
	blackjackMu.Lock()
	activeBlackjackGames[userID] = game
	blackjackMu.Unlock()
	
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
		// Cleanup on error
		blackjackMu.Lock()
		delete(activeBlackjackGames, userID)
		blackjackMu.Unlock()
		database.AddCoins(userID, bet) // Refund
	}
}

// Create game embed
func (g *BlackjackGame) createGameEmbed(showDealer bool) *discordgo.MessageEmbed {
	dealerScore := "?"
	if showDealer {
		dealerScore = fmt.Sprintf("%d", g.DealerHand.Score)
	}
	
	embed := &discordgo.MessageEmbed{
		Title: "🃏 Blackjack",
		Color: 0x2F3136,
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   "💰 Bet",
				Value:  fmt.Sprintf("%d %s", g.Bet, config.Bot.CurrencySymbol),
				Inline: true,
			},
			{
				Name:   "🎰 Dealer's Hand",
				Value:  fmt.Sprintf("%s\nScore: %s", formatHand(g.DealerHand, !showDealer), dealerScore),
				Inline: false,
			},
			{
				Name:   "🎴 Your Hand",
				Value:  fmt.Sprintf("%s\nScore: %d", formatHand(g.PlayerHand, false), g.PlayerHand.Score),
				Inline: false,
			},
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: "Choose your action",
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
	
	// Offer insurance if dealer shows an Ace
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
	
	return []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: buttons,
		},
	}
}

// Handle Hit action
func HandleBlackjackHit(s *discordgo.Session, i *discordgo.InteractionCreate, userID string) {
	// Verify the user clicking is the one who started the game
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
	
	// Deal card to player
	card := game.dealCard()
	game.PlayerHand.Cards = append(game.PlayerHand.Cards, card)
	calculateScore(&game.PlayerHand)
	
	// Check for bust
	if game.PlayerHand.Score > 21 {
		game.Status = "player_bust"
		game.endGame(s, i)
		return
	}
	
	// Update game state
	embed := game.createGameEmbed(false)
	components := game.createActionButtons()
	
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
		},
	})
}

// Handle Stand action
func HandleBlackjackStand(s *discordgo.Session, i *discordgo.InteractionCreate, userID string) {
	// Verify the user clicking is the one who started the game
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
	
	// Dealer plays
	game.playDealer()
	game.endGame(s, i)
}

// Handle Double Down action
func HandleBlackjackDouble(s *discordgo.Session, i *discordgo.InteractionCreate, userID string) {
	// Verify the user clicking is the one who started the game
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
	
	// Deduct additional bet
	balance := database.GetBalance(userID)
	if balance < game.Bet {
		respondEmbed(s, i, utils.ErrorEmbed("Insufficient balance to double down!"))
		return
	}
	
	database.AddCoins(userID, -game.Bet)
	game.Bet *= 2
	game.DoubledDown = true
	
	// Deal one card and stand
	card := game.dealCard()
	game.PlayerHand.Cards = append(game.PlayerHand.Cards, card)
	calculateScore(&game.PlayerHand)
	
	// Check for bust
	if game.PlayerHand.Score > 21 {
		game.Status = "player_bust"
		game.endGame(s, i)
		return
	}
	
	// Dealer plays
	game.playDealer()
	game.endGame(s, i)
}

// Handle Insurance action
func HandleBlackjackInsurance(s *discordgo.Session, i *discordgo.InteractionCreate, userID string) {
	// Verify the user clicking is the one who started the game
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
	
	insuranceAmount := game.Bet / 2
	balance := database.GetBalance(userID)
	
	if balance < insuranceAmount {
		respondEmbed(s, i, utils.ErrorEmbed("Insufficient balance for insurance!"))
		return
	}
	
	database.AddCoins(userID, -insuranceAmount)
	game.Insurance = true
	game.InsuranceBet = insuranceAmount
	
	// Update display
	embed := game.createGameEmbed(false)
	embed.Footer.Text = fmt.Sprintf("Insurance purchased: %d %s", insuranceAmount, config.Bot.CurrencySymbol)
	components := game.createActionButtons()
	
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
		},
	})
}

// Dealer plays according to rules (hits on 16 or less, stands on 17+)
func (g *BlackjackGame) playDealer() {
	for g.DealerHand.Score < 17 {
		card := g.dealCard()
		g.DealerHand.Cards = append(g.DealerHand.Cards, card)
		calculateScore(&g.DealerHand)
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

// End game and distribute winnings
func (g *BlackjackGame) endGame(s *discordgo.Session, i *discordgo.InteractionCreate) {
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
	}
	
	// Handle insurance payout
	insuranceText := ""
	if g.Insurance {
		if isBlackjack(g.DealerHand) {
			insurancePayout := g.InsuranceBet * 3 // Insurance pays 2:1
			winnings += insurancePayout
			insuranceText = fmt.Sprintf("\n🛡️ Insurance paid: +%d %s", insurancePayout, config.Bot.CurrencySymbol)
		} else {
			insuranceText = fmt.Sprintf("\n🛡️ Insurance lost: -%d %s", g.InsuranceBet, config.Bot.CurrencySymbol)
		}
	}
	
	// Add winnings
	if winnings > 0 {
		database.AddCoins(g.UserID, winnings)
	}
	
	profit := winnings - g.Bet
	profitText := ""
	if profit > 0 {
		profitText = fmt.Sprintf("\n💰 Profit: **+%d %s**", profit, config.Bot.CurrencySymbol)
	} else if profit < 0 {
		profitText = fmt.Sprintf("\n💸 Loss: **%d %s**", profit, config.Bot.CurrencySymbol)
	}
	
	newBalance := database.GetBalance(g.UserID)
	
	embed := &discordgo.MessageEmbed{
		Title:       "🃏 Blackjack - Game Over",
		Description: resultText + insuranceText + profitText,
		Color:       resultColor,
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   "🎰 Dealer's Hand",
				Value:  fmt.Sprintf("%s\nScore: %d", formatHand(g.DealerHand, false), g.DealerHand.Score),
				Inline: false,
			},
			{
				Name:   "🎴 Your Hand",
				Value:  fmt.Sprintf("%s\nScore: %d", formatHand(g.PlayerHand, false), g.PlayerHand.Score),
				Inline: false,
			},
			{
				Name:   "💵 Balance",
				Value:  fmt.Sprintf("%d %s", newBalance, config.Bot.CurrencySymbol),
				Inline: true,
			},
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: "Game ended",
		},
		Timestamp: time.Now().Format(time.RFC3339),
	}
	
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: []discordgo.MessageComponent{}, // Remove buttons
		},
	})
	
	// Remove game from active games
	blackjackMu.Lock()
	delete(activeBlackjackGames, g.UserID)
	blackjackMu.Unlock()
}

// StartBlackjackText starts a blackjack game from a text command
func StartBlackjackText(s *discordgo.Session, m *discordgo.MessageCreate, bet int) {
	userID := m.Author.ID
	
	// Check if user already has an active game
	blackjackMu.Lock()
	if _, exists := activeBlackjackGames[userID]; exists {
		blackjackMu.Unlock()
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("You already have an active Blackjack game!"))
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
	
	// Deduct bet
	database.AddCoins(userID, -bet)
	
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
	
	// Check for immediate blackjack
	playerBJ := isBlackjack(game.PlayerHand)
	dealerBJ := isBlackjack(game.DealerHand)
	
	if playerBJ && dealerBJ {
		game.Status = "push"
		game.endGameText(s, m)
		return
	} else if playerBJ {
		game.Status = "blackjack"
		game.endGameText(s, m)
		return
	} else if dealerBJ {
		game.Status = "dealer_win"
		game.endGameText(s, m)
		return
	}
	
	// Store game
	blackjackMu.Lock()
	activeBlackjackGames[userID] = game
	blackjackMu.Unlock()
	
	// Send initial game state
	embed := game.createGameEmbed(false)
	embed.Footer.Text = fmt.Sprintf("Use: !bj hit | !bj stand | !bj double | !bj insurance (User: %s)", m.Author.Username)
	components := game.createActionButtons()
	
	msg, err := s.ChannelMessageSendComplex(m.ChannelID, &discordgo.MessageSend{
		Embeds:     []*discordgo.MessageEmbed{embed},
		Components: components,
	})
	
	if err != nil {
		// Cleanup on error
		blackjackMu.Lock()
		delete(activeBlackjackGames, userID)
		blackjackMu.Unlock()
		database.AddCoins(userID, bet) // Refund
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Failed to start game."))
		return
	}
	
	game.MessageID = msg.ID
}

// endGameText ends the game for text commands
func (g *BlackjackGame) endGameText(s *discordgo.Session, m *discordgo.MessageCreate) {
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
	}
	
	// Handle insurance payout
	insuranceText := ""
	if g.Insurance {
		if isBlackjack(g.DealerHand) {
			insurancePayout := g.InsuranceBet * 3 // Insurance pays 2:1
			winnings += insurancePayout
			insuranceText = fmt.Sprintf("\n🛡️ Insurance paid: +%d %s", insurancePayout, config.Bot.CurrencySymbol)
		} else {
			insuranceText = fmt.Sprintf("\n🛡️ Insurance lost: -%d %s", g.InsuranceBet, config.Bot.CurrencySymbol)
		}
	}
	
	// Add winnings
	if winnings > 0 {
		database.AddCoins(g.UserID, winnings)
	}
	
	profit := winnings - g.Bet
	profitText := ""
	if profit > 0 {
		profitText = fmt.Sprintf("\n💰 Profit: **+%d %s**", profit, config.Bot.CurrencySymbol)
	} else if profit < 0 {
		profitText = fmt.Sprintf("\n💸 Loss: **%d %s**", profit, config.Bot.CurrencySymbol)
	}
	
	newBalance := database.GetBalance(g.UserID)
	
	embed := &discordgo.MessageEmbed{
		Title:       "🃏 Blackjack - Game Over",
		Description: resultText + insuranceText + profitText,
		Color:       resultColor,
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   "🎰 Dealer's Hand",
				Value:  fmt.Sprintf("%s\nScore: %d", formatHand(g.DealerHand, false), g.DealerHand.Score),
				Inline: false,
			},
			{
				Name:   "🎴 Your Hand",
				Value:  fmt.Sprintf("%s\nScore: %d", formatHand(g.PlayerHand, false), g.PlayerHand.Score),
				Inline: false,
			},
			{
				Name:   "💵 Balance",
				Value:  fmt.Sprintf("%d %s", newBalance, config.Bot.CurrencySymbol),
				Inline: true,
			},
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: "Game ended",
		},
	}
	
	s.ChannelMessageSendEmbed(m.ChannelID, embed)
	
	// Remove game from active games
	blackjackMu.Lock()
	delete(activeBlackjackGames, g.UserID)
	blackjackMu.Unlock()
}

func respondEmbed(s *discordgo.Session, i *discordgo.InteractionCreate, embed *discordgo.MessageEmbed) {
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds: []*discordgo.MessageEmbed{embed},
			Flags:  discordgo.MessageFlagsEphemeral,
		},
	})
}
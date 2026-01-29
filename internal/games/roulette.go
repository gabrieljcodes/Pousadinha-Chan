package games

import (
	"estudocoin/internal/database"
	"estudocoin/pkg/config"
	"estudocoin/pkg/utils"
	"fmt"
	"log"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

const (
	MinRouletteBet  = 50
	BetPhaseMinutes = 2 // Time before spin to place bets
)

// Roulette numbers with their colors
// 0 = Green, Red/Black alternating
var rouletteNumbers = []struct {
	Number int
	Color  string // "green", "red", "black"
}{
	{0, "green"},
	{1, "red"}, {2, "black"}, {3, "red"}, {4, "black"}, {5, "red"}, {6, "black"}, {7, "red"}, {8, "black"}, {9, "red"},
	{10, "black"}, {11, "black"}, {12, "red"}, {13, "black"}, {14, "red"}, {15, "black"}, {16, "red"}, {17, "black"}, {18, "red"},
	{19, "red"}, {20, "black"}, {21, "red"}, {22, "black"}, {23, "red"}, {24, "black"}, {25, "red"}, {26, "black"}, {27, "red"},
	{28, "black"}, {29, "black"}, {30, "red"}, {31, "black"}, {32, "red"}, {33, "black"}, {34, "red"}, {35, "black"}, {36, "red"},
}

type BetType string

const (
	BetNumber  BetType = "number"  // Straight up - 35:1
	BetColor   BetType = "color"   // Red/Black - 1:1
	BetEvenOdd BetType = "evenodd" // Even/Odd - 1:1 (0 loses)
	BetHalf    BetType = "half"    // 1-18 / 19-36 - 1:1
	BetDozen   BetType = "dozen"   // 1st 12, 2nd 12, 3rd 12 - 2:1
)

type RouletteBet struct {
	UserID   string
	Username string
	BetType  BetType
	Value    string // "red", "black", "even", "odd", "1-18", "19-36", "1st", "2nd", "3rd", or number
	Amount   int
}

type RouletteRound struct {
	Bets      []RouletteBet
	Result    int
	Color     string
	Spinning  bool
	StartTime time.Time
	EndTime   time.Time
	mu        sync.RWMutex
}

var (
	currentRound    *RouletteRound
	rouletteSession *discordgo.Session
	rouletteTicker  *time.Ticker
	rouletteStop    chan bool
)

func StartRoulette(s *discordgo.Session) {
	// Check if roulette is enabled
	if !config.Economy.RouletteEnabled {
		log.Println("Roulette is disabled in configuration")
		return
	}

	// Check if channel is configured
	if config.Bot.RouletteChannelID == "" {
		log.Println("Roulette channel ID not configured. Set 'roulette_channel_id' in config.json")
		return
	}

	rouletteSession = s
	rouletteStop = make(chan bool)

	interval := config.Economy.RouletteIntervalMinutes
	if interval <= 0 {
		interval = 10
	}

	log.Printf("Starting Roulette with %d minute intervals in channel %s", interval, config.Bot.RouletteChannelID)

	// Start first round immediately
	startNewRound()

	// Schedule next rounds
	rouletteTicker = time.NewTicker(time.Duration(interval) * time.Minute)
	go func() {
		for {
			select {
			case <-rouletteTicker.C:
				log.Println("Roulette ticker triggered - spinning wheel")
				spinRoulette()
				startNewRound()
			case <-rouletteStop:
				log.Println("Roulette stopped")
				return
			}
		}
	}()
}

func StopRoulette() {
	if rouletteTicker != nil {
		rouletteTicker.Stop()
		close(rouletteStop)
		rouletteTicker = nil
	}
}

func startNewRound() {
	interval := config.Economy.RouletteIntervalMinutes
	if interval <= 0 {
		interval = 10
	}

	now := time.Now()
	round := &RouletteRound{
		Bets:      make([]RouletteBet, 0),
		Spinning:  false,
		StartTime: now,
		EndTime:   now.Add(time.Duration(interval) * time.Minute),
	}

	currentRound = round

	log.Printf("Starting new roulette round. Next spin at %s", round.EndTime.Format("15:04:05"))

	// Post betting open message
	postBettingOpenEmbed(round)
}

func spinRoulette() {
	if currentRound == nil {
		log.Println("No active roulette round to spin")
		return
	}

	currentRound.mu.Lock()
	if currentRound.Spinning {
		currentRound.mu.Unlock()
		log.Println("Roulette already spinning")
		return
	}
	currentRound.Spinning = true
	currentRound.mu.Unlock()

	// Generate result
	rand.Seed(time.Now().UnixNano())
	result := rand.Intn(37) // 0-36
	currentRound.Result = result
	currentRound.Color = rouletteNumbers[result].Color

	log.Printf("Roulette result: %d (%s)", result, currentRound.Color)

	// Process payouts
	payouts := processPayouts(currentRound)

	// Post result
	postResultEmbed(currentRound, payouts)
}

func processPayouts(round *RouletteRound) map[string]int {
	payouts := make(map[string]int)
	resultNum := round.Result
	resultColor := round.Color

	round.mu.RLock()
	defer round.mu.RUnlock()

	for _, bet := range round.Bets {
		won := false
		multiplier := 0

		switch bet.BetType {
		case BetNumber:
			betNum := 0
			fmt.Sscanf(bet.Value, "%d", &betNum)
			if betNum == resultNum {
				won = true
				multiplier = 35
			}
		case BetColor:
			if bet.Value == resultColor {
				won = true
				multiplier = 1
			}
		case BetEvenOdd:
			if resultNum == 0 {
				// 0 loses on even/odd bets
				won = false
			} else if bet.Value == "even" && resultNum%2 == 0 {
				won = true
				multiplier = 1
			} else if bet.Value == "odd" && resultNum%2 == 1 {
				won = true
				multiplier = 1
			}
		case BetHalf:
			if bet.Value == "1-18" && resultNum >= 1 && resultNum <= 18 {
				won = true
				multiplier = 1
			} else if bet.Value == "19-36" && resultNum >= 19 && resultNum <= 36 {
				won = true
				multiplier = 1
			}
		case BetDozen:
			if bet.Value == "1st" && resultNum >= 1 && resultNum <= 12 {
				won = true
				multiplier = 2
			} else if bet.Value == "2nd" && resultNum >= 13 && resultNum <= 24 {
				won = true
				multiplier = 2
			} else if bet.Value == "3rd" && resultNum >= 25 && resultNum <= 36 {
				won = true
				multiplier = 2
			}
		}

		if won {
			winnings := bet.Amount + (bet.Amount * multiplier)
			database.AddCoins(bet.UserID, winnings)
			payouts[bet.UserID] += winnings - bet.Amount // Track net profit
		}
	}

	return payouts
}

func PlaceRouletteBet(userID, username string, betType BetType, value string, amount int) (bool, string) {
	if currentRound == nil {
		return false, "No active roulette round."
	}

	currentRound.mu.RLock()
	if currentRound.Spinning {
		currentRound.mu.RUnlock()
		return false, "Too late! The wheel is already spinning."
	}
	currentRound.mu.RUnlock()

	if amount < MinRouletteBet {
		return false, fmt.Sprintf("Minimum bet is %d %s", MinRouletteBet, config.Bot.CurrencySymbol)
	}

	balance := database.GetBalance(userID)
	if balance < amount {
		return false, fmt.Sprintf("Insufficient balance! You have %d %s", balance, config.Bot.CurrencySymbol)
	}

	// Validate bet
	if !isValidBet(betType, value) {
		return false, "Invalid bet."
	}

	// Deduct bet
	if err := database.RemoveCoins(userID, amount); err != nil {
		return false, "Error placing bet."
	}

	// Add to round
	currentRound.mu.Lock()
	currentRound.Bets = append(currentRound.Bets, RouletteBet{
		UserID:   userID,
		Username: username,
		BetType:  betType,
		Value:    value,
		Amount:   amount,
	})
	currentRound.mu.Unlock()

	return true, ""
}

func isValidBet(betType BetType, value string) bool {
	switch betType {
	case BetNumber:
		var num int
		_, err := fmt.Sscanf(value, "%d", &num)
		return err == nil && num >= 0 && num <= 36
	case BetColor:
		return value == "red" || value == "black"
	case BetEvenOdd:
		return value == "even" || value == "odd"
	case BetHalf:
		return value == "1-18" || value == "19-36"
	case BetDozen:
		return value == "1st" || value == "2nd" || value == "3rd"
	}
	return false
}

func postBettingOpenEmbed(round *RouletteRound) {
	channelID := config.Bot.RouletteChannelID
	if channelID == "" {
		log.Println("No roulette channel configured, skipping betting open message")
		return
	}

	if rouletteSession == nil {
		log.Println("Roulette session not initialized")
		return
	}

	timeUntilSpin := round.EndTime.Sub(time.Now())
	minutes := int(timeUntilSpin.Minutes())
	seconds := int(timeUntilSpin.Seconds()) % 60

	embed := &discordgo.MessageEmbed{
		Title:       "🎰 ROULETTE - Betting Open!",
		Description: fmt.Sprintf("Place your bets! The wheel spins in **%d minutes and %d seconds**.", minutes, seconds),
		Color:       0x00FF00,
		Fields: []*discordgo.MessageEmbedField{
			{
				Name: "📋 Available Bets",
				Value: "• `!wheel number <0-36> <amount>` - **35:1**\n" +
					"• `!wheel red <amount>` or `!wheel black <amount>` - **1:1**\n" +
					"• `!wheel even <amount>` or `!wheel odd <amount>` - **1:1**\n" +
					"• `!wheel low <amount>` (1-18) or `!wheel high <amount>` (19-36) - **1:1**\n" +
					"• `!wheel dozen <1st/2nd/3rd> <amount>` - **2:1**",
				Inline: false,
			},
			{
				Name:   "💰 Minimum Bet",
				Value:  fmt.Sprintf("%d %s", MinRouletteBet, config.Bot.CurrencySymbol),
				Inline: true,
			},
			{
				Name:   "⏰ Next Spin",
				Value:  fmt.Sprintf("<t:%d:R>", round.EndTime.Unix()),
				Inline: true,
			},
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: "🍀 Good luck!",
		},
	}

	_, err := rouletteSession.ChannelMessageSendEmbed(channelID, embed)
	if err != nil {
		log.Printf("Error sending roulette betting open message: %v", err)
	} else {
		log.Printf("Sent roulette betting open message to channel %s", channelID)
	}
}

func postResultEmbed(round *RouletteRound, payouts map[string]int) {
	channelID := config.Bot.RouletteChannelID
	if channelID == "" {
		log.Println("No roulette channel configured, skipping result message")
		return
	}

	if rouletteSession == nil {
		log.Println("Roulette session not initialized")
		return
	}

	resultNum := round.Result
	resultColor := round.Color

	// Get emoji for number
	emoji := "🟢"
	if resultColor == "red" {
		emoji = "🔴"
	} else if resultColor == "black" {
		emoji = "⚫"
	}

	// Build winners list
	winnersList := "No winners this round."
	if len(payouts) > 0 {
		var sb strings.Builder
		for userID, profit := range payouts {
			sb.WriteString(fmt.Sprintf("<@%s>: +%d %s\n", userID, profit, config.Bot.CurrencySymbol))
		}
		winnersList = sb.String()
	}

	// Get total bets
	round.mu.RLock()
	totalBets := len(round.Bets)
	totalAmount := 0
	for _, bet := range round.Bets {
		totalAmount += bet.Amount
	}
	round.mu.RUnlock()

	color := 0xFFD700
	if resultColor == "red" {
		color = 0xFF0000
	} else if resultColor == "black" {
		color = 0x000000
	} else {
		color = 0x00FF00
	}

	embed := &discordgo.MessageEmbed{
		Title:       "🎰 ROULETTE - Result!",
		Description: fmt.Sprintf("# %s **%d**\n\nThe ball landed on **%s %d**!", emoji, resultNum, strings.ToUpper(resultColor), resultNum),
		Color:       color,
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   "🏆 Winners",
				Value:  winnersList,
				Inline: false,
			},
			{
				Name:   "📊 Round Stats",
				Value:  fmt.Sprintf("Total Bets: %d\nTotal Wagered: %d %s", totalBets, totalAmount, config.Bot.CurrencySymbol),
				Inline: false,
			},
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: "Next round starting soon...",
		},
		Timestamp: time.Now().Format(time.RFC3339),
	}

	_, err := rouletteSession.ChannelMessageSendEmbed(channelID, embed)
	if err != nil {
		log.Printf("Error sending roulette result message: %v", err)
	} else {
		log.Printf("Sent roulette result message to channel %s", channelID)
	}
}

func GetCurrentRoundInfo() (time.Time, bool) {
	if currentRound == nil {
		return time.Time{}, false
	}
	currentRound.mu.RLock()
	defer currentRound.mu.RUnlock()
	return currentRound.EndTime, !currentRound.Spinning
}

func CmdRoulette(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	if len(args) < 2 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.InfoEmbed("Roulette",
			"Usage:\n"+
				"`!wheel number <0-36> <amount>` - Bet on a specific number (35:1)\n"+
				"`!wheel red <amount>` - Bet on red (1:1)\n"+
				"`!wheel black <amount>` - Bet on black (1:1)\n"+
				"`!wheel even <amount>` - Bet on even (1:1)\n"+
				"`!wheel odd <amount>` - Bet on odd (1:1)\n"+
				"`!wheel low <amount>` - Bet on 1-18 (1:1)\n"+
				"`!wheel high <amount>` - Bet on 19-36 (1:1)\n"+
				"`!wheel dozen <1st/2nd/3rd> <amount>` - Bet on dozen (2:1)\n\n"+
				"Use `!wheel time` to see when the next spin is."))
		return
	}

	// Check if there's time left
	endTime, active := GetCurrentRoundInfo()
	if !active {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Betting is closed! The wheel is spinning."))
		return
	}

	timeLeft := endTime.Sub(time.Now())
	if timeLeft <= 0 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Too late! Betting is closed for this round."))
		return
	}

	betTypeStr := strings.ToLower(args[0])

	// Special case: time command
	if betTypeStr == "time" {
		minutes := int(timeLeft.Minutes())
		seconds := int(timeLeft.Seconds()) % 60
		s.ChannelMessageSendEmbed(m.ChannelID, utils.InfoEmbed("Roulette",
			fmt.Sprintf("Next spin in **%d minutes and %d seconds**.", minutes, seconds)))
		return
	}

	var betType BetType
	var value string
	var amount int
	var err error

	switch betTypeStr {
	case "number":
		if len(args) < 3 {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Usage: `!roulette number <0-36> <amount>`"))
			return
		}
		betType = BetNumber
		value = args[1]
		amount, err = parseAmount(args[2])
	case "red", "black":
		betType = BetColor
		value = betTypeStr
		amount, err = parseAmount(args[1])
	case "even", "odd":
		betType = BetEvenOdd
		value = betTypeStr
		amount, err = parseAmount(args[1])
	case "low":
		betType = BetHalf
		value = "1-18"
		amount, err = parseAmount(args[1])
	case "high":
		betType = BetHalf
		value = "19-36"
		amount, err = parseAmount(args[1])
	case "dozen":
		if len(args) < 3 {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Usage: `!roulette dozen <1st/2nd/3rd> <amount>`"))
			return
		}
		betType = BetDozen
		value = args[1]
		amount, err = parseAmount(args[2])
	default:
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Invalid bet type. Use `!roulette` for help."))
		return
	}

	if err != nil || amount <= 0 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Invalid amount."))
		return
	}

	success, msg := PlaceRouletteBet(m.Author.ID, m.Author.Username, betType, value, amount)
	if !success {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed(msg))
		return
	}

	s.ChannelMessageSendEmbed(m.ChannelID, utils.SuccessEmbed("Bet Placed!",
		fmt.Sprintf("You bet **%d %s** on **%s**.", amount, config.Bot.CurrencySymbol, formatBet(betType, value))))
}

func parseAmount(s string) (int, error) {
	var amount int
	_, err := fmt.Sscanf(s, "%d", &amount)
	return amount, err
}

func formatBet(betType BetType, value string) string {
	switch betType {
	case BetNumber:
		return "number " + value
	case BetColor:
		return value
	case BetEvenOdd:
		return value
	case BetHalf:
		if value == "1-18" {
			return "low (1-18)"
		}
		return "high (19-36)"
	case BetDozen:
		return value + " dozen"
	}
	return value
}

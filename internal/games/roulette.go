package games

import (
	"crypto/rand"
	"encoding/binary"
	"bot/internal/database"
	"bot/pkg/config"
	"bot/pkg/utils"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

const (
	MinRouletteBet = 50
)

// European roulette numbers with colors (0 = Green, 18 Red, 18 Black)
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
	BetColor   BetType = "color"   // Red/Black - 1:1 (0 loses)
	BetEvenOdd BetType = "evenodd" // Even/Odd - 1:1 (0 loses)
	BetHalf    BetType = "half"    // 1-18 / 19-36 - 1:1 (0 loses)
	BetDozen   BetType = "dozen"   // 1st 12, 2nd 12, 3rd 12 - 2:1 (0 loses)
)

type RouletteBet struct {
	GuildID  string
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

type RouletteHistoryItem struct {
	Number int
	Color  string
}

type UserRoundStats struct {
	GuildID      string
	TotalWagered int
	TotalPayout  int
	NetProfit    int
}

var (
	currentRound    *RouletteRound
	wheelMu      sync.RWMutex
	rouletteSession *discordgo.Session
	rouletteTicker  *time.Ticker
	rouletteStop    chan bool
	recentHistory   []RouletteHistoryItem
)

// spinWheelCrypto generates a uniform random number between 0 and 36 using crypto/rand
func spinWheelCrypto() int {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return int(binary.LittleEndian.Uint32(b[:]) % 37)
}

func StartRoulette(s *discordgo.Session) {
	if !config.Economy.RouletteEnabled {
		log.Println("Roulette is disabled in configuration")
		return
	}

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

	startNewRound()

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

	// Refund active round bets if the bot stops
	wheelMu.Lock()
	if currentRound != nil {
		currentRound.mu.Lock()
		if !currentRound.Spinning && len(currentRound.Bets) > 0 {
			for _, bet := range currentRound.Bets {
				_ = database.AddCoins(bet.GuildID, bet.UserID, bet.Amount)
			}
			log.Printf("Refunded %d bets on roulette shutdown", len(currentRound.Bets))
			currentRound.Bets = nil
		}
		currentRound.mu.Unlock()
	}
	wheelMu.Unlock()
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

	wheelMu.Lock()
	currentRound = round
	wheelMu.Unlock()

	log.Printf("Starting new roulette round. Next spin at %s", round.EndTime.Format("15:04:05"))

	postBettingOpenEmbed(round)
}

func spinRoulette() {
	wheelMu.Lock()
	round := currentRound
	if round == nil {
		wheelMu.Unlock()
		log.Println("No active roulette round to spin")
		return
	}

	round.mu.Lock()
	if round.Spinning {
		round.mu.Unlock()
		wheelMu.Unlock()
		log.Println("Roulette already spinning")
		return
	}
	round.Spinning = true
	round.mu.Unlock()
	wheelMu.Unlock()

	result := spinWheelCrypto()
	resultColor := rouletteNumbers[result].Color

	round.mu.Lock()
	round.Result = result
	round.Color = resultColor
	round.mu.Unlock()

	// Update recent history
	wheelMu.Lock()
	recentHistory = append([]RouletteHistoryItem{{Number: result, Color: resultColor}}, recentHistory...)
	if len(recentHistory) > 5 {
		recentHistory = recentHistory[:5]
	}
	wheelMu.Unlock()

	log.Printf("Roulette result: %d (%s)", result, resultColor)

	userStats := processPayouts(round)
	postResultEmbed(round, userStats)
}

// calculatePayouts calculates total wagered, total payout, and net profit for each player
func calculatePayouts(round *RouletteRound) map[string]*UserRoundStats {
	stats := make(map[string]*UserRoundStats)
	resultNum := round.Result
	resultColor := round.Color

	round.mu.RLock()
	defer round.mu.RUnlock()

	// Step 1: Accumulate total wagered per player
	for _, bet := range round.Bets {
		if _, exists := stats[bet.UserID]; !exists {
			stats[bet.UserID] = &UserRoundStats{
				GuildID: bet.GuildID,
			}
		}
		stats[bet.UserID].TotalWagered += bet.Amount
	}

	// Step 2: Compute winnings per bet
	for _, bet := range round.Bets {
		won := false
		multiplier := 0

		switch bet.BetType {
		case BetNumber:
			betNum := 0
			_, _ = fmt.Sscanf(bet.Value, "%d", &betNum)
			if betNum == resultNum {
				won = true
				multiplier = 35
			}
		case BetColor:
			if resultNum != 0 && bet.Value == resultColor {
				won = true
				multiplier = 1
			}
		case BetEvenOdd:
			if resultNum != 0 {
				if bet.Value == "even" && resultNum%2 == 0 {
					won = true
					multiplier = 1
				} else if bet.Value == "odd" && resultNum%2 == 1 {
					won = true
					multiplier = 1
				}
			}
		case BetHalf:
			if resultNum != 0 {
				if bet.Value == "1-18" && resultNum >= 1 && resultNum <= 18 {
					won = true
					multiplier = 1
				} else if bet.Value == "19-36" && resultNum >= 19 && resultNum <= 36 {
					won = true
					multiplier = 1
				}
			}
		case BetDozen:
			if resultNum != 0 {
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
		}

		if won {
			winnings := bet.Amount + (bet.Amount * multiplier)
			stats[bet.UserID].TotalPayout += winnings
		}
	}

	// Step 3: Compute exact net profit
	for _, s := range stats {
		s.NetProfit = s.TotalPayout - s.TotalWagered
	}

	return stats
}

func processPayouts(round *RouletteRound) map[string]*UserRoundStats {
	stats := calculatePayouts(round)

	if database.DB != nil {
		for userID, s := range stats {
			if s.TotalPayout > 0 {
				_ = database.AddCoins(s.GuildID, userID, s.TotalPayout)
			}
		}
	}

	return stats
}

func PlaceRouletteBet(guildID, userID, username string, betType BetType, value string, amount int) (bool, string) {
	if guildID == "" {
		return false, "This command can only be used within a server."
	}

	wheelMu.RLock()
	round := currentRound
	wheelMu.RUnlock()

	if round == nil {
		return false, "No active roulette round. The casino wheel may be initializing."
	}

	if amount < MinRouletteBet {
		return false, fmt.Sprintf("Minimum bet is %d %s", MinRouletteBet, config.Bot.CurrencySymbol)
	}

	if !isValidBet(betType, value) {
		return false, "Invalid bet type or value. Use `!wheel` to see valid options."
	}

	balance := database.GetBalance(guildID, userID)
	if balance < amount {
		return false, fmt.Sprintf("Insufficient balance! You have %d %s", balance, config.Bot.CurrencySymbol)
	}

	round.mu.Lock()
	defer round.mu.Unlock()

	if round.Spinning || time.Now().After(round.EndTime) {
		return false, "Too late! The wheel is already spinning for this round."
	}

	if err := database.CollectLostBet(guildID, userID, amount); err != nil {
		return false, "Error deducting bet coins."
	}

	round.Bets = append(round.Bets, RouletteBet{
		GuildID:  guildID,
		UserID:   userID,
		Username: username,
		BetType:  betType,
		Value:    value,
		Amount:   amount,
	})

	return true, ""
}

func isValidBet(betType BetType, value string) bool {
	switch betType {
	case BetNumber:
		var num int
		n, err := fmt.Sscanf(value, "%d", &num)
		return err == nil && n == 1 && num >= 0 && num <= 36
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

func formatRecentHistory() string {
	wheelMu.RLock()
	defer wheelMu.RUnlock()

	if len(recentHistory) == 0 {
		return "None yet"
	}

	var parts []string
	for _, item := range recentHistory {
		emoji := "🟢"
		if item.Color == "red" {
			emoji = "🔴"
		} else if item.Color == "black" {
			emoji = "⚫"
		}
		parts = append(parts, fmt.Sprintf("%s %d", emoji, item.Number))
	}
	return strings.Join(parts, "  |  ")
}

func postBettingOpenEmbed(round *RouletteRound) {
	channelID := config.Bot.RouletteChannelID
	if channelID == "" || rouletteSession == nil {
		return
	}

	embed := &discordgo.MessageEmbed{
		Title:       "🎰 ROULETTE - Betting Open!",
		Description: fmt.Sprintf("Place your bets! The wheel spins <t:%d:R>.", round.EndTime.Unix()),
		Color:       utils.ColorGreen,
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
				Name:   "⏰ Spin Time",
				Value:  fmt.Sprintf("<t:%d:T>", round.EndTime.Unix()),
				Inline: true,
			},
			{
				Name:   "📜 Recent Spins",
				Value:  formatRecentHistory(),
				Inline: false,
			},
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: "🍀 Use !wheel time to check countdown from any channel!",
		},
	}

	_, err := rouletteSession.ChannelMessageSendEmbed(channelID, embed)
	if err != nil {
		log.Printf("Error sending roulette betting open message: %v", err)
	}
}

func postResultEmbed(round *RouletteRound, userStats map[string]*UserRoundStats) {
	channelID := config.Bot.RouletteChannelID
	if channelID == "" || rouletteSession == nil {
		return
	}

	resultNum := round.Result
	resultColor := round.Color

	emoji := "🟢"
	if resultColor == "red" {
		emoji = "🔴"
	} else if resultColor == "black" {
		emoji = "⚫"
	}

	// Build honest winners/participants list
	var winnersSb strings.Builder
	winnerCount := 0

	for userID, stats := range userStats {
		if stats.NetProfit > 0 {
			winnerCount++
			winnersSb.WriteString(fmt.Sprintf("• <@%s>: **+%d %s** *(Payout: %d %s, Bet: %d %s)*\n",
				userID, stats.NetProfit, config.Bot.CurrencySymbol, stats.TotalPayout, config.Bot.CurrencySymbol, stats.TotalWagered, config.Bot.CurrencySymbol))
		}
	}

	winnersList := "No winners this round."
	if winnerCount > 0 {
		winnersList = winnersSb.String()
	}

	round.mu.RLock()
	totalBets := len(round.Bets)
	totalAmount := 0
	for _, bet := range round.Bets {
		totalAmount += bet.Amount
	}
	round.mu.RUnlock()

	color := 0x2ECC71 // Green
	if resultColor == "red" {
		color = 0xE74C3C // Red
	} else if resultColor == "black" {
		color = 0x34495E // Dark
	}

	embed := &discordgo.MessageEmbed{
		Title:       "🎰 ROULETTE - Result!",
		Description: fmt.Sprintf("# %s **%d (%s)**\n\nThe ball landed on **%s %d**!", emoji, resultNum, strings.ToUpper(resultColor), strings.ToUpper(resultColor), resultNum),
		Color:       color,
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   "🏆 Net Winners",
				Value:  winnersList,
				Inline: false,
			},
			{
				Name:   "📊 Round Stats",
				Value:  fmt.Sprintf("Total Bets: %d | Total Wagered: %d %s", totalBets, totalAmount, config.Bot.CurrencySymbol),
				Inline: false,
			},
			{
				Name:   "📜 Recent Spins",
				Value:  formatRecentHistory(),
				Inline: false,
			},
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: "Next round starting shortly...",
		},
		Timestamp: time.Now().Format(time.RFC3339),
	}

	_, err := rouletteSession.ChannelMessageSendEmbed(channelID, embed)
	if err != nil {
		log.Printf("Error sending roulette result message: %v", err)
	}
}

func GetCurrentRoundInfo() (time.Time, bool, int, int) {
	wheelMu.RLock()
	round := currentRound
	wheelMu.RUnlock()

	if round == nil {
		return time.Time{}, false, 0, 0
	}

	round.mu.RLock()
	defer round.mu.RUnlock()

	totalBets := len(round.Bets)
	totalAmount := 0
	for _, bet := range round.Bets {
		totalAmount += bet.Amount
	}

	return round.EndTime, !round.Spinning, totalBets, totalAmount
}

// CmdRoulette processes text commands (!wheel and !roleta-cassino)
func CmdRoulette(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	if m.GuildID == "" {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("This command can only be used within a server."))
		return
	}

	if len(args) == 0 {
		sendRouletteHelp(s, m.ChannelID)
		return
	}

	subCmd := strings.ToLower(args[0])

	// Status / Time check (!wheel time, !wheel status, !wheel tempo)
	if subCmd == "time" || subCmd == "status" || subCmd == "tempo" {
		endTime, active, betsCount, totalAmount := GetCurrentRoundInfo()
		if !active {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.InfoEmbed("Casino Roulette", "The wheel is currently spinning! Please wait for the next round."))
			return
		}

		timeLeft := time.Until(endTime)
		if timeLeft < 0 {
			timeLeft = 0
		}

		embed := &discordgo.MessageEmbed{
			Title: "🎰 Casino Roulette - Status",
			Description: fmt.Sprintf("Next spin <t:%d:R> (<t:%d:T>).\n\n"+
				"**Bets Placed:** %d\n"+
				"**Total Wagered:** %d %s\n\n"+
				"**Recent History:** %s",
				endTime.Unix(), endTime.Unix(), betsCount, totalAmount, config.Bot.CurrencySymbol, formatRecentHistory()),
			Color: utils.ColorGold,
			Footer: &discordgo.MessageEmbedFooter{
				Text: "Place your bets with !wheel <type> <amount>",
			},
		}
		s.ChannelMessageSendEmbed(m.ChannelID, embed)
		return
	}

	if len(args) < 2 {
		sendRouletteHelp(s, m.ChannelID)
		return
	}

	endTime, active, _, _ := GetCurrentRoundInfo()
	if !active || time.Now().After(endTime) {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Betting is currently closed! The wheel is spinning or resolving."))
		return
	}

	var betType BetType
	var value string
	var amount int
	var err error

	switch subCmd {
	case "number", "numero":
		if len(args) < 3 {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Usage: `!wheel number <0-36> <amount>`"))
			return
		}
		betType = BetNumber
		value = args[1]
		amount, err = parseAmount(args[2])
	case "red", "vermelho":
		betType = BetColor
		value = "red"
		amount, err = parseAmount(args[1])
	case "black", "preto":
		betType = BetColor
		value = "black"
		amount, err = parseAmount(args[1])
	case "even", "par":
		betType = BetEvenOdd
		value = "even"
		amount, err = parseAmount(args[1])
	case "odd", "impar", "ímpar":
		betType = BetEvenOdd
		value = "odd"
		amount, err = parseAmount(args[1])
	case "low", "baixo":
		betType = BetHalf
		value = "1-18"
		amount, err = parseAmount(args[1])
	case "high", "alto":
		betType = BetHalf
		value = "19-36"
		amount, err = parseAmount(args[1])
	case "dozen", "duzia", "dúzia":
		if len(args) < 3 {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Usage: `!wheel dozen <1st/2nd/3rd> <amount>`"))
			return
		}
		betType = BetDozen
		value = strings.ToLower(args[1])
		if value == "1" || value == "1a" {
			value = "1st"
		} else if value == "2" || value == "2a" {
			value = "2nd"
		} else if value == "3" || value == "3a" {
			value = "3rd"
		}
		amount, err = parseAmount(args[2])
	default:
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Invalid bet type. Use `!wheel` for available options."))
		return
	}

	if err != nil || amount <= 0 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Invalid amount."))
		return
	}

	success, msg := PlaceRouletteBet(m.GuildID, m.Author.ID, m.Author.Username, betType, value, amount)
	if !success {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed(msg))
		return
	}

	s.ChannelMessageSendEmbed(m.ChannelID, utils.SuccessEmbed("Bet Placed!",
		fmt.Sprintf("You bet **%d %s** on **%s**.\nNext spin: <t:%d:R>", amount, config.Bot.CurrencySymbol, formatBet(betType, value), endTime.Unix())))
}

func sendRouletteHelp(s *discordgo.Session, channelID string) {
	s.ChannelMessageSendEmbed(channelID, utils.InfoEmbed("Casino Roulette (!wheel)",
		"**Available Bets:**\n"+
			"• `!wheel number <0-36> <amount>` - Straight up (**35:1**)\n"+
			"• `!wheel red <amount>` / `!wheel black <amount>` - Colors (**1:1**)\n"+
			"• `!wheel even <amount>` / `!wheel odd <amount>` - Even or Odd (**1:1**)\n"+
			"• `!wheel low <amount>` (1-18) / `!wheel high <amount>` (19-36) - Halves (**1:1**)\n"+
			"• `!wheel dozen <1st|2nd|3rd> <amount>` - Dozens (**2:1**)\n\n"+
			"**Status:** Use `!wheel time` to check countdown and round stats."))
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
		if value == "red" {
			return "🔴 Red"
		}
		return "⚫ Black"
	case BetEvenOdd:
		if value == "even" {
			return "Even (Par)"
		}
		return "Odd (Ímpar)"
	case BetHalf:
		if value == "1-18" {
			return "Low (1-18)"
		}
		return "High (19-36)"
	case BetDozen:
		return value + " dozen"
	}
	return value
}

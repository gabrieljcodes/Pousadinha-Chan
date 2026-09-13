package games

import (
	"bot/internal/database"
	"bot/internal/locale"
	"bot/pkg/config"
	"bot/pkg/utils"
	"crypto/rand"
	"encoding/binary"
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
	wheelMu         sync.RWMutex
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
		log.Println(locale.Text("games.roulette.roulette_is_disabled_in_configuration"))
		return
	}

	if config.Bot.RouletteChannelID == "" {
		log.Println(locale.Text("games.roulette.roulette_channel_id_not_configured_set_roulette"))
		return
	}

	rouletteSession = s
	rouletteStop = make(chan bool)

	interval := config.Economy.RouletteIntervalMinutes
	if interval <= 0 {
		interval = 10
	}

	log.Printf(locale.Text("games.roulette.starting_roulette_with_minute_intervals_in_channel"), interval, config.Bot.RouletteChannelID)

	startNewRound()

	rouletteTicker = time.NewTicker(time.Duration(interval) * time.Minute)
	go func() {
		for {
			select {
			case <-rouletteTicker.C:
				log.Println(locale.Text("games.roulette.roulette_ticker_triggered_spinning_wheel"))
				spinRoulette()
				startNewRound()
			case <-rouletteStop:
				log.Println(locale.Text("games.roulette.roulette_stopped"))
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
			log.Printf(locale.Text("games.roulette.refunded_bets_on_roulette_shutdown"), len(currentRound.Bets))
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

	log.Printf(locale.Text("games.roulette.starting_new_roulette_round_next_spin_at"), round.EndTime.Format("15:04:05"))

	postBettingOpenEmbed(round)
}

func spinRoulette() {
	wheelMu.Lock()
	round := currentRound
	if round == nil {
		wheelMu.Unlock()
		log.Println(locale.Text("games.roulette.no_active_roulette_round_to_spin"))
		return
	}

	round.mu.Lock()
	if round.Spinning {
		round.mu.Unlock()
		wheelMu.Unlock()
		log.Println(locale.Text("games.roulette.roulette_already_spinning"))
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

	log.Printf(locale.Text("games.roulette.roulette_result"), result, resultColor)

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
		return false, locale.Text("games.roulette.this_command_can_only_be_used_within")
	}

	wheelMu.RLock()
	round := currentRound
	wheelMu.RUnlock()

	if round == nil {
		return false, locale.Text("games.roulette.no_active_roulette_round_the_casino_wheel")
	}

	if amount < MinRouletteBet {
		return false, locale.Text("games.cups.minimum_bet_is.formatted2", locale.Data{"MinRouletteBet": MinRouletteBet, "CurrencySymbol": config.Bot.CurrencySymbol})
	}

	if !isValidBet(betType, value) {
		return false, locale.Text("games.roulette.invalid_bet_type_or_value_use_wheel")
	}

	balance := database.GetBalance(guildID, userID)
	if balance < amount {
		return false, locale.Text("games.blackjack.insufficient_balance_you_have.formatted", locale.Data{"Balance": balance, "CurrencySymbol": config.Bot.CurrencySymbol})
	}

	round.mu.Lock()
	defer round.mu.Unlock()

	if round.Spinning || time.Now().After(round.EndTime) {
		return false, locale.Text("games.roulette.too_late_the_wheel_is_already_spinning")
	}

	if err := database.CollectLostBet(guildID, userID, amount); err != nil {
		return false, locale.Text("games.roulette.error_deducting_bet_coins")
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
		return locale.Text("games.roulette.none_yet")
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
		Title:       locale.Text("games.roulette.roulette_betting_open"),
		Description: locale.Text("games.roulette.place_your_bets_the_wheel_spins_t.formatted", locale.Data{"EndTime": round.EndTime.Unix()}),
		Color:       utils.ColorGreen,
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   locale.Text("games.roulette.available_bets"),
				Value:  locale.Text("games.roulette.wheel_bet_type_number_number_amount_coins"),
				Inline: false,
			},
			{
				Name:   locale.Text("games.roulette.minimum_bet"),
				Value:  fmt.Sprintf("%d %s", MinRouletteBet, config.Bot.CurrencySymbol),
				Inline: true,
			},
			{
				Name:   locale.Text("games.roulette.spin_time"),
				Value:  fmt.Sprintf("<t:%d:T>", round.EndTime.Unix()),
				Inline: true,
			},
			{
				Name:   locale.Text("games.roulette.recent_spins"),
				Value:  formatRecentHistory(),
				Inline: false,
			},
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: locale.Text("games.roulette.use_wheel_status_to_check_countdown_from"),
		},
	}

	_, err := rouletteSession.ChannelMessageSendEmbed(channelID, embed)
	if err != nil {
		log.Printf(locale.Text("games.roulette.error_sending_roulette_betting_open_message"), err)
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
			winnersSb.WriteString(locale.Text("games.roulette.payout_bet.formatted", locale.Data{"UserID": userID, "NetProfit": stats.NetProfit, "CurrencySymbol": config.Bot.CurrencySymbol, "TotalPayout": stats.TotalPayout, "CurrencySymbol5": config.Bot.CurrencySymbol, "TotalWagered": stats.TotalWagered, "CurrencySymbol7": config.Bot.CurrencySymbol}))
		}
	}

	winnersList := locale.Text("games.roulette.no_winners_this_round")
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
		Title:       locale.Text("games.roulette.roulette_result_5075f4"),
		Description: locale.Text("games.roulette.the_ball_landed_on.formatted", locale.Data{"Emoji": emoji, "ResultNum": resultNum, "Strings": strings.ToUpper(resultColor), "Strings4": strings.ToUpper(resultColor), "ResultNum5": resultNum}),
		Color:       color,
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   locale.Text("games.roulette.net_winners"),
				Value:  winnersList,
				Inline: false,
			},
			{
				Name:   locale.Text("games.roulette.round_stats"),
				Value:  locale.Text("games.roulette.total_bets_total_wagered.formatted", locale.Data{"TotalBets": totalBets, "TotalAmount": totalAmount, "CurrencySymbol": config.Bot.CurrencySymbol}),
				Inline: false,
			},
			{
				Name:   locale.Text("games.roulette.recent_spins"),
				Value:  formatRecentHistory(),
				Inline: false,
			},
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: locale.Text("games.roulette.next_round_starting_shortly"),
		},
		Timestamp: time.Now().Format(time.RFC3339),
	}

	_, err := rouletteSession.ChannelMessageSendEmbed(channelID, embed)
	if err != nil {
		log.Printf(locale.Text("games.roulette.error_sending_roulette_result_message"), err)
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

func formatBet(betType BetType, value string) string {
	switch betType {
	case BetNumber:
		return locale.Text("games.roulette.number") + value
	case BetColor:
		if value == "red" {
			return locale.Text("games.roulette.red")
		}
		return locale.Text("games.roulette.black")
	case BetEvenOdd:
		if value == "even" {
			return locale.Text("games.roulette.even")
		}
		return locale.Text("games.roulette.odd")
	case BetHalf:
		if value == "1-18" {
			return locale.Text("games.roulette.low")
		}
		return locale.Text("games.roulette.high")
	case BetDozen:
		return value + locale.Text("games.roulette.dozen")
	}
	return value
}

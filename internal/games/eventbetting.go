package games

import (
	"estudocoin/internal/database"
	"estudocoin/pkg/config"
	"estudocoin/pkg/utils"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

const MinEventBet = 10

// House edge percentage (kept by bot to prevent exploits)
const HouseEdge = 0.05

type EventOption struct {
	ID          string
	Name        string
	TotalBets   int
	TotalAmount int
}

type UserBet struct {
	UserID   string
	Username string
	OptionID string
	Amount   int
}

type BettingEvent struct {
	ID          string
	Question    string
	Options     map[string]*EventOption
	UserBets    map[string]*UserBet // Key: userID_optionID
	TotalPool   int
	CreatorID   string
	ChannelID   string
	EndTime     time.Time
	Closed      bool
	WinnerID    string
	MessageID   string
	mu          sync.RWMutex
}

var (
	activeEvents    = make(map[string]*BettingEvent)
	eventsMu        sync.RWMutex
	eventSession    *discordgo.Session
)

func StartEventBetting(s *discordgo.Session) {
	eventSession = s
}

// CreateEvent creates a new betting event (admin only)
func CreateEvent(adminID, question string, options []string, durationMinutes int, channelID string) (*BettingEvent, string) {
	if len(options) < 2 {
		return nil, "Need at least 2 options."
	}
	if len(options) > 10 {
		return nil, "Maximum 10 options allowed."
	}
	if durationMinutes < 1 || durationMinutes > 1440 {
		return nil, "Duration must be between 1 and 1440 minutes (24 hours)."
	}
	if len(question) < 5 || len(question) > 200 {
		return nil, "Question must be between 5 and 200 characters."
	}

	eventID := generateEventID()
	event := &BettingEvent{
		ID:        eventID,
		Question:  question,
		Options:   make(map[string]*EventOption),
		UserBets:  make(map[string]*UserBet),
		CreatorID: adminID,
		ChannelID: channelID,
		EndTime:   time.Now().Add(time.Duration(durationMinutes) * time.Minute),
		Closed:    false,
	}

	// Create options
	for i, optName := range options {
		optID := fmt.Sprintf("opt_%d", i)
		event.Options[optID] = &EventOption{
			ID:        optID,
			Name:      strings.TrimSpace(optName),
			TotalBets: 0,
		}
	}

	eventsMu.Lock()
	activeEvents[eventID] = event
	eventsMu.Unlock()

	// Schedule auto-close
	go func() {
		time.Sleep(time.Duration(durationMinutes) * time.Minute)
		CloseEventAuto(eventID)
	}()

	return event, ""
}

func generateEventID() string {
	return fmt.Sprintf("evt_%d", time.Now().UnixNano())
}

// PlaceBet allows a user to place a bet
func PlaceBet(userID, username, eventID, optionID string, amount int) (bool, string) {
	if amount < MinEventBet {
		return false, fmt.Sprintf("Minimum bet is %d %s", MinEventBet, config.Bot.CurrencySymbol)
	}

	balance := database.GetBalance(userID)
	if balance < amount {
		return false, fmt.Sprintf("Insufficient balance! You have %d %s", balance, config.Bot.CurrencySymbol)
	}

	eventsMu.RLock()
	event, exists := activeEvents[eventID]
	eventsMu.RUnlock()

	if !exists {
		return false, "Event not found."
	}

	event.mu.Lock()
	defer event.mu.Unlock()

	if event.Closed {
		return false, "This event is closed."
	}

	if time.Now().After(event.EndTime) {
		event.Closed = true
		return false, "Betting time has ended."
	}

	option, exists := event.Options[optionID]
	if !exists {
		return false, "Invalid option."
	}

	// Check if user already bet on this event
	betKey := fmt.Sprintf("%s_%s", userID, optionID)
	if _, exists := event.UserBets[betKey]; exists {
		return false, "You already bet on this option. Use a different option or wait for the next event."
	}

	// Check if user bet on any option in this event
	for key, bet := range event.UserBets {
		if strings.HasPrefix(key, userID+"_") {
			return false, fmt.Sprintf("You already bet on '%s'. Only one bet per event allowed.", event.Options[bet.OptionID].Name)
		}
	}

	// Deduct coins
	if err := database.RemoveCoins(userID, amount); err != nil {
		return false, "Error processing bet."
	}

	// Record bet
	event.UserBets[betKey] = &UserBet{
		UserID:   userID,
		Username: username,
		OptionID: optionID,
		Amount:   amount,
	}

	option.TotalBets++
	option.TotalAmount += amount
	event.TotalPool += amount

	return true, ""
}

// CloseEventAuto closes betting automatically when time ends
func CloseEventAuto(eventID string) {
	eventsMu.RLock()
	event, exists := activeEvents[eventID]
	eventsMu.RUnlock()

	if !exists {
		return
	}

	event.mu.Lock()
	if event.Closed {
		event.mu.Unlock()
		return
	}
	event.Closed = true
	totalBets := len(event.UserBets)
	totalPool := event.TotalPool
	event.mu.Unlock()

	// Notify channel
	if eventSession != nil && totalBets > 0 {
		embed := &discordgo.MessageEmbed{
			Title:       "🔒 Betting Closed",
			Description: fmt.Sprintf("**%s**\n\nBetting is now closed! Waiting for admin to set the result.\n\nTotal Pool: **%d %s** | Total Bets: **%d**", event.Question, totalPool, config.Bot.CurrencySymbol, totalBets),
			Color:       0xFFA500,
			Footer: &discordgo.MessageEmbedFooter{
				Text: fmt.Sprintf("Event ID: %s | Use !result %s <option>", event.ID, event.ID),
			},
		}
		eventSession.ChannelMessageSendEmbed(event.ChannelID, embed)
	}
}

// SetResult sets the winning option and distributes prizes (admin only)
func SetResult(adminID, eventID, optionID string) (bool, string, map[string]int) {
	eventsMu.RLock()
	event, exists := activeEvents[eventID]
	eventsMu.RUnlock()

	if !exists {
		return false, "Event not found.", nil
	}

	event.mu.Lock()
	defer event.mu.Unlock()

	if event.CreatorID != adminID {
		return false, "Only the event creator can set the result.", nil
	}

	if !event.Closed && time.Now().Before(event.EndTime) {
		return false, "Betting is still active. Close the event first or wait for time to end.", nil
	}

	if event.WinnerID != "" {
		return false, "Result already set.", nil
	}

	winnerOption, exists := event.Options[optionID]
	if !exists {
		return false, "Invalid winning option.", nil
	}

	event.WinnerID = optionID

	// Calculate payouts
	payouts := make(map[string]int)
	
	if winnerOption.TotalAmount == 0 {
		// No one bet on winning option - house keeps everything
		return true, "No winners! House keeps the pool.", payouts
	}

	// Calculate pool after house edge
	poolAfterEdge := int(float64(event.TotalPool) * (1 - HouseEdge))
	houseProfit := event.TotalPool - poolAfterEdge

	// Distribute to winners proportionally
	for _, bet := range event.UserBets {
		if bet.OptionID == optionID {
			// User gets their bet back + share of losing pool
			userShare := float64(bet.Amount) / float64(winnerOption.TotalAmount)
			winnings := int(math.Floor(userShare * float64(poolAfterEdge)))
			
			database.AddCoins(bet.UserID, winnings)
			payouts[bet.UserID] = winnings - bet.Amount // Net profit
		}
	}

	return true, fmt.Sprintf("Result set! Distributed %d %s to winners. House kept %d %s.", 
		poolAfterEdge, config.Bot.CurrencySymbol, houseProfit, config.Bot.CurrencySymbol), payouts
}

// GetOdds calculates current odds for each option
func (e *BettingEvent) GetOdds() map[string]float64 {
	e.mu.RLock()
	defer e.mu.RUnlock()

	odds := make(map[string]float64)
	
	if e.TotalPool == 0 {
		// No bets yet, equal odds
		for optID := range e.Options {
			odds[optID] = 1.0
		}
		return odds
	}

	// Calculate implied probability and invert for odds
	poolAfterEdge := float64(e.TotalPool) * (1 - HouseEdge)
	
	for optID, opt := range e.Options {
		if opt.TotalAmount == 0 {
			odds[optID] = 99.99 // Very high odds if no bets
		} else {
			// Odds = (Pool after edge) / Amount bet on this option
			odds[optID] = poolAfterEdge / float64(opt.TotalAmount)
		}
	}
	
	return odds
}

func (e *BettingEvent) ToEmbed() *discordgo.MessageEmbed {
	e.mu.RLock()
	defer e.mu.RUnlock()

	timeLeft := e.EndTime.Sub(time.Now())
	status := "🟢 Open"
	color := 0x00FF00
	
	if e.Closed || timeLeft <= 0 {
		status = "🔒 Closed"
		color = 0xFF0000
	}

	odds := e.GetOdds()

	var optionsText strings.Builder
	for _, opt := range e.Options {
		oddsVal := odds[opt.ID]
		oddsStr := fmt.Sprintf("%.2fx", oddsVal)
		if oddsVal >= 99 {
			oddsStr = "∞"
		}
		
		optionsText.WriteString(fmt.Sprintf("**%s** - Odds: %s | Bets: %d (%d %s)\n", 
			opt.Name, oddsStr, opt.TotalBets, opt.TotalAmount, config.Bot.CurrencySymbol))
	}

	footerText := fmt.Sprintf("Event ID: %s | Min Bet: %d %s", e.ID, MinEventBet, config.Bot.CurrencySymbol)
	if !e.Closed && timeLeft > 0 {
		footerText += fmt.Sprintf(" | Ends in %d min", int(timeLeft.Minutes()))
	}

	return &discordgo.MessageEmbed{
		Title:       fmt.Sprintf("🎲 %s", e.Question),
		Description: fmt.Sprintf("**Status:** %s\n**Total Pool:** %d %s\n\n%s", 
			status, e.TotalPool, config.Bot.CurrencySymbol, optionsText.String()),
		Color: color,
		Footer: &discordgo.MessageEmbedFooter{
			Text: footerText,
		},
		Timestamp: time.Now().Format(time.RFC3339),
	}
}

// Command handlers
func CmdCreateEvent(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	// Args: question | option1 | option2 | ... | duration_minutes
	if len(args) < 4 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed(
			"Usage: `!createevent <question> | <option1> | <option2> | ... | <duration_minutes>`\n" +
			"Example: `!createevent Will Team A win? | Yes | No | Maybe | 30`"))
		return
	}

	// Parse last arg as duration
	durationStr := args[len(args)-1]
	duration, err := strconv.Atoi(durationStr)
	if err != nil {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Invalid duration. Use minutes (e.g., 30)"))
		return
	}

	// Join remaining args and split by |
	argsStr := strings.Join(args[:len(args)-1], " ")
	parts := strings.Split(argsStr, "|")
	
	if len(parts) < 3 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Need question and at least 2 options separated by |"))
		return
	}

	question := strings.TrimSpace(parts[0])
	options := make([]string, 0, len(parts)-1)
	for i := 1; i < len(parts); i++ {
		opt := strings.TrimSpace(parts[i])
		if opt != "" {
			options = append(options, opt)
		}
	}

	event, errMsg := CreateEvent(m.Author.ID, question, options, duration, m.ChannelID)
	if event == nil {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed(errMsg))
		return
	}

	embed := event.ToEmbed()
	msg, _ := s.ChannelMessageSendEmbed(m.ChannelID, embed)
	
	if msg != nil {
		event.mu.Lock()
		event.MessageID = msg.ID
		event.mu.Unlock()
	}
}

func CmdPlaceBet(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	// Args: event_id option_number amount
	if len(args) < 3 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed(
			"Usage: `!betevent <event_id> <option_number> <amount>`\n" +
			"Example: `!betevent evt_123456 1 100` (bets 100 on option 1)"))
		return
	}

	eventID := args[0]
	optNum, err := strconv.Atoi(args[1])
	if err != nil || optNum < 1 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Invalid option number."))
		return
	}
	
	amount, err := strconv.Atoi(args[2])
	if err != nil || amount < MinEventBet {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed(fmt.Sprintf("Invalid amount. Minimum is %d", MinEventBet)))
		return
	}

	// Find event
	eventsMu.RLock()
	event, exists := activeEvents[eventID]
	eventsMu.RUnlock()

	if !exists {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Event not found. Use `!events` to see active events."))
		return
	}

	// Get option ID from number
	event.mu.RLock()
	optionID := fmt.Sprintf("opt_%d", optNum-1)
	option, exists := event.Options[optionID]
	if !exists {
		event.mu.RUnlock()
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Invalid option number."))
		return
	}
	event.mu.RUnlock()

	success, msg := PlaceBet(m.Author.ID, m.Author.Username, eventID, optionID, amount)
	if !success {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed(msg))
		return
	}

	s.ChannelMessageSendEmbed(m.ChannelID, utils.SuccessEmbed("Bet Placed!", 
		fmt.Sprintf("You bet **%d %s** on **%s** (Option %d)", amount, config.Bot.CurrencySymbol, option.Name, optNum)))

	// Update event embed if possible
	if event.MessageID != "" {
		s.ChannelMessageEditEmbed(m.ChannelID, event.MessageID, event.ToEmbed())
	}
}

func CmdSetResult(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	// Args: event_id option_number
	if len(args) < 2 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed(
			"Usage: `!result <event_id> <option_number>`\n" +
			"Example: `!result evt_123456 1` (sets option 1 as winner)"))
		return
	}

	eventID := args[0]
	optNum, err := strconv.Atoi(args[1])
	if err != nil || optNum < 1 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Invalid option number."))
		return
	}

	optionID := fmt.Sprintf("opt_%d", optNum-1)

	success, msg, payouts := SetResult(m.Author.ID, eventID, optionID)
	if !success {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed(msg))
		return
	}

	// Get event info for result embed
	eventsMu.RLock()
	event := activeEvents[eventID]
	eventsMu.RUnlock()

	var winnerName string
	if event != nil {
		event.mu.RLock()
		if opt, exists := event.Options[optionID]; exists {
			winnerName = opt.Name
		}
		event.mu.RUnlock()
	}

	// Build winners list
	winnersText := "No winners this time."
	if len(payouts) > 0 {
		var sb strings.Builder
		for userID, profit := range payouts {
			sb.WriteString(fmt.Sprintf("<@%s>: +%d %s\n", userID, profit, config.Bot.CurrencySymbol))
		}
		winnersText = sb.String()
	}

	embed := &discordgo.MessageEmbed{
		Title:       "🏆 Event Result!",
		Description: fmt.Sprintf("**%s**\n\n**Winner:** %s", event.Question, winnerName),
		Color:       0xFFD700,
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   "💰 Winners",
				Value:  winnersText,
				Inline: false,
			},
			{
				Name:   "📊 Summary",
				Value:  msg,
				Inline: false,
			},
		},
	}
	s.ChannelMessageSendEmbed(m.ChannelID, embed)

	// Clean up event after some time
	go func() {
		time.Sleep(1 * time.Hour)
		eventsMu.Lock()
		delete(activeEvents, eventID)
		eventsMu.Unlock()
	}()
}

func CmdListEvents(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	eventsMu.RLock()
	defer eventsMu.RUnlock()

	if len(activeEvents) == 0 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.InfoEmbed("Active Events", "No active betting events."))
		return
	}

	var sb strings.Builder
	for _, event := range activeEvents {
		event.mu.RLock()
		status := "🟢 Open"
		if event.Closed {
			status = "🔒 Closed"
		} else if time.Now().After(event.EndTime) {
			status = "⏰ Ended"
		}
		
		timeLeft := event.EndTime.Sub(time.Now())
		timeStr := fmt.Sprintf("Ends in %dm", int(timeLeft.Minutes()))
		if event.Closed {
			timeStr = "Waiting for result"
		}
		
		sb.WriteString(fmt.Sprintf("**%s** - %s\nID: `%s` | Pool: %d %s | %s\n\n", 
			event.Question, status, event.ID, event.TotalPool, config.Bot.CurrencySymbol, timeStr))
		event.mu.RUnlock()
	}

	s.ChannelMessageSendEmbed(m.ChannelID, utils.InfoEmbed("🎲 Active Betting Events", sb.String()))
}

func CmdViewEvent(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	if len(args) < 1 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Usage: `!event <event_id>`"))
		return
	}

	eventID := args[0]
	eventsMu.RLock()
	event, exists := activeEvents[eventID]
	eventsMu.RUnlock()

	if !exists {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Event not found."))
		return
	}

	embed := event.ToEmbed()
	s.ChannelMessageSendEmbed(m.ChannelID, embed)
}

func CmdCloseEvent(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	if len(args) < 1 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Usage: `!closeevent <event_id>`"))
		return
	}

	eventID := args[0]
	eventsMu.RLock()
	event, exists := activeEvents[eventID]
	eventsMu.RUnlock()

	if !exists {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Event not found."))
		return
	}

	event.mu.Lock()
	if event.CreatorID != m.Author.ID {
		event.mu.Unlock()
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Only the event creator can close it early."))
		return
	}
	event.Closed = true
	event.mu.Unlock()

	CloseEventAuto(eventID)
	s.ChannelMessageSendEmbed(m.ChannelID, utils.SuccessEmbed("Event Closed", "Betting is now closed. Use `!result` to set the winner."))
}

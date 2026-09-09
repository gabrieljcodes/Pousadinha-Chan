package games

import (
	"crypto/rand"
	"bot/internal/database"
	"bot/pkg/config"
	"bot/pkg/utils"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

const MinEventBet = 10

// HouseEdge is the percentage of the losing pool kept by the house
const HouseEdge = 0.05

type EventOption struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	TotalBets   int    `json:"total_bets"`
	TotalAmount int    `json:"total_amount"`
}

type BettingEvent struct {
	ID         string
	GuildID    string
	ChannelID  string
	MessageID  string
	CreatorID  string
	Question   string
	Options    []*EventOption // Ordered slice ensures deterministic 1-based indexing
	TotalPool  int
	Status     string // "open", "closed", "resolved", "cancelled"
	WinnerID   string
	EndTime    time.Time
	CreatedAt  time.Time
	ResolvedAt *time.Time
	mu         sync.RWMutex
}

var (
	activeEvents = make(map[string]*BettingEvent)
	eventsMu     sync.RWMutex
	eventSession *discordgo.Session
)

// StartEventBetting initializes active events and starts the background worker ticker
func StartEventBetting(s *discordgo.Session) {
	eventSession = s

	// Restore active events from PostgreSQL
	loadActiveEventsFromDB()

	// Start 30-second background ticker worker for automatic event closure and cleanup
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			checkAndAutoCloseEvents(s)
		}
	}()

	log.Println("[EventBetting] Background worker and database sync initialized successfully")
}

func loadActiveEventsFromDB() {
	dbEvents, err := database.GetActiveBettingEventsDB()
	if err != nil {
		log.Printf("[EventBetting] Error loading active events from database: %v", err)
		return
	}

	eventsMu.Lock()
	defer eventsMu.Unlock()

	for _, dbe := range dbEvents {
		activeEvents[dbe.ID] = fromDBEvent(&dbe)
	}
	log.Printf("[EventBetting] Restored %d active betting events from PostgreSQL", len(dbEvents))
}

func fromDBEvent(dbe *database.DBBettingEvent) *BettingEvent {
	options := make([]*EventOption, len(dbe.Options))
	for i, o := range dbe.Options {
		options[i] = &EventOption{
			ID:          o.ID,
			Name:        o.Name,
			TotalBets:   o.TotalBets,
			TotalAmount: o.TotalAmount,
		}
	}

	return &BettingEvent{
		ID:         dbe.ID,
		GuildID:    dbe.GuildID,
		ChannelID:  dbe.ChannelID,
		MessageID:  dbe.MessageID,
		CreatorID:  dbe.CreatorID,
		Question:   dbe.Question,
		Options:    options,
		TotalPool:  dbe.TotalPool,
		Status:     dbe.Status,
		WinnerID:   dbe.WinnerID,
		EndTime:    dbe.EndTime,
		CreatedAt:  dbe.CreatedAt,
		ResolvedAt: dbe.ResolvedAt,
	}
}

func checkAndAutoCloseEvents(s *discordgo.Session) {
	closedIDs, err := database.AutoCloseExpiredBettingEventsDB()
	if err != nil {
		log.Printf("[EventBetting] Error auto-closing expired events: %v", err)
		return
	}

	for _, id := range closedIDs {
		eventsMu.RLock()
		event, exists := activeEvents[id]
		eventsMu.RUnlock()

		if !exists {
			continue
		}

		event.mu.Lock()
		event.Status = "closed"
		channelID := event.ChannelID
		messageID := event.MessageID
		question := event.Question
		totalPool := event.TotalPool
		totalBets := 0
		for _, opt := range event.Options {
			totalBets += opt.TotalBets
		}
		embed := event.ToEmbed()
		event.mu.Unlock()

		// Notify channel that betting has closed
		if s != nil && channelID != "" {
			closeEmbed := &discordgo.MessageEmbed{
				Title:       "🔒 Betting Closed",
				Description: fmt.Sprintf("**%s**\n\nBetting is now closed! Awaiting result from admin/creator.\n\nTotal Pool: **%d %s** | Total Bets: **%d**", question, totalPool, config.Bot.CurrencySymbol, totalBets),
				Color:       0xFFA500,
				Footer: &discordgo.MessageEmbedFooter{
					Text: fmt.Sprintf("Event ID: %s | Use !result %s <option_number>", id, id),
				},
			}
			_, _ = s.ChannelMessageSendEmbed(channelID, closeEmbed)

			// Update original message embed if possible
			if messageID != "" {
				_, _ = s.ChannelMessageEditEmbed(channelID, messageID, embed)
			}
		}
	}

	// Clean up resolved and cancelled events from memory cache if finished over 30 minutes ago
	eventsMu.Lock()
	for id, evt := range activeEvents {
		evt.mu.RLock()
		isDone := evt.Status == "resolved" || evt.Status == "cancelled"
		isOld := evt.ResolvedAt != nil && time.Since(*evt.ResolvedAt) > 30*time.Minute
		evt.mu.RUnlock()

		if isDone && isOld {
			delete(activeEvents, id)
		}
	}
	eventsMu.Unlock()
}

func generateUniqueEventID() string {
	for {
		b := make([]byte, 2)
		_, _ = rand.Read(b)
		id := fmt.Sprintf("evt_%x", b)

		eventsMu.RLock()
		_, exists := activeEvents[id]
		eventsMu.RUnlock()

		if !exists {
			return id
		}
	}
}

// CreateEvent creates a new betting event and persists it in PostgreSQL
func CreateEvent(guildID, channelID, adminID, question string, options []string, durationMinutes int) (*BettingEvent, string) {
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

	eventID := generateUniqueEventID()
	now := time.Now()
	endTime := now.Add(time.Duration(durationMinutes) * time.Minute)

	event := &BettingEvent{
		ID:        eventID,
		GuildID:   guildID,
		ChannelID: channelID,
		CreatorID: adminID,
		Question:  question,
		Options:   make([]*EventOption, len(options)),
		TotalPool: 0,
		Status:    "open",
		EndTime:   endTime,
		CreatedAt: now,
	}

	dbOptions := make([]database.DBEventOption, len(options))
	for i, optName := range options {
		optID := fmt.Sprintf("opt_%d", i)
		trimmed := strings.TrimSpace(optName)
		event.Options[i] = &EventOption{
			ID:          optID,
			Name:        trimmed,
			TotalBets:   0,
			TotalAmount: 0,
		}
		dbOptions[i] = database.DBEventOption{
			ID:          optID,
			Name:        trimmed,
			TotalBets:   0,
			TotalAmount: 0,
		}
	}

	dbEvt := &database.DBBettingEvent{
		ID:        event.ID,
		GuildID:   event.GuildID,
		ChannelID: event.ChannelID,
		CreatorID: event.CreatorID,
		Question:  event.Question,
		Options:   dbOptions,
		TotalPool: 0,
		Status:    "open",
		EndTime:   event.EndTime,
		CreatedAt: event.CreatedAt,
	}

	if err := database.CreateBettingEventDB(dbEvt); err != nil {
		log.Printf("[CreateEvent] Database error creating event: %v", err)
		return nil, "Failed to persist betting event in database."
	}

	eventsMu.Lock()
	activeEvents[eventID] = event
	eventsMu.Unlock()

	return event, ""
}

// PlaceBet places a bet on an option using an atomic database transaction
func PlaceBet(userID, username, eventID string, optIndex int, amount int) (bool, string) {
	if amount < MinEventBet {
		return false, fmt.Sprintf("Minimum bet is %d %s", MinEventBet, config.Bot.CurrencySymbol)
	}

	eventsMu.RLock()
	event, exists := activeEvents[eventID]
	eventsMu.RUnlock()

	if !exists {
		return false, "Event not found or betting is closed."
	}

	event.mu.RLock()
	if optIndex < 1 || optIndex > len(event.Options) {
		event.mu.RUnlock()
		return false, fmt.Sprintf("Invalid option number. Choose between 1 and %d.", len(event.Options))
	}
	targetOpt := event.Options[optIndex-1]
	optionID := targetOpt.ID
	event.mu.RUnlock()

	updatedDBEvt, err := database.PlaceBetAtomic(eventID, userID, username, optionID, amount)
	if err != nil {
		return false, err.Error()
	}

	// Update in-memory state from verified database state
	event.mu.Lock()
	event.TotalPool = updatedDBEvt.TotalPool
	for i, dbOpt := range updatedDBEvt.Options {
		if i < len(event.Options) {
			event.Options[i].TotalBets = dbOpt.TotalBets
			event.Options[i].TotalAmount = dbOpt.TotalAmount
		}
	}
	event.mu.Unlock()

	return true, ""
}

// SetResult sets the winning option and atomically distributes prizes to winners
func SetResult(requesterID, eventID string, optIndex int) (bool, string, map[string]int) {
	eventsMu.RLock()
	event, exists := activeEvents[eventID]
	eventsMu.RUnlock()

	if !exists {
		dbEvt, err := database.GetBettingEventByID(eventID)
		if err != nil || dbEvt == nil {
			return false, "Event not found.", nil
		}
		event = fromDBEvent(dbEvt)
		eventsMu.Lock()
		activeEvents[eventID] = event
		eventsMu.Unlock()
	}

	event.mu.RLock()
	if optIndex < 1 || optIndex > len(event.Options) {
		event.mu.RUnlock()
		return false, fmt.Sprintf("Invalid option number. Choose between 1 and %d.", len(event.Options)), nil
	}
	targetOpt := event.Options[optIndex-1]
	optionID := targetOpt.ID
	event.mu.RUnlock()

	payouts, totalDistributed, houseProfit, err := database.ResolveBettingEventAtomic(
		eventID, optionID, database.BotUserID, HouseEdge,
	)
	if err != nil {
		return false, err.Error(), nil
	}

	event.mu.Lock()
	event.Status = "resolved"
	event.WinnerID = optionID
	now := time.Now()
	event.ResolvedAt = &now
	event.mu.Unlock()

	msg := fmt.Sprintf("Distributed **%d %s** to winners. House profit: **%d %s**.",
		totalDistributed, config.Bot.CurrencySymbol, houseProfit, config.Bot.CurrencySymbol)
	return true, msg, payouts
}

// CancelEvent cancels an event and refunds 100% of all bets
func CancelEvent(requesterID, eventID string) (bool, string, int) {
	eventsMu.RLock()
	event, exists := activeEvents[eventID]
	eventsMu.RUnlock()

	if !exists {
		dbEvt, err := database.GetBettingEventByID(eventID)
		if err != nil || dbEvt == nil {
			return false, "Event not found.", 0
		}
		event = fromDBEvent(dbEvt)
		eventsMu.Lock()
		activeEvents[eventID] = event
		eventsMu.Unlock()
	}

	refunds, totalRefunded, err := database.CancelBettingEventAtomic(eventID)
	if err != nil {
		return false, err.Error(), 0
	}

	event.mu.Lock()
	event.Status = "cancelled"
	now := time.Now()
	event.ResolvedAt = &now
	event.mu.Unlock()

	return true, fmt.Sprintf("Event cancelled. Refunded **%d %s** to %d bettors.",
		totalRefunded, config.Bot.CurrencySymbol, len(refunds)), totalRefunded
}

// getOddsLocked calculates odds without acquiring locks (caller MUST hold e.mu.RLock)
// This strictly avoids nested RLock deadlocks when called by ToEmbed()
func (e *BettingEvent) getOddsLocked() map[string]float64 {
	odds := make(map[string]float64)

	if e.TotalPool == 0 {
		for _, opt := range e.Options {
			odds[opt.ID] = 1.0
		}
		return odds
	}

	poolAfterEdge := float64(e.TotalPool) * (1 - HouseEdge)
	for _, opt := range e.Options {
		if opt.TotalAmount == 0 {
			odds[opt.ID] = 99.99
		} else {
			odd := poolAfterEdge / float64(opt.TotalAmount)
			if odd < 1.0 {
				odd = 1.0
			}
			odds[opt.ID] = odd
		}
	}
	return odds
}

// GetOdds public concurrency-safe getter for option odds
func (e *BettingEvent) GetOdds() map[string]float64 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.getOddsLocked()
}

// ToEmbed builds the Discord status embed deterministically and free from deadlocks
func (e *BettingEvent) ToEmbed() *discordgo.MessageEmbed {
	e.mu.RLock()
	defer e.mu.RUnlock()

	timeLeft := time.Until(e.EndTime)
	status := "🟢 Open"
	color := 0x00FF00

	if e.Status == "cancelled" {
		status = "❌ Cancelled"
		color = 0x888888
	} else if e.Status == "resolved" {
		status = "🏆 Resolved"
		color = 0xFFD700
	} else if e.Status == "closed" || timeLeft <= 0 {
		status = "🔒 Closed (Awaiting Result)"
		color = 0xFFA500
	}

	odds := e.getOddsLocked()

	var optionsText strings.Builder
	for i, opt := range e.Options {
		oddsVal := odds[opt.ID]
		oddsStr := fmt.Sprintf("%.2fx", oddsVal)
		if oddsVal >= 99 {
			oddsStr = "∞"
		}

		optionsText.WriteString(fmt.Sprintf("**%d.** %s — Odds: **%s** | Bets: %d (%d %s)\n",
			i+1, opt.Name, oddsStr, opt.TotalBets, opt.TotalAmount, config.Bot.CurrencySymbol))
	}

	footerText := fmt.Sprintf("Event ID: %s | Min Bet: %d %s", e.ID, MinEventBet, config.Bot.CurrencySymbol)
	if e.Status == "open" && timeLeft > 0 {
		footerText += fmt.Sprintf(" | Ends in %d min", int(timeLeft.Minutes())+1)
	}

	return &discordgo.MessageEmbed{
		Title: fmt.Sprintf("🎲 %s", e.Question),
		Description: fmt.Sprintf("**Status:** %s\n**Total Pool:** %d %s\n\n%s\n*Use `!betevent %s <number> <amount>` to place a bet!*",
			status, e.TotalPool, config.Bot.CurrencySymbol, optionsText.String(), e.ID),
		Color: color,
		Footer: &discordgo.MessageEmbedFooter{
			Text: footerText,
		},
		Timestamp: time.Now().Format(time.RFC3339),
	}
}

// hasAdminPermission verifies if the user is a guild administrator or has Manage Server permissions
func hasAdminPermission(s *discordgo.Session, m *discordgo.MessageCreate) bool {
	if m.GuildID == "" {
		return false
	}

	guild, err := s.State.Guild(m.GuildID)
	if err == nil && guild != nil && guild.OwnerID == m.Author.ID {
		return true
	}

	perms, err := s.UserChannelPermissions(m.Author.ID, m.ChannelID)
	if err == nil {
		return (perms&discordgo.PermissionAdministrator != 0) || (perms&discordgo.PermissionManageServer != 0)
	}

	member, err := s.GuildMember(m.GuildID, m.Author.ID)
	if err == nil && member != nil && guild != nil && guild.OwnerID == member.User.ID {
		return true
	}

	return false
}

// canManageEvent checks if requester is either the event creator or a server admin
func canManageEvent(s *discordgo.Session, m *discordgo.MessageCreate, event *BettingEvent) bool {
	if hasAdminPermission(s, m) {
		return true
	}
	event.mu.RLock()
	defer event.mu.RUnlock()
	return event.CreatorID == m.Author.ID
}

// --- Discord Command Handlers ---

func sendCreateEventUsage(s *discordgo.Session, channelID string) {
	s.ChannelMessageSendEmbed(channelID, utils.ErrorEmbed(
		"**Usage:** `!createevent <question> | <option1> | <option2> | ... [| duration_minutes]`\n\n"+
			"**Examples:**\n"+
			"• `!createevent Who will win the derby? | Team A | Team B | 30`\n"+
			"• `!createevent Will it rain tomorrow? | Yes | No` *(defaults to 60 min)*"))
}

// CmdCreateEvent creates a new betting event (admin only)
func CmdCreateEvent(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	if !hasAdminPermission(s, m) {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("You need Administrator or Manage Server permissions to create betting events."))
		return
	}

	content := strings.TrimSpace(m.Content)
	spaceIdx := strings.Index(content, " ")
	if spaceIdx == -1 {
		sendCreateEventUsage(s, m.ChannelID)
		return
	}

	rawArgs := strings.TrimSpace(content[spaceIdx:])
	rawParts := strings.Split(rawArgs, "|")
	var parts []string
	for _, p := range rawParts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			parts = append(parts, trimmed)
		}
	}

	if len(parts) < 3 {
		sendCreateEventUsage(s, m.ChannelID)
		return
	}

	question := parts[0]
	durationMinutes := 60

	// Check if the last item is a number (duration in minutes)
	lastPart := parts[len(parts)-1]
	parsedDuration, err := strconv.Atoi(lastPart)
	var options []string

	if err == nil && len(parts) >= 4 {
		durationMinutes = parsedDuration
		options = parts[1 : len(parts)-1]
	} else {
		options = parts[1:]
	}

	if len(options) < 2 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Need at least 2 options separated by `|`."))
		return
	}

	event, errMsg := CreateEvent(m.GuildID, m.ChannelID, m.Author.ID, question, options, durationMinutes)
	if event == nil {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed(errMsg))
		return
	}

	embed := event.ToEmbed()
	msg, err := s.ChannelMessageSendEmbed(m.ChannelID, embed)
	if err == nil && msg != nil {
		event.mu.Lock()
		event.MessageID = msg.ID
		event.mu.Unlock()
		_ = database.UpdateEventMessageIDDB(event.ID, msg.ID)
	}
}

// CmdPlaceBet places a user's bet on an option
func CmdPlaceBet(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	if len(args) < 3 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed(
			"**Usage:** `!betevent <event_id> <option_number> <amount>`\n"+
				"**Example:** `!betevent evt_4821 1 100` (bets 100 on option 1)"))
		return
	}

	eventID := strings.TrimSpace(args[0])
	optNum, err := strconv.Atoi(args[1])
	if err != nil || optNum < 1 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Invalid option number. Use the numbers shown in the event (e.g. 1, 2)."))
		return
	}

	amount, err := strconv.Atoi(args[2])
	if err != nil || amount < MinEventBet {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed(fmt.Sprintf("Invalid amount. Minimum bet is %d %s.", MinEventBet, config.Bot.CurrencySymbol)))
		return
	}

	eventsMu.RLock()
	event, exists := activeEvents[eventID]
	eventsMu.RUnlock()

	if !exists {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Event not found. Use `!events` to see active events."))
		return
	}

	success, msg := PlaceBet(m.Author.ID, m.Author.Username, eventID, optNum, amount)
	if !success {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed(msg))
		return
	}

	event.mu.RLock()
	optName := "Option"
	if optNum <= len(event.Options) {
		optName = event.Options[optNum-1].Name
	}
	msgID := event.MessageID
	chID := event.ChannelID
	embed := event.ToEmbed()
	event.mu.RUnlock()

	s.ChannelMessageSendEmbed(m.ChannelID, utils.SuccessEmbed("Bet Placed!",
		fmt.Sprintf("You bet **%d %s** on **%d. %s**!", amount, config.Bot.CurrencySymbol, optNum, optName)))

	if msgID != "" && chID != "" {
		_, _ = s.ChannelMessageEditEmbed(chID, msgID, embed)
	}
}

// CmdSetResult declares the winning option and distributes prizes (admin or creator only)
func CmdSetResult(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	if len(args) < 2 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed(
			"**Usage:** `!result <event_id> <option_number>`\n"+
				"**Example:** `!result evt_4821 1` (sets option 1 as winner)"))
		return
	}

	eventID := strings.TrimSpace(args[0])
	optNum, err := strconv.Atoi(args[1])
	if err != nil || optNum < 1 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Invalid option number."))
		return
	}

	eventsMu.RLock()
	event, exists := activeEvents[eventID]
	eventsMu.RUnlock()

	if !exists {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Event not found."))
		return
	}

	if !canManageEvent(s, m, event) {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Only the event creator or a server administrator can declare results."))
		return
	}

	success, msg, payouts := SetResult(m.Author.ID, eventID, optNum)
	if !success {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed(msg))
		return
	}

	event.mu.RLock()
	winnerName := "Unknown"
	if optNum <= len(event.Options) {
		winnerName = event.Options[optNum-1].Name
	}
	question := event.Question
	msgID := event.MessageID
	chID := event.ChannelID
	embed := event.ToEmbed()
	event.mu.RUnlock()

	winnersText := "No winners this time."
	if len(payouts) > 0 {
		var sb strings.Builder
		for userID, profit := range payouts {
			sb.WriteString(fmt.Sprintf("<@%s>: **+%d %s** (profit)\n", userID, profit, config.Bot.CurrencySymbol))
		}
		winnersText = sb.String()
	}

	resultEmbed := &discordgo.MessageEmbed{
		Title:       "🏆 Event Result!",
		Description: fmt.Sprintf("**%s**\n\n**Winner:** **%d. %s**", question, optNum, winnerName),
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
	s.ChannelMessageSendEmbed(m.ChannelID, resultEmbed)

	if msgID != "" && chID != "" {
		_, _ = s.ChannelMessageEditEmbed(chID, msgID, embed)
	}
}

// CmdCancelEvent cancels an event and refunds all bets (admin or creator only)
func CmdCancelEvent(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	if len(args) < 1 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed(
			"**Usage:** `!cancelevent <event_id>`\n"+
				"**Example:** `!cancelevent evt_4821` (cancels event and refunds all bets)"))
		return
	}

	eventID := strings.TrimSpace(args[0])

	eventsMu.RLock()
	event, exists := activeEvents[eventID]
	eventsMu.RUnlock()

	if !exists {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Event not found."))
		return
	}

	if !canManageEvent(s, m, event) {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Only the event creator or a server administrator can cancel this event."))
		return
	}

	success, msg, _ := CancelEvent(m.Author.ID, eventID)
	if !success {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed(msg))
		return
	}

	event.mu.RLock()
	msgID := event.MessageID
	chID := event.ChannelID
	embed := event.ToEmbed()
	question := event.Question
	event.mu.RUnlock()

	cancelEmbed := &discordgo.MessageEmbed{
		Title:       "❌ Event Cancelled",
		Description: fmt.Sprintf("**%s**\n\n%s\n\nAll bets have been returned to user balances.", question, msg),
		Color:       0xFF0000,
	}
	s.ChannelMessageSendEmbed(m.ChannelID, cancelEmbed)

	if msgID != "" && chID != "" {
		_, _ = s.ChannelMessageEditEmbed(chID, msgID, embed)
	}
}

// CmdCloseEvent closes betting early (admin or creator only)
func CmdCloseEvent(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	if len(args) < 1 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Usage: `!closeevent <event_id>`"))
		return
	}

	eventID := strings.TrimSpace(args[0])
	eventsMu.RLock()
	event, exists := activeEvents[eventID]
	eventsMu.RUnlock()

	if !exists {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Event not found."))
		return
	}

	if !canManageEvent(s, m, event) {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Only the event creator or a server administrator can close it early."))
		return
	}

	if err := database.CloseBettingEventDB(eventID); err != nil {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Error closing event: "+err.Error()))
		return
	}

	event.mu.Lock()
	event.Status = "closed"
	msgID := event.MessageID
	chID := event.ChannelID
	embed := event.ToEmbed()
	event.mu.Unlock()

	if msgID != "" && chID != "" {
		_, _ = s.ChannelMessageEditEmbed(chID, msgID, embed)
	}

	s.ChannelMessageSendEmbed(m.ChannelID, utils.SuccessEmbed("Event Closed", "Betting is now closed. Use `!result <event_id> <option>` to declare the winner."))
}

// CmdListEvents lists active and closed events
func CmdListEvents(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	eventsMu.RLock()
	defer eventsMu.RUnlock()

	if len(activeEvents) == 0 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.InfoEmbed("Active Events", "No active betting events right now."))
		return
	}

	var sb strings.Builder
	count := 0
	for _, event := range activeEvents {
		event.mu.RLock()
		if event.Status == "resolved" || event.Status == "cancelled" {
			event.mu.RUnlock()
			continue
		}
		count++

		status := "🟢 Open"
		timeLeft := time.Until(event.EndTime)
		if event.Status == "closed" || timeLeft <= 0 {
			status = "🔒 Closed (Awaiting Result)"
		}

		timeStr := fmt.Sprintf("Ends in %dm", int(timeLeft.Minutes())+1)
		if event.Status == "closed" || timeLeft <= 0 {
			timeStr = "Awaiting Result"
		}

		sb.WriteString(fmt.Sprintf("**%s** — %s\nID: `%s` | Pool: **%d %s** | %s\n\n",
			event.Question, status, event.ID, event.TotalPool, config.Bot.CurrencySymbol, timeStr))
		event.mu.RUnlock()
	}

	if count == 0 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.InfoEmbed("Active Events", "No active betting events right now."))
		return
	}

	s.ChannelMessageSendEmbed(m.ChannelID, utils.InfoEmbed("🎲 Active Betting Events", sb.String()))
}

// CmdViewEvent views details for a specific event
func CmdViewEvent(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	if len(args) < 1 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Usage: `!event <event_id>`"))
		return
	}

	eventID := strings.TrimSpace(args[0])
	eventsMu.RLock()
	event, exists := activeEvents[eventID]
	eventsMu.RUnlock()

	if !exists {
		// Attempt to load from database
		dbEvt, err := database.GetBettingEventByID(eventID)
		if err != nil || dbEvt == nil {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Event not found."))
			return
		}
		event = fromDBEvent(dbEvt)
		eventsMu.Lock()
		activeEvents[eventID] = event
		eventsMu.Unlock()
	}

	embed := event.ToEmbed()
	s.ChannelMessageSendEmbed(m.ChannelID, embed)
}

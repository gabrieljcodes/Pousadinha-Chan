package games

import (
	"bot/internal/database"
	"bot/internal/locale"
	"bot/pkg/config"
	"crypto/rand"

	"fmt"
	"log"

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
				Title:       locale.Text("games.eventbetting.betting_closed"),
				Description: locale.Text("games.eventbetting.betting_is_now_closed_awaiting_result_from.formatted", locale.Data{"Question": question, "TotalPool": totalPool, "CurrencySymbol": config.Bot.CurrencySymbol, "TotalBets": totalBets}),
				Color:       0xFFA500,
				Footer: &discordgo.MessageEmbedFooter{
					Text: locale.Text("games.eventbetting.event_id_use_event_result_option_number.formatted", locale.Data{"Id": id, "Id2": id}),
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
		return nil, locale.Text("games.eventbetting.need_at_least_options")
	}
	if len(options) > 10 {
		return nil, locale.Text("games.eventbetting.maximum_options_allowed")
	}
	if durationMinutes < 1 || durationMinutes > 1440 {
		return nil, locale.Text("games.eventbetting.duration_must_be_between_and_minutes_hours")
	}
	if len(question) < 5 || len(question) > 200 {
		return nil, locale.Text("games.eventbetting.question_must_be_between_and_characters")
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
		return nil, locale.Text("games.eventbetting.failed_to_persist_betting_event_in_database")
	}

	eventsMu.Lock()
	activeEvents[eventID] = event
	eventsMu.Unlock()

	return event, ""
}

// PlaceBet places a bet on an option using an atomic database transaction
func PlaceBet(userID, username, eventID string, optIndex int, amount int) (bool, string) {
	if amount < MinEventBet {
		return false, locale.Text("games.cups.minimum_bet_is.formatted1", locale.Data{"MinEventBet": MinEventBet, "CurrencySymbol": config.Bot.CurrencySymbol})
	}

	eventsMu.RLock()
	event, exists := activeEvents[eventID]
	eventsMu.RUnlock()

	if !exists {
		return false, locale.Text("games.eventbetting.event_not_found_or_betting_is_closed")
	}

	event.mu.RLock()
	if optIndex < 1 || optIndex > len(event.Options) {
		event.mu.RUnlock()
		return false, locale.Text("games.eventbetting.invalid_option_number_choose_between_and.formatted", locale.Data{"Value1": len(event.Options)})
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
			return false, locale.Text("games.eventbetting.event_not_found"), nil
		}
		event = fromDBEvent(dbEvt)
		eventsMu.Lock()
		activeEvents[eventID] = event
		eventsMu.Unlock()
	}

	event.mu.RLock()
	if optIndex < 1 || optIndex > len(event.Options) {
		event.mu.RUnlock()
		return false, locale.Text("games.eventbetting.invalid_option_number_choose_between_and.formatted", locale.Data{"Value1": len(event.Options)}), nil
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

	msg := locale.Text("games.eventbetting.distributed_to_winners_house_profit.formatted", locale.Data{"TotalDistributed": totalDistributed, "CurrencySymbol": config.Bot.CurrencySymbol, "HouseProfit": houseProfit, "CurrencySymbol4": config.Bot.CurrencySymbol})
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
			return false, locale.Text("games.eventbetting.event_not_found"), 0
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

	return true, locale.Text("games.eventbetting.event_cancelled_refunded_to_bettors.formatted", locale.Data{"TotalRefunded": totalRefunded, "CurrencySymbol": config.Bot.CurrencySymbol, "Value3": len(refunds)}), totalRefunded
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
	status := locale.Text("games.eventbetting.open")
	color := 0x00FF00

	if e.Status == "cancelled" {
		status = locale.Text("games.eventbetting.cancelled")
		color = 0x888888
	} else if e.Status == "resolved" {
		status = locale.Text("games.eventbetting.resolved")
		color = 0xFFD700
	} else if e.Status == "closed" || timeLeft <= 0 {
		status = locale.Text("games.eventbetting.closed_awaiting_result")
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

		optionsText.WriteString(locale.Text("games.eventbetting.odds_bets.formatted", locale.Data{"I": i + 1, "Name": opt.Name, "OddsStr": oddsStr, "TotalBets": opt.TotalBets, "TotalAmount": opt.TotalAmount, "CurrencySymbol": config.Bot.CurrencySymbol}))
	}

	footerText := locale.Text("games.eventbetting.event_id_min_bet.formatted", locale.Data{"ID": e.ID, "MinEventBet": MinEventBet, "CurrencySymbol": config.Bot.CurrencySymbol})
	if e.Status == "open" && timeLeft > 0 {
		footerText += locale.Text("games.eventbetting.ends_in_min.formatted", locale.Data{"Value1": int(timeLeft.Minutes()) + 1})
	}

	return &discordgo.MessageEmbed{
		Title:       fmt.Sprintf("🎲 %s", e.Question),
		Description: locale.Text("games.eventbetting.status_total_pool_use_event_bet_number.formatted", locale.Data{"Status": status, "TotalPool": e.TotalPool, "CurrencySymbol": config.Bot.CurrencySymbol, "OptionsText": optionsText.String(), "ID": e.ID}),
		Color:       color,
		Footer: &discordgo.MessageEmbedFooter{
			Text: footerText,
		},
		Timestamp: time.Now().Format(time.RFC3339),
	}
}

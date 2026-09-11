package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"time"
)

// DBEventOption represents a single betting choice stored within JSONB
type DBEventOption struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	TotalBets   int    `json:"total_bets"`
	TotalAmount int    `json:"total_amount"`
}

// DBBettingEvent represents an event record in PostgreSQL
type DBBettingEvent struct {
	ID         string          `json:"id"`
	GuildID    string          `json:"guild_id"`
	ChannelID  string          `json:"channel_id"`
	MessageID  string          `json:"message_id"`
	CreatorID  string          `json:"creator_id"`
	Question   string          `json:"question"`
	Options    []DBEventOption `json:"options"`
	TotalPool  int             `json:"total_pool"`
	Status     string          `json:"status"` // "open", "closed", "resolved", "cancelled"
	WinnerID   string          `json:"winner_id"`
	EndTime    time.Time       `json:"end_time"`
	CreatedAt  time.Time       `json:"created_at"`
	ResolvedAt *time.Time      `json:"resolved_at"`
}

// DBUserBet represents an individual bet placed on an event
type DBUserBet struct {
	ID        int64     `json:"id"`
	EventID   string    `json:"event_id"`
	UserID    string    `json:"user_id"`
	Username  string    `json:"username"`
	OptionID  string    `json:"option_id"`
	Amount    int       `json:"amount"`
	CreatedAt time.Time `json:"created_at"`
}

// CreateBettingEventDB inserts a new betting event into PostgreSQL
func CreateBettingEventDB(event *DBBettingEvent) error {
	optionsJSON, err := json.Marshal(event.Options)
	if err != nil {
		return fmt.Errorf("failed to marshal options: %w", err)
	}

	// Ensure creator exists in users table and guild_members first
	_, _ = DB.Exec(`INSERT INTO users (id, balance) VALUES ($1, 0) ON CONFLICT (id) DO NOTHING`, event.CreatorID)
	_, _ = DB.Exec(`INSERT INTO guild_members (guild_id, user_id, balance) VALUES ($1, $2, 0) ON CONFLICT (guild_id, user_id) DO NOTHING`, event.GuildID, event.CreatorID)

	query := `
		INSERT INTO betting_events (
			id, guild_id, channel_id, message_id, creator_id, question, options, total_pool, status, end_time, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`
	_, err = DB.Exec(query,
		event.ID,
		event.GuildID,
		event.ChannelID,
		event.MessageID,
		event.CreatorID,
		event.Question,
		string(optionsJSON),
		event.TotalPool,
		event.Status,
		event.EndTime,
		event.CreatedAt,
	)
	return err
}

// UpdateEventMessageIDDB stores the Discord message ID of an event
func UpdateEventMessageIDDB(eventID, messageID string) error {
	_, err := DB.Exec(`UPDATE betting_events SET message_id = $1 WHERE id = $2`, messageID, eventID)
	return err
}

// PlaceBetAtomic places a bet atomically, deducting the user's balance and updating pool counters
func PlaceBetAtomic(eventID, userID, username, optionID string, amount int) (*DBBettingEvent, error) {
	if amount <= 0 {
		return nil, fmt.Errorf("invalid bet amount")
	}

	tx, err := DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// 1. Lock and fetch event
	var (
		evt         DBBettingEvent
		optionsRaw  string
		resolvedAt  sql.NullTime
		messageID   sql.NullString
		winnerID    sql.NullString
	)

	query := `
		SELECT id, guild_id, channel_id, message_id, creator_id, question, options, total_pool, status, winner_id, end_time, created_at, resolved_at
		FROM betting_events
		WHERE id = $1
		FOR UPDATE
	`
	err = tx.QueryRow(query, eventID).Scan(
		&evt.ID, &evt.GuildID, &evt.ChannelID, &messageID,
		&evt.CreatorID, &evt.Question, &optionsRaw, &evt.TotalPool,
		&evt.Status, &winnerID, &evt.EndTime, &evt.CreatedAt, &resolvedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("event not found")
		}
		return nil, err
	}

	if messageID.Valid {
		evt.MessageID = messageID.String
	}
	if winnerID.Valid {
		evt.WinnerID = winnerID.String
	}
	if resolvedAt.Valid {
		evt.ResolvedAt = &resolvedAt.Time
	}

	if evt.Status != "open" {
		return nil, fmt.Errorf("this event is %s", evt.Status)
	}

	if time.Now().After(evt.EndTime) {
		// Mark closed in database
		_, _ = tx.Exec(`UPDATE betting_events SET status = 'closed' WHERE id = $1`, eventID)
		return nil, fmt.Errorf("betting time has ended for this event")
	}

	// 2. Deserialize options and find target option
	var options []DBEventOption
	if err := json.Unmarshal([]byte(optionsRaw), &options); err != nil {
		return nil, fmt.Errorf("corrupt options payload in database: %w", err)
	}

	optionIndex := -1
	for i := range options {
		if options[i].ID == optionID {
			optionIndex = i
			break
		}
	}
	if optionIndex == -1 {
		return nil, fmt.Errorf("invalid option ID")
	}

	// 3. Ensure user exists and check for prior bet on this event
	_, _ = tx.Exec(`INSERT INTO users (id, balance) VALUES ($1, 0) ON CONFLICT (id) DO NOTHING`, userID)
	_, _ = tx.Exec(`INSERT INTO guild_members (guild_id, user_id, balance) VALUES ($1, $2, 0) ON CONFLICT (guild_id, user_id) DO NOTHING`, evt.GuildID, userID)

	var existingBetsCount int
	err = tx.QueryRow(`SELECT COUNT(*) FROM event_bets WHERE event_id = $1 AND user_id = $2`, eventID, userID).Scan(&existingBetsCount)
	if err != nil {
		return nil, err
	}
	if existingBetsCount > 0 {
		return nil, fmt.Errorf("you have already placed a bet on this event (only one bet per event is allowed)")
	}

	// 4. Lock user row and verify balance
	var balance int
	err = tx.QueryRow(`SELECT balance FROM guild_members WHERE guild_id = $1 AND user_id = $2 FOR UPDATE`, evt.GuildID, userID).Scan(&balance)
	if err != nil {
		return nil, err
	}
	if balance < amount {
		return nil, fmt.Errorf("insufficient balance (you have %d)", balance)
	}

	// 5. Deduct coins from user balance atomically
	_, err = tx.Exec(`UPDATE guild_members SET balance = balance - $1, updated_at = NOW() WHERE guild_id = $2 AND user_id = $3`, amount, evt.GuildID, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to deduct balance: %w", err)
	}

	// 6. Record the bet
	insertBetQuery := `
		INSERT INTO event_bets (event_id, user_id, username, option_id, amount, created_at)
		VALUES ($1, $2, $3, $4, $5, NOW())
	`
	_, err = tx.Exec(insertBetQuery, eventID, userID, username, optionID, amount)
	if err != nil {
		return nil, fmt.Errorf("failed to record bet: %w", err)
	}

	// 7. Update options counts and event total pool
	options[optionIndex].TotalBets++
	options[optionIndex].TotalAmount += amount
	evt.TotalPool += amount
	evt.Options = options

	newOptionsJSON, err := json.Marshal(options)
	if err != nil {
		return nil, err
	}

	updateEventQuery := `
		UPDATE betting_events 
		SET options = $1, total_pool = total_pool + $2
		WHERE id = $3
	`
	_, err = tx.Exec(updateEventQuery, string(newOptionsJSON), amount, eventID)
	if err != nil {
		return nil, fmt.Errorf("failed to update event total: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &evt, nil
}

// CloseBettingEventDB marks an event as closed in the database
func CloseBettingEventDB(eventID string) error {
	_, err := DB.Exec(`UPDATE betting_events SET status = 'closed' WHERE id = $1 AND status = 'open'`, eventID)
	return err
}

// AutoCloseExpiredBettingEventsDB finds open events past their EndTime and closes them
func AutoCloseExpiredBettingEventsDB() ([]string, error) {
	rows, err := DB.Query(`
		UPDATE betting_events
		SET status = 'closed'
		WHERE status = 'open' AND end_time <= NOW()
		RETURNING id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var closedIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			closedIDs = append(closedIDs, id)
		}
	}
	return closedIDs, nil
}

// ResolveBettingEventAtomic calculates and distributes winnings atomically, guaranteeing winners never lose their principal
func ResolveBettingEventAtomic(eventID, winnerOptionID, houseUserID string, houseEdge float64) (payouts map[string]int, totalDistributed int, houseProfit int, err error) {
	tx, err := DB.Begin()
	if err != nil {
		return nil, 0, 0, err
	}
	defer tx.Rollback()

	// 1. Lock and load event
	var (
		evt        DBBettingEvent
		optionsRaw string
	)
	query := `
		SELECT id, guild_id, channel_id, creator_id, question, options, total_pool, status
		FROM betting_events
		WHERE id = $1
		FOR UPDATE
	`
	err = tx.QueryRow(query, eventID).Scan(
		&evt.ID, &evt.GuildID, &evt.ChannelID, &evt.CreatorID,
		&evt.Question, &optionsRaw, &evt.TotalPool, &evt.Status,
	)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("event not found: %w", err)
	}

	if evt.Status == "resolved" {
		return nil, 0, 0, fmt.Errorf("event is already resolved")
	}
	if evt.Status == "cancelled" {
		return nil, 0, 0, fmt.Errorf("cannot resolve a cancelled event")
	}

	var options []DBEventOption
	if err := json.Unmarshal([]byte(optionsRaw), &options); err != nil {
		return nil, 0, 0, fmt.Errorf("corrupt options payload: %w", err)
	}

	var winnerOption *DBEventOption
	for i := range options {
		if options[i].ID == winnerOptionID {
			winnerOption = &options[i]
			break
		}
	}
	if winnerOption == nil {
		return nil, 0, 0, fmt.Errorf("invalid winning option ID")
	}

	// 2. Fetch all bets
	rows, err := tx.Query(`
		SELECT user_id, username, option_id, amount
		FROM event_bets
		WHERE event_id = $1
	`, eventID)
	if err != nil {
		return nil, 0, 0, err
	}
	defer rows.Close()

	type betEntry struct {
		userID   string
		username string
		optionID string
		amount   int
	}
	var allBets []betEntry
	for rows.Next() {
		var b betEntry
		if err := rows.Scan(&b.userID, &b.username, &b.optionID, &b.amount); err != nil {
			return nil, 0, 0, err
		}
		allBets = append(allBets, b)
	}

	payouts = make(map[string]int)

	// Case A: Nobody bet on the winning option
	if winnerOption.TotalAmount == 0 {
		houseProfit = evt.TotalPool
		if houseProfit > 0 && houseUserID != "" {
			_, _ = tx.Exec(`INSERT INTO users (id, balance) VALUES ($1, 0) ON CONFLICT (id) DO NOTHING`, houseUserID)
			_, _ = tx.Exec(`INSERT INTO guild_members (guild_id, user_id, balance) VALUES ($1, $2, $3) ON CONFLICT (guild_id, user_id) DO UPDATE SET balance = guild_members.balance + $3, updated_at = NOW()`, evt.GuildID, houseUserID, houseProfit)
		}
		_, err = tx.Exec(`UPDATE betting_events SET status = 'resolved', winner_id = $1, resolved_at = NOW() WHERE id = $2`, winnerOptionID, eventID)
		if err != nil {
			return nil, 0, 0, err
		}
		return payouts, 0, houseProfit, tx.Commit()
	}

	// Case B: Winning bets exist
	losingPool := evt.TotalPool - winnerOption.TotalAmount
	distributableBonusPool := losingPool
	if losingPool > 0 {
		houseProfit = int(float64(losingPool) * houseEdge)
		distributableBonusPool = losingPool - houseProfit
	}

	for _, b := range allBets {
		if b.optionID == winnerOptionID {
			userShare := float64(b.amount) / float64(winnerOption.TotalAmount)
			bonus := int(math.Floor(userShare * float64(distributableBonusPool)))
			winnings := b.amount + bonus // Guaranteed >= b.amount (non-negative profit)
			profit := bonus

			// Credit winner balance atomically
			_, err = tx.Exec(`UPDATE guild_members SET balance = balance + $1, updated_at = NOW() WHERE guild_id = $2 AND user_id = $3`, winnings, evt.GuildID, b.userID)
			if err != nil {
				return nil, 0, 0, fmt.Errorf("failed to payout user %s: %w", b.userID, err)
			}

			payouts[b.userID] = profit
			totalDistributed += winnings
		}
	}

	// Any remainder from rounding or house profit is credited to the bot/house user
	residualHouseShare := evt.TotalPool - totalDistributed
	if residualHouseShare > 0 {
		houseProfit = residualHouseShare
		if houseUserID != "" {
			_, _ = tx.Exec(`INSERT INTO users (id, balance) VALUES ($1, 0) ON CONFLICT (id) DO NOTHING`, houseUserID)
			_, _ = tx.Exec(`INSERT INTO guild_members (guild_id, user_id, balance) VALUES ($1, $2, $3) ON CONFLICT (guild_id, user_id) DO UPDATE SET balance = guild_members.balance + $3, updated_at = NOW()`, evt.GuildID, houseUserID, houseProfit)
		}
	}

	// 3. Update status to resolved
	_, err = tx.Exec(`UPDATE betting_events SET status = 'resolved', winner_id = $1, resolved_at = NOW() WHERE id = $2`, winnerOptionID, eventID)
	if err != nil {
		return nil, 0, 0, err
	}

	return payouts, totalDistributed, houseProfit, tx.Commit()
}

// CancelBettingEventAtomic cancels an event and refunds 100% of all bets to respective users
func CancelBettingEventAtomic(eventID string) (refunds map[string]int, totalRefunded int, err error) {
	tx, err := DB.Begin()
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback()

	// 1. Lock and load event
	var (
		evt DBBettingEvent
	)
	query := `SELECT id, status, total_pool FROM betting_events WHERE id = $1 FOR UPDATE`
	err = tx.QueryRow(query, eventID).Scan(&evt.ID, &evt.Status, &evt.TotalPool)
	if err != nil {
		return nil, 0, fmt.Errorf("event not found: %w", err)
	}

	if evt.Status == "resolved" {
		return nil, 0, fmt.Errorf("cannot cancel an already resolved event")
	}
	if evt.Status == "cancelled" {
		return nil, 0, fmt.Errorf("event is already cancelled")
	}

	// 2. Fetch all bets
	rows, err := tx.Query(`SELECT user_id, amount FROM event_bets WHERE event_id = $1`, eventID)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	refunds = make(map[string]int)
	for rows.Next() {
		var userID string
		var amount int
		if err := rows.Scan(&userID, &amount); err != nil {
			return nil, 0, err
		}

		// Refund user balance
		_, err = tx.Exec(`UPDATE guild_members SET balance = balance + $1, updated_at = NOW() WHERE guild_id = $2 AND user_id = $3`, amount, evt.GuildID, userID)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to refund user %s: %w", userID, err)
		}
		refunds[userID] += amount
		totalRefunded += amount
	}

	// 3. Update status to cancelled
	_, err = tx.Exec(`UPDATE betting_events SET status = 'cancelled', resolved_at = NOW() WHERE id = $1`, eventID)
	if err != nil {
		return nil, 0, err
	}

	return refunds, totalRefunded, tx.Commit()
}

// GetActiveBettingEventsDB retrieves all open and closed events from PostgreSQL
func GetActiveBettingEventsDB() ([]DBBettingEvent, error) {
	query := `
		SELECT id, guild_id, channel_id, COALESCE(message_id, ''), creator_id, question, options, total_pool, status, COALESCE(winner_id, ''), end_time, created_at, resolved_at
		FROM betting_events
		WHERE status IN ('open', 'closed')
		ORDER BY created_at DESC
	`
	rows, err := DB.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []DBBettingEvent
	for rows.Next() {
		var (
			evt        DBBettingEvent
			optionsRaw string
			resolvedAt sql.NullTime
		)
		err := rows.Scan(
			&evt.ID, &evt.GuildID, &evt.ChannelID, &evt.MessageID,
			&evt.CreatorID, &evt.Question, &optionsRaw, &evt.TotalPool,
			&evt.Status, &evt.WinnerID, &evt.EndTime, &evt.CreatedAt, &resolvedAt,
		)
		if err != nil {
			log.Printf("[GetActiveBettingEventsDB] Error scanning event row: %v", err)
			continue
		}
		if resolvedAt.Valid {
			evt.ResolvedAt = &resolvedAt.Time
		}
		if err := json.Unmarshal([]byte(optionsRaw), &evt.Options); err != nil {
			log.Printf("[GetActiveBettingEventsDB] Error unmarshaling options for event %s: %v", evt.ID, err)
			continue
		}
		events = append(events, evt)
	}

	return events, nil
}

// GetBettingEventByID retrieves an event by its ID
func GetBettingEventByID(eventID string) (*DBBettingEvent, error) {
	query := `
		SELECT id, guild_id, channel_id, COALESCE(message_id, ''), creator_id, question, options, total_pool, status, COALESCE(winner_id, ''), end_time, created_at, resolved_at
		FROM betting_events
		WHERE id = $1
	`
	var (
		evt        DBBettingEvent
		optionsRaw string
		resolvedAt sql.NullTime
	)
	err := DB.QueryRow(query, eventID).Scan(
		&evt.ID, &evt.GuildID, &evt.ChannelID, &evt.MessageID,
		&evt.CreatorID, &evt.Question, &optionsRaw, &evt.TotalPool,
		&evt.Status, &evt.WinnerID, &evt.EndTime, &evt.CreatedAt, &resolvedAt,
	)
	if err != nil {
		return nil, err
	}
	if resolvedAt.Valid {
		evt.ResolvedAt = &resolvedAt.Time
	}
	if err := json.Unmarshal([]byte(optionsRaw), &evt.Options); err != nil {
		return nil, err
	}
	return &evt, nil
}

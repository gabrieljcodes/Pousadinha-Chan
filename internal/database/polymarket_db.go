package database

import (
	"database/sql"
	"fmt"
	"log"
	"time"
)

type DBPolymarketSettings struct {
	GuildID    string    `json:"guild_id"`
	ImportMode string    `json:"import_mode"` // "admin_only" or "all_users"
	ChannelID  string    `json:"channel_id"`
	MinBet     int       `json:"min_bet"`
	HouseEdge  float64   `json:"house_edge"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type DBPolymarketMarket struct {
	ID           string     `json:"id"`
	PolymarketID string     `json:"polymarket_id"`
	ConditionID  string     `json:"condition_id"`
	Slug         string     `json:"slug"`
	Question     string     `json:"question"`
	Description  string     `json:"description"`
	Category     string     `json:"category"`
	ImageURL     string     `json:"image_url"`
	YesPrice     float64    `json:"yes_price"`
	NoPrice      float64    `json:"no_price"`
	Status       string     `json:"status"` // "open", "closed", "resolved", "cancelled"
	Winner       string     `json:"winner"` // "Yes", "No", "cancelled"
	EndDate      *time.Time `json:"end_date"`
	GuildID      string     `json:"guild_id"`
	ChannelID    string     `json:"channel_id"`
	MessageID    string     `json:"message_id"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	ResolvedAt   *time.Time `json:"resolved_at"`
}

type DBPolymarketPosition struct {
	ID            int64     `json:"id"`
	MarketID      string    `json:"market_id"`
	UserID        string    `json:"user_id"`
	Outcome       string    `json:"outcome"` // "Yes" or "No"
	Shares        int64     `json:"shares"`
	TotalInvested int64     `json:"total_invested"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type DBPolymarketOrder struct {
	ID            int64     `json:"id"`
	MarketID      string    `json:"market_id"`
	UserID        string    `json:"user_id"`
	Side          string    `json:"side"`    // "buy" or "sell"
	Outcome       string    `json:"outcome"` // "Yes" or "No"
	Shares        int64     `json:"shares"`
	PricePerShare float64   `json:"price_per_share"`
	FeeAmount     int64     `json:"fee_amount"`
	TotalAmount   int64     `json:"total_amount"`
	CreatedAt     time.Time `json:"created_at"`
}

// GetGuildPolymarketSettings retrieves guild settings or creates defaults if missing
func GetGuildPolymarketSettings(guildID string) (*DBPolymarketSettings, error) {
	query := `
		SELECT guild_id, import_mode, channel_id, min_bet, house_edge, updated_at
		FROM polymarket_settings
		WHERE guild_id = $1
	`
	s := &DBPolymarketSettings{}
	err := DB.QueryRow(query, guildID).Scan(&s.GuildID, &s.ImportMode, &s.ChannelID, &s.MinBet, &s.HouseEdge, &s.UpdatedAt)
	if err == sql.ErrNoRows {
		// Insert default configuration
		insertQuery := `
			INSERT INTO polymarket_settings (guild_id, import_mode, channel_id, min_bet, house_edge)
			VALUES ($1, 'admin_only', '', 10, 0.0300)
			ON CONFLICT (guild_id) DO NOTHING
			RETURNING guild_id, import_mode, channel_id, min_bet, house_edge, updated_at
		`
		err = DB.QueryRow(insertQuery, guildID).Scan(&s.GuildID, &s.ImportMode, &s.ChannelID, &s.MinBet, &s.HouseEdge, &s.UpdatedAt)
		if err != nil {
			// Fallback in case of race condition
			return &DBPolymarketSettings{
				GuildID:    guildID,
				ImportMode: "admin_only",
				ChannelID:  "",
				MinBet:     10,
				HouseEdge:  0.03,
				UpdatedAt:  time.Now(),
			}, nil
		}
	} else if err != nil {
		return nil, fmt.Errorf("error querying polymarket settings: %w", err)
	}
	return s, nil
}

// UpdateGuildPolymarketSettings updates or upserts settings for a guild
func UpdateGuildPolymarketSettings(s *DBPolymarketSettings) error {
	query := `
		INSERT INTO polymarket_settings (guild_id, import_mode, channel_id, min_bet, house_edge, updated_at)
		VALUES ($1, $2, $3, $4, $5, NOW())
		ON CONFLICT (guild_id) DO UPDATE SET
			import_mode = EXCLUDED.import_mode,
			channel_id = EXCLUDED.channel_id,
			min_bet = EXCLUDED.min_bet,
			house_edge = EXCLUDED.house_edge,
			updated_at = NOW()
	`
	_, err := DB.Exec(query, s.GuildID, s.ImportMode, s.ChannelID, s.MinBet, s.HouseEdge)
	return err
}

// CreatePolymarketMarketDB inserts a new imported market into PostgreSQL
func CreatePolymarketMarketDB(m *DBPolymarketMarket) error {
	query := `
		INSERT INTO polymarket_markets (
			id, polymarket_id, condition_id, slug, question, description, category,
			image_url, yes_price, no_price, status, winner, end_date, guild_id,
			channel_id, message_id, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7,
			$8, $9, $10, $11, $12, $13, $14,
			$15, $16, NOW(), NOW()
		)
	`
	_, err := DB.Exec(query,
		m.ID, m.PolymarketID, m.ConditionID, m.Slug, m.Question, m.Description, m.Category,
		m.ImageURL, m.YesPrice, m.NoPrice, m.Status, m.Winner, m.EndDate, m.GuildID,
		m.ChannelID, m.MessageID,
	)
	return err
}

// GetPolymarketMarketByID retrieves a market by internal ID
func GetPolymarketMarketByID(id string) (*DBPolymarketMarket, error) {
	query := `
		SELECT id, polymarket_id, condition_id, slug, question, description, category,
		       image_url, yes_price, no_price, status, winner, end_date, guild_id,
		       channel_id, message_id, created_at, updated_at, resolved_at
		FROM polymarket_markets
		WHERE id = $1
	`
	return scanMarketRow(DB.QueryRow(query, id))
}

// GetPolymarketMarketByPolyID retrieves a market by external Polymarket ID
func GetPolymarketMarketByPolyID(polyID string) (*DBPolymarketMarket, error) {
	query := `
		SELECT id, polymarket_id, condition_id, slug, question, description, category,
		       image_url, yes_price, no_price, status, winner, end_date, guild_id,
		       channel_id, message_id, created_at, updated_at, resolved_at
		FROM polymarket_markets
		WHERE polymarket_id = $1
	`
	return scanMarketRow(DB.QueryRow(query, polyID))
}

// GetActivePolymarketMarketsDB returns all markets with status = 'open'
func GetActivePolymarketMarketsDB() ([]*DBPolymarketMarket, error) {
	query := `
		SELECT id, polymarket_id, condition_id, slug, question, description, category,
		       image_url, yes_price, no_price, status, winner, end_date, guild_id,
		       channel_id, message_id, created_at, updated_at, resolved_at
		FROM polymarket_markets
		WHERE status = 'open'
		ORDER BY created_at DESC
	`
	rows, err := DB.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var markets []*DBPolymarketMarket
	for rows.Next() {
		m, err := scanMarket(rows)
		if err != nil {
			log.Printf("[PolymarketDB] Error scanning market row: %v", err)
			continue
		}
		markets = append(markets, m)
	}
	return markets, nil
}

// UpdatePolymarketPricesDB updates latest prices and status
func UpdatePolymarketPricesDB(id string, yesPrice, noPrice float64, status string) error {
	query := `
		UPDATE polymarket_markets
		SET yes_price = $2, no_price = $3, status = $4, updated_at = NOW()
		WHERE id = $1
	`
	_, err := DB.Exec(query, id, yesPrice, noPrice, status)
	return err
}

// UpdatePolymarketMessageIDDB updates message and channel IDs for an imported market
func UpdatePolymarketMessageIDDB(id, channelID, messageID string) error {
	query := `
		UPDATE polymarket_markets
		SET channel_id = $2, message_id = $3, updated_at = NOW()
		WHERE id = $1
	`
	_, err := DB.Exec(query, id, channelID, messageID)
	return err
}

// ExecuteBuySharesTransaction atomic purchase of shares:
// Deducts user balance, creates/updates user position, and logs an order.
func ExecuteBuySharesTransaction(marketID, userID, outcome string, shares int64, pricePerShare float64, feeAmount, totalCost int64) error {
	tx, err := DB.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// 1. Check user balance and lock row
	var currentBalance int64
	err = tx.QueryRow(`SELECT balance FROM users WHERE id = $1 FOR UPDATE`, userID).Scan(&currentBalance)
	if err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("user not found")
		}
		return fmt.Errorf("failed to lock user balance: %w", err)
	}

	if currentBalance < totalCost {
		return fmt.Errorf("insufficient balance: have %d EC, need %d EC", currentBalance, totalCost)
	}

	// 2. Deduct balance
	_, err = tx.Exec(`UPDATE users SET balance = balance - $1 WHERE id = $2`, totalCost, userID)
	if err != nil {
		return fmt.Errorf("failed to deduct balance: %w", err)
	}

	// 3. Upsert position
	upsertPosition := `
		INSERT INTO polymarket_positions (market_id, user_id, outcome, shares, total_invested, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, NOW(), NOW())
		ON CONFLICT (market_id, user_id, outcome) DO UPDATE SET
			shares = polymarket_positions.shares + EXCLUDED.shares,
			total_invested = polymarket_positions.total_invested + EXCLUDED.total_invested,
			updated_at = NOW()
	`
	_, err = tx.Exec(upsertPosition, marketID, userID, outcome, shares, totalCost)
	if err != nil {
		return fmt.Errorf("failed to update position: %w", err)
	}

	// 4. Record order audit log
	insertOrder := `
		INSERT INTO polymarket_orders (market_id, user_id, side, outcome, shares, price_per_share, fee_amount, total_amount, created_at)
		VALUES ($1, $2, 'buy', $3, $4, $5, $6, $7, NOW())
	`
	_, err = tx.Exec(insertOrder, marketID, userID, outcome, shares, pricePerShare, feeAmount, totalCost)
	if err != nil {
		return fmt.Errorf("failed to record order: %w", err)
	}

	return tx.Commit()
}

// GetUserMarketPositions retrieves all positions a user holds on a specific market
func GetUserMarketPositions(userID, marketID string) ([]*DBPolymarketPosition, error) {
	query := `
		SELECT id, market_id, user_id, outcome, shares, total_invested, created_at, updated_at
		FROM polymarket_positions
		WHERE user_id = $1 AND market_id = $2 AND shares > 0
		ORDER BY outcome ASC
	`
	rows, err := DB.Query(query, userID, marketID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var positions []*DBPolymarketPosition
	for rows.Next() {
		p := &DBPolymarketPosition{}
		if err := rows.Scan(&p.ID, &p.MarketID, &p.UserID, &p.Outcome, &p.Shares, &p.TotalInvested, &p.CreatedAt, &p.UpdatedAt); err != nil {
			continue
		}
		positions = append(positions, p)
	}
	return positions, nil
}

// GetUserAllPositions retrieves all active positions for a user across all open markets
func GetUserAllPositions(userID string) ([]*DBPolymarketPosition, error) {
	query := `
		SELECT p.id, p.market_id, p.user_id, p.outcome, p.shares, p.total_invested, p.created_at, p.updated_at
		FROM polymarket_positions p
		JOIN polymarket_markets m ON m.id = p.market_id
		WHERE p.user_id = $1 AND p.shares > 0 AND m.status = 'open'
		ORDER BY p.updated_at DESC
	`
	rows, err := DB.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var positions []*DBPolymarketPosition
	for rows.Next() {
		p := &DBPolymarketPosition{}
		if err := rows.Scan(&p.ID, &p.MarketID, &p.UserID, &p.Outcome, &p.Shares, &p.TotalInvested, &p.CreatedAt, &p.UpdatedAt); err != nil {
			continue
		}
		positions = append(positions, p)
	}
	return positions, nil
}

// ResolvePolymarketMarketDB marks the market as resolved, calculates 100 EC per winning share,
// and atomically credits all winning users in a single transaction.
// Returns map[userID]payoutAmount for notification.
func ResolvePolymarketMarketDB(marketID, winner string) (map[string]int64, error) {
	tx, err := DB.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// 1. Mark market resolved
	updateMarket := `
		UPDATE polymarket_markets
		SET status = 'resolved', winner = $2, resolved_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND status != 'resolved'
	`
	res, err := tx.Exec(updateMarket, marketID, winner)
	if err != nil {
		return nil, fmt.Errorf("failed to update market status: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return nil, fmt.Errorf("market already resolved or not found")
	}

	// 2. Fetch all winning positions
	queryWinners := `
		SELECT user_id, shares
		FROM polymarket_positions
		WHERE market_id = $1 AND outcome = $2 AND shares > 0
	`
	rows, err := tx.Query(queryWinners, marketID, winner)
	if err != nil {
		return nil, fmt.Errorf("failed to query winning positions: %w", err)
	}
	defer rows.Close()

	payouts := make(map[string]int64)
	for rows.Next() {
		var userID string
		var shares int64
		if err := rows.Scan(&userID, &shares); err != nil {
			continue
		}
		// 1 share = 100 EC on resolution
		payouts[userID] = shares * 100
	}

	// 3. Credit winners
	for userID, payout := range payouts {
		_, err := tx.Exec(`UPDATE users SET balance = balance + $1 WHERE id = $2`, payout, userID)
		if err != nil {
			return nil, fmt.Errorf("failed to credit winner %s: %w", userID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit resolution: %w", err)
	}

	return payouts, nil
}

// CancelAndRefundPolymarketMarketDB marks the market as cancelled and refunds total_invested to all participants.
func CancelAndRefundPolymarketMarketDB(marketID string) (map[string]int64, error) {
	tx, err := DB.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	updateMarket := `
		UPDATE polymarket_markets
		SET status = 'cancelled', winner = 'cancelled', resolved_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND status != 'cancelled'
	`
	res, err := tx.Exec(updateMarket, marketID)
	if err != nil {
		return nil, fmt.Errorf("failed to update market status: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return nil, fmt.Errorf("market already cancelled or not found")
	}

	// Query all positions for refund
	queryPositions := `
		SELECT user_id, SUM(total_invested)
		FROM polymarket_positions
		WHERE market_id = $1 AND total_invested > 0
		GROUP BY user_id
	`
	rows, err := tx.Query(queryPositions, marketID)
	if err != nil {
		return nil, fmt.Errorf("failed to query positions for refund: %w", err)
	}
	defer rows.Close()

	refunds := make(map[string]int64)
	for rows.Next() {
		var userID string
		var refundAmount int64
		if err := rows.Scan(&userID, &refundAmount); err != nil {
			continue
		}
		refunds[userID] = refundAmount
	}

	// Credit refunds
	for userID, amount := range refunds {
		_, err := tx.Exec(`UPDATE users SET balance = balance + $1 WHERE id = $2`, amount, userID)
		if err != nil {
			return nil, fmt.Errorf("failed to refund user %s: %w", userID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit cancellation: %w", err)
	}

	return refunds, nil
}

func scanMarketRow(row *sql.Row) (*DBPolymarketMarket, error) {
	m := &DBPolymarketMarket{}
	err := row.Scan(
		&m.ID, &m.PolymarketID, &m.ConditionID, &m.Slug, &m.Question, &m.Description, &m.Category,
		&m.ImageURL, &m.YesPrice, &m.NoPrice, &m.Status, &m.Winner, &m.EndDate, &m.GuildID,
		&m.ChannelID, &m.MessageID, &m.CreatedAt, &m.UpdatedAt, &m.ResolvedAt,
	)
	if err != nil {
		return nil, err
	}
	return m, nil
}

func scanMarket(rows *sql.Rows) (*DBPolymarketMarket, error) {
	m := &DBPolymarketMarket{}
	err := rows.Scan(
		&m.ID, &m.PolymarketID, &m.ConditionID, &m.Slug, &m.Question, &m.Description, &m.Category,
		&m.ImageURL, &m.YesPrice, &m.NoPrice, &m.Status, &m.Winner, &m.EndDate, &m.GuildID,
		&m.ChannelID, &m.MessageID, &m.CreatedAt, &m.UpdatedAt, &m.ResolvedAt,
	)
	if err != nil {
		return nil, err
	}
	return m, nil
}

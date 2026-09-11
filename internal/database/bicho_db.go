package database

import (
	"database/sql"
	"fmt"
	"time"
)

type DBBichoSettings struct {
	GuildID    string    `json:"guild_id"`
	ChannelID  string    `json:"channel_id"`
	DrawHour   int       `json:"draw_hour"`
	DrawMinute int       `json:"draw_minute"`
	Enabled    bool      `json:"enabled"`
	MinBet     int64     `json:"min_bet"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type DBBichoRound struct {
	ID          int64      `json:"id"`
	GuildID     string     `json:"guild_id"`
	ChannelID   string     `json:"channel_id"`
	RoundNumber int        `json:"round_number"`
	Status      string     `json:"status"` // "open", "drawing", "completed", "cancelled"
	DrawTime    time.Time  `json:"draw_time"`
	MessageID   string     `json:"message_id"`
	Prize1      int        `json:"prize_1"`
	Prize2      int        `json:"prize_2"`
	Prize3      int        `json:"prize_3"`
	Prize4      int        `json:"prize_4"`
	Prize5      int        `json:"prize_5"`
	CreatedAt   time.Time  `json:"created_at"`
	DrawnAt     *time.Time `json:"drawn_at"`
}

type DBBichoBet struct {
	ID        int64     `json:"id"`
	RoundID   int64     `json:"round_id"`
	UserID    string    `json:"user_id"`
	GuildID   string    `json:"guild_id"`
	BetType   string    `json:"bet_type"` // "grupo", "dezena", "centena", "milhar", "duque", "terno"
	Scope     string    `json:"scope"`    // "cabeca", "cercado"
	Target    string    `json:"target"`
	Amount    int64     `json:"amount"`
	Payout    int64     `json:"payout"`
	Won       bool      `json:"won"`
	CreatedAt time.Time `json:"created_at"`
}

// GetBichoSettings retrieves guild settings or returns a default instance
func GetBichoSettings(guildID string) (*DBBichoSettings, error) {
	query := `
		SELECT guild_id, channel_id, draw_hour, draw_minute, enabled, min_bet, updated_at
		FROM bicho_settings
		WHERE guild_id = $1
	`
	row := DB.QueryRow(query, guildID)
	s := &DBBichoSettings{}
	err := row.Scan(&s.GuildID, &s.ChannelID, &s.DrawHour, &s.DrawMinute, &s.Enabled, &s.MinBet, &s.UpdatedAt)
	if err == sql.ErrNoRows {
		return &DBBichoSettings{
			GuildID:    guildID,
			ChannelID:  "",
			DrawHour:   20,
			DrawMinute: 0,
			Enabled:    true,
			MinBet:     10,
			UpdatedAt:  time.Now(),
		}, nil
	}
	if err != nil {
		return nil, err
	}
	return s, nil
}

// SaveBichoSettings inserts or updates guild settings
func SaveBichoSettings(s *DBBichoSettings) error {
	query := `
		INSERT INTO bicho_settings (guild_id, channel_id, draw_hour, draw_minute, enabled, min_bet, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW())
		ON CONFLICT (guild_id) DO UPDATE SET
			channel_id = EXCLUDED.channel_id,
			draw_hour = EXCLUDED.draw_hour,
			draw_minute = EXCLUDED.draw_minute,
			enabled = EXCLUDED.enabled,
			min_bet = EXCLUDED.min_bet,
			updated_at = NOW()
	`
	_, err := DB.Exec(query, s.GuildID, s.ChannelID, s.DrawHour, s.DrawMinute, s.Enabled, s.MinBet)
	return err
}

// GetAllActiveBichoGuilds returns all guilds with an active channel configured
func GetAllActiveBichoGuilds() ([]*DBBichoSettings, error) {
	query := `
		SELECT guild_id, channel_id, draw_hour, draw_minute, enabled, min_bet, updated_at
		FROM bicho_settings
		WHERE enabled = TRUE AND channel_id != ''
	`
	rows, err := DB.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []*DBBichoSettings
	for rows.Next() {
		s := &DBBichoSettings{}
		if err := rows.Scan(&s.GuildID, &s.ChannelID, &s.DrawHour, &s.DrawMinute, &s.Enabled, &s.MinBet, &s.UpdatedAt); err != nil {
			continue
		}
		list = append(list, s)
	}
	return list, nil
}

// GetActiveBichoRound finds the currently open round for a guild
func GetActiveBichoRound(guildID string) (*DBBichoRound, error) {
	query := `
		SELECT id, guild_id, channel_id, round_number, status, draw_time, message_id,
		       prize_1, prize_2, prize_3, prize_4, prize_5, created_at, drawn_at
		FROM bicho_rounds
		WHERE guild_id = $1 AND status = 'open'
		ORDER BY id DESC
		LIMIT 1
	`
	row := DB.QueryRow(query, guildID)
	r := &DBBichoRound{}
	err := row.Scan(&r.ID, &r.GuildID, &r.ChannelID, &r.RoundNumber, &r.Status, &r.DrawTime, &r.MessageID,
		&r.Prize1, &r.Prize2, &r.Prize3, &r.Prize4, &r.Prize5, &r.CreatedAt, &r.DrawnAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return r, nil
}

// GetBichoRoundByID gets a round by its ID
func GetBichoRoundByID(roundID int64) (*DBBichoRound, error) {
	query := `
		SELECT id, guild_id, channel_id, round_number, status, draw_time, message_id,
		       prize_1, prize_2, prize_3, prize_4, prize_5, created_at, drawn_at
		FROM bicho_rounds
		WHERE id = $1
	`
	row := DB.QueryRow(query, roundID)
	r := &DBBichoRound{}
	err := row.Scan(&r.ID, &r.GuildID, &r.ChannelID, &r.RoundNumber, &r.Status, &r.DrawTime, &r.MessageID,
		&r.Prize1, &r.Prize2, &r.Prize3, &r.Prize4, &r.Prize5, &r.CreatedAt, &r.DrawnAt)
	if err != nil {
		return nil, err
	}
	return r, nil
}

// GetLastCompletedBichoRound gets the most recent completed round for a guild
func GetLastCompletedBichoRound(guildID string) (*DBBichoRound, error) {
	query := `
		SELECT id, guild_id, channel_id, round_number, status, draw_time, message_id,
		       prize_1, prize_2, prize_3, prize_4, prize_5, created_at, drawn_at
		FROM bicho_rounds
		WHERE guild_id = $1 AND status = 'completed'
		ORDER BY id DESC
		LIMIT 1
	`
	row := DB.QueryRow(query, guildID)
	r := &DBBichoRound{}
	err := row.Scan(&r.ID, &r.GuildID, &r.ChannelID, &r.RoundNumber, &r.Status, &r.DrawTime, &r.MessageID,
		&r.Prize1, &r.Prize2, &r.Prize3, &r.Prize4, &r.Prize5, &r.CreatedAt, &r.DrawnAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return r, nil
}

// CreateBichoRound inserts a new round for a guild
func CreateBichoRound(guildID, channelID string, roundNumber int, drawTime time.Time) (*DBBichoRound, error) {
	query := `
		INSERT INTO bicho_rounds (guild_id, channel_id, round_number, status, draw_time, created_at)
		VALUES ($1, $2, $3, 'open', $4, NOW())
		RETURNING id, guild_id, channel_id, round_number, status, draw_time, message_id,
		          prize_1, prize_2, prize_3, prize_4, prize_5, created_at, drawn_at
	`
	row := DB.QueryRow(query, guildID, channelID, roundNumber, drawTime)
	r := &DBBichoRound{}
	err := row.Scan(&r.ID, &r.GuildID, &r.ChannelID, &r.RoundNumber, &r.Status, &r.DrawTime, &r.MessageID,
		&r.Prize1, &r.Prize2, &r.Prize3, &r.Prize4, &r.Prize5, &r.CreatedAt, &r.DrawnAt)
	if err != nil {
		return nil, err
	}
	return r, nil
}

// UpdateBichoRoundMessageID updates the announcement message ID of a round
func UpdateBichoRoundMessageID(roundID int64, messageID string) error {
	query := `UPDATE bicho_rounds SET message_id = $2 WHERE id = $1`
	_, err := DB.Exec(query, roundID, messageID)
	return err
}

// PlaceBichoBetDB atomically debits user balance and stores a new bet
func PlaceBichoBetDB(roundID int64, userID, guildID, betType, scope, target string, amount int64) error {
	tx, err := DB.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Ensure user and guild member exist
	_, _ = tx.Exec(`INSERT INTO users (id, balance) VALUES ($1, 0) ON CONFLICT (id) DO NOTHING`, userID)
	_, _ = tx.Exec(`INSERT INTO guild_members (guild_id, user_id, balance) VALUES ($1, $2, 0) ON CONFLICT (guild_id, user_id) DO NOTHING`, guildID, userID)

	// 1. Verify user balance and lock row
	var currentBalance int64
	err = tx.QueryRow(`SELECT balance FROM guild_members WHERE guild_id = $1 AND user_id = $2 FOR UPDATE`, guildID, userID).Scan(&currentBalance)
	if err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("user not found")
		}
		return fmt.Errorf("failed to lock balance: %w", err)
	}

	if currentBalance < amount {
		return fmt.Errorf("insufficient balance: you have %d EC, but the bet costs %d EC", currentBalance, amount)
	}

	// 2. Verify round is still open
	var roundStatus string
	err = tx.QueryRow(`SELECT status FROM bicho_rounds WHERE id = $1`, roundID).Scan(&roundStatus)
	if err != nil {
		return fmt.Errorf("round not found: %w", err)
	}
	if roundStatus != "open" {
		return fmt.Errorf("betting for this round has already closed")
	}

	// 3. Deduct user balance
	_, err = tx.Exec(`UPDATE guild_members SET balance = balance - $1, updated_at = NOW() WHERE guild_id = $2 AND user_id = $3`, amount, guildID, userID)
	if err != nil {
		return fmt.Errorf("failed to deduct balance: %w", err)
	}

	// 4. Insert bet record
	insertQuery := `
		INSERT INTO bicho_bets (round_id, user_id, guild_id, bet_type, scope, target, amount, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
	`
	_, err = tx.Exec(insertQuery, roundID, userID, guildID, betType, scope, target, amount)
	if err != nil {
		return fmt.Errorf("failed to insert bet: %w", err)
	}

	return tx.Commit()
}

// GetUserRoundBetsDB fetches all bets by a user in a specific round
func GetUserRoundBetsDB(roundID int64, userID string) ([]*DBBichoBet, error) {
	query := `
		SELECT id, round_id, user_id, guild_id, bet_type, scope, target, amount, payout, won, created_at
		FROM bicho_bets
		WHERE round_id = $1 AND user_id = $2
		ORDER BY id ASC
	`
	rows, err := DB.Query(query, roundID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var bets []*DBBichoBet
	for rows.Next() {
		b := &DBBichoBet{}
		if err := rows.Scan(&b.ID, &b.RoundID, &b.UserID, &b.GuildID, &b.BetType, &b.Scope, &b.Target, &b.Amount, &b.Payout, &b.Won, &b.CreatedAt); err != nil {
			continue
		}
		bets = append(bets, b)
	}
	return bets, nil
}

// GetRoundBetsDB fetches all bets placed in a round
func GetRoundBetsDB(roundID int64) ([]*DBBichoBet, error) {
	query := `
		SELECT id, round_id, user_id, guild_id, bet_type, scope, target, amount, payout, won, created_at
		FROM bicho_bets
		WHERE round_id = $1
		ORDER BY id ASC
	`
	rows, err := DB.Query(query, roundID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var bets []*DBBichoBet
	for rows.Next() {
		b := &DBBichoBet{}
		if err := rows.Scan(&b.ID, &b.RoundID, &b.UserID, &b.GuildID, &b.BetType, &b.Scope, &b.Target, &b.Amount, &b.Payout, &b.Won, &b.CreatedAt); err != nil {
			continue
		}
		bets = append(bets, b)
	}
	return bets, nil
}

// GetBichoRoundStats returns aggregated stats for an active round
func GetBichoRoundStats(roundID int64) (totalBets int, totalAmount int64, distinctUsers int, err error) {
	query := `
		SELECT COUNT(*), COALESCE(SUM(amount), 0), COUNT(DISTINCT user_id)
		FROM bicho_bets
		WHERE round_id = $1
	`
	err = DB.QueryRow(query, roundID).Scan(&totalBets, &totalAmount, &distinctUsers)
	return
}

// ResolveBichoRoundDB completes a round, saves prizes, updates winning bets, and credits user balances atomically
func ResolveBichoRoundDB(roundID int64, prizes [5]int, winningPayouts map[int64]int64, betUserMap map[int64]string) error {
	tx, err := DB.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// 1. Fetch guild_id and update round status
	var guildID string
	err = tx.QueryRow(`SELECT guild_id FROM bicho_rounds WHERE id = $1`, roundID).Scan(&guildID)
	if err != nil {
		return fmt.Errorf("failed to fetch round guild: %w", err)
	}

	updateRound := `
		UPDATE bicho_rounds
		SET status = 'completed',
		    prize_1 = $2, prize_2 = $3, prize_3 = $4, prize_4 = $5, prize_5 = $6,
		    drawn_at = NOW()
		WHERE id = $1 AND status != 'completed'
	`
	res, err := tx.Exec(updateRound, roundID, prizes[0], prizes[1], prizes[2], prizes[3], prizes[4])
	if err != nil {
		return fmt.Errorf("failed to update round status: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("round already completed or not found")
	}

	// 2. Process winning bets and credit users
	userCredits := make(map[string]int64)
	for betID, payout := range winningPayouts {
		if payout <= 0 {
			continue
		}
		// Update bet status
		_, err := tx.Exec(`UPDATE bicho_bets SET won = TRUE, payout = $2 WHERE id = $1`, betID, payout)
		if err != nil {
			return fmt.Errorf("failed to update bet %d: %w", betID, err)
		}

		userID, ok := betUserMap[betID]
		if ok && userID != "" {
			userCredits[userID] += payout
		}
	}

	// 3. Credit users in guild_members
	for userID, totalPayout := range userCredits {
		_, err := tx.Exec(`UPDATE guild_members SET balance = balance + $1, updated_at = NOW() WHERE guild_id = $2 AND user_id = $3`, totalPayout, guildID, userID)
		if err != nil {
			return fmt.Errorf("failed to credit user %s in guild %s: %w", userID, guildID, err)
		}
	}

	return tx.Commit()
}

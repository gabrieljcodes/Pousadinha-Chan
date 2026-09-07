package database

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"log"
	"time"

	"estudocoin/pkg/config"
)

// Initialize initializes the database based on configuration
func Initialize() {
	var err error

	log.Println("Initializing PostgreSQL database...")
	DB, err = NewPostgres(config.ConnString)

	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}

	if err := DB.Ping(); err != nil {
		log.Fatalf("Failed to ping database: %v", err)
	}

	log.Println("Database initialized successfully")
}



// NewPostgres creates and initializes a PostgreSQL database
func NewPostgres(connString string) (Database, error) {
	db := NewPostgresDatabase(connString)
	if err := db.Open(); err != nil {
		return nil, err
	}
	if err := db.CreateTables(); err != nil {
		return nil, err
	}
	return db, nil
}

// Helper functions to facilitate migration of existing queries

// prepareQuery converts a query with ? to PostgreSQL format ($1, $2, etc.)
func prepareQuery(query string) string {
	result := ""
	placeholderIndex := 1
	for i := 0; i < len(query); i++ {
		if query[i] == '?' {
			result += fmt.Sprintf("$%d", placeholderIndex)
			placeholderIndex++
		} else {
			result += string(query[i])
		}
	}
	return result
}

// GetBalance returns a user's balance with retry on error
func GetBalance(userID string) int {
	var balance int
	// Ensure user exists atomically
	_, _ = DB.Exec(`INSERT INTO users (id, balance) VALUES ($1, 0) ON CONFLICT (id) DO NOTHING`, userID)

	// Retry up to 3 times with a short delay on transient error
	for i := 0; i < 3; i++ {
		err := DB.QueryRow(`SELECT balance FROM users WHERE id = $1`, userID).Scan(&balance)
		if err == nil {
			return balance
		}
		log.Printf("[GetBalance] Error getting balance for %s: %v (attempt %d)", userID, err, i+1)
		time.Sleep(100 * time.Millisecond)
	}

	log.Printf("[GetBalance] Failed to get balance for %s after 3 attempts, returning 0", userID)
	return 0
}

// GetLeaderboard returns the balance leaderboard (excluding the bot and including investments)
// using a single aggregated SQL query instead of N+1 queries.
func GetLeaderboard(limit int) ([]UserBalance, error) {
	if limit <= 0 {
		limit = 10
	}

	query := `
		SELECT 
			u.id, 
			COALESCE(u.balance, 0) AS balance,
			COALESCE(s.stock_val, 0) AS stock_value,
			COALESCE(c.crypto_cnt, 0) AS crypto_value,
			(COALESCE(u.balance, 0) + COALESCE(s.stock_val, 0)) AS total_net_worth
		FROM users u
		LEFT JOIN (
			SELECT si.user_id, FLOOR(SUM(si.shares * COALESCE(sp.last_price, 0)))::BIGINT AS stock_val
			FROM stock_investments si
			LEFT JOIN stock_prices sp ON sp.ticker = si.ticker
			GROUP BY si.user_id
		) s ON s.user_id = u.id
		LEFT JOIN (
			SELECT ci.user_id, COUNT(*)::BIGINT AS crypto_cnt
			FROM crypto_investments ci
			WHERE ci.coins > 0
			GROUP BY ci.user_id
		) c ON c.user_id = u.id
		WHERE ($1 = '' OR u.id != $1)
		ORDER BY total_net_worth DESC
		LIMIT $2;`

	rows, err := DB.Query(query, BotUserID, limit)
	if err != nil {
		log.Printf("[LEADERBOARD ERROR] Query failed: %v", err)
		return nil, err
	}
	defer rows.Close()

	var users []UserBalance
	for rows.Next() {
		var u UserBalance
		if err := rows.Scan(&u.ID, &u.Balance, &u.StockValue, &u.CryptoValue, &u.TotalNetWorth); err != nil {
			log.Printf("[LEADERBOARD ERROR] Scan row failed: %v", err)
			continue
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return users, nil
}

// AddCoins adds coins to a user
func AddCoins(userID string, amount int) error {
	query := `INSERT INTO users (id, balance) VALUES ($1, $2) 
			  ON CONFLICT(id) DO UPDATE SET balance = users.balance + $2`
	_, err := DB.Exec(query, userID, amount)
	return err
}

// RemoveCoins removes coins from a user atomically, ensuring balance does not drop below zero
func RemoveCoins(userID string, amount int) error {
	if amount <= 0 {
		return nil
	}
	res, err := DB.Exec(`UPDATE users SET balance = balance - $1 WHERE id = $2 AND balance >= $1`, amount, userID)
	if err != nil {
		return err
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return sql.ErrNoRows // Insufficient funds or user not found
	}
	return nil
}

// BotUserID is the bot's user ID (must be set in main.go)
var BotUserID string

// CollectLostBet sends lost bet coins to the bot user profile
func CollectLostBet(userID string, amount int) error {
	if BotUserID == "" {
		// If bot ID is not set, simply remove coins from user
		return RemoveCoins(userID, amount)
	}
	
	// Transfer from user to bot
	return TransferCoins(userID, BotUserID, amount)
}

// TransferCoins transfers coins between users atomically and without deadlock risks
func TransferCoins(fromID, toID string, amount int) error {
	if fromID == toID || amount <= 0 {
		return fmt.Errorf("invalid transfer parameters")
	}

	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 1. Ensure receiver exists first so row can be locked
	_, err = tx.Exec(`INSERT INTO users (id, balance) VALUES ($1, 0) ON CONFLICT (id) DO NOTHING`, toID)
	if err != nil {
		return err
	}

	// 2. Lock both user rows in deterministic order (lexicographically) to eliminate deadlocks
	firstID, secondID := fromID, toID
	if firstID > secondID {
		firstID, secondID = secondID, firstID
	}

	rows, err := tx.Query(`SELECT id, balance FROM users WHERE id IN ($1, $2) ORDER BY id FOR UPDATE`, firstID, secondID)
	if err != nil {
		return err
	}
	defer rows.Close()

	balances := make(map[string]int)
	for rows.Next() {
		var id string
		var bal int
		if err := rows.Scan(&id, &bal); err != nil {
			return err
		}
		balances[id] = bal
	}
	if err := rows.Err(); err != nil {
		return err
	}

	fromBalance, exists := balances[fromID]
	if !exists || fromBalance < amount {
		return sql.ErrNoRows
	}

	// 3. Atomically update balances
	_, err = tx.Exec(`UPDATE users SET balance = balance - $1 WHERE id = $2`, amount, fromID)
	if err != nil {
		return err
	}

	_, err = tx.Exec(`UPDATE users SET balance = balance + $1 WHERE id = $2`, amount, toID)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// DailyStreakInfo contains information about a user's daily streak
type DailyStreakInfo struct {
	Streak      int
	MaxStreak   int
	Reward      int
	CanClaim    bool
	NextDaily   time.Time
	StreakReset bool
	IsNewRecord bool
}

// CalculateDailyReward calculates the daily reward based on current streak and config
func CalculateDailyReward(streak int) int {
	baseReward := config.Economy.DailyAmount
	if baseReward <= 0 {
		baseReward = 100
	}
	if streak <= 0 {
		streak = 1
	}
	// Cap scaling at day 50 (50 * baseReward)
	if streak > 50 {
		streak = 50
	}
	return streak * baseReward
}

// GetDailyStreakInfo returns full information about a user's daily streak
func GetDailyStreakInfo(userID string) *DailyStreakInfo {
	info := &DailyStreakInfo{
		Streak:    0,
		MaxStreak: 0,
		CanClaim:  true,
		NextDaily: time.Now(),
	}

	if DB == nil {
		info.Reward = CalculateDailyReward(1)
		return info
	}

	var lastDaily sql.NullTime
	var streak sql.NullInt64
	var maxStreak sql.NullInt64

	query := `SELECT last_daily, daily_streak, max_daily_streak FROM users WHERE id = $1`
	err := DB.QueryRow(query, userID).Scan(&lastDaily, &streak, &maxStreak)

	if err == nil {
		if streak.Valid {
			info.Streak = int(streak.Int64)
		}
		if maxStreak.Valid {
			info.MaxStreak = int(maxStreak.Int64)
		}
		if lastDaily.Valid {
			timeSince := time.Since(lastDaily.Time)
			info.CanClaim = timeSince >= 24*time.Hour
			info.NextDaily = lastDaily.Time.Add(24 * time.Hour)

			// Check if streak was lost (more than 48 hours since last claim)
			if timeSince > 48*time.Hour {
				info.Streak = 0
			}
		}
	}

	// Calculate reward based on next streak
	nextStreak := info.Streak + 1
	info.Reward = CalculateDailyReward(nextStreak)

	return info
}

// CanDaily checks if the user can claim their daily reward
func CanDaily(userID string) bool {
	return GetDailyStreakInfo(userID).CanClaim
}

// GetNextDailyTime returns when the next daily reward will be available
func GetNextDailyTime(userID string) time.Time {
	return GetDailyStreakInfo(userID).NextDaily
}

// GetDailyReward calculates the daily reward based on current streak
func GetDailyReward(userID string) int {
	return GetDailyStreakInfo(userID).Reward
}

// ClaimDaily atomically claims the daily reward, credits balance, and updates streak
func ClaimDaily(userID string) (*DailyStreakInfo, error) {
	if DB == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	tx, err := DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// Ensure user exists
	_, err = tx.Exec(`INSERT INTO users (id, balance, daily_streak, max_daily_streak) 
		VALUES ($1, 0, 0, 0) ON CONFLICT (id) DO NOTHING`, userID)
	if err != nil {
		return nil, err
	}

	// Lock the user row for update to prevent concurrent double-claim race conditions
	var lastDaily sql.NullTime
	var currentStreak int
	var maxStreak int

	err = tx.QueryRow(`SELECT last_daily, COALESCE(daily_streak, 0), COALESCE(max_daily_streak, 0) 
		FROM users WHERE id = $1 FOR UPDATE`, userID).Scan(&lastDaily, &currentStreak, &maxStreak)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	info := &DailyStreakInfo{
		Streak:    currentStreak,
		MaxStreak: maxStreak,
	}

	if lastDaily.Valid {
		timeSince := now.Sub(lastDaily.Time)
		if timeSince < 24*time.Hour {
			info.CanClaim = false
			info.NextDaily = lastDaily.Time.Add(24 * time.Hour)
			return info, fmt.Errorf("daily not available yet")
		}

		if timeSince <= 48*time.Hour && currentStreak > 0 {
			// Continue streak
			info.Streak = currentStreak + 1
		} else {
			// Expired: reset streak to 1
			info.Streak = 1
			if currentStreak > 0 {
				info.StreakReset = true
			}
		}
	} else {
		// First claim ever
		info.Streak = 1
	}

	// Update max streak
	if info.Streak > info.MaxStreak {
		if info.MaxStreak > 0 {
			info.IsNewRecord = true
		}
		info.MaxStreak = info.Streak
	}

	// Calculate reward for this streak
	info.Reward = CalculateDailyReward(info.Streak)
	info.CanClaim = false
	info.NextDaily = now.Add(24 * time.Hour)

	// Atomically add reward to balance and update daily streak/timestamps in single step
	_, err = tx.Exec(`UPDATE users 
		SET balance = balance + $1, 
		    last_daily = $2, 
		    daily_streak = $3, 
		    max_daily_streak = $4 
		WHERE id = $5`, info.Reward, now, info.Streak, info.MaxStreak, userID)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return info, nil
}

// HashAPIKey computes the SHA-256 hash of a raw API key
func HashAPIKey(key string) string {
	hash := sha256.Sum256([]byte(key))
	return fmt.Sprintf("%x", hash[:])
}

// CreateAPIKey creates a new API key storing its secure SHA-256 hash
func CreateAPIKey(rawKey, userID, name string) error {
	hashedKey := HashAPIKey(rawKey)
	query := `INSERT INTO api_keys (key, user_id, name, created_at) VALUES ($1, $2, $3, $4)`
	_, err := DB.Exec(query, hashedKey, userID, name, time.Now())
	return err
}

// GetUserByAPIKey returns the userID associated with an API key,
// supporting both secure SHA-256 hashes and legacy plaintext keys.
func GetUserByAPIKey(key string) (string, error) {
	hashedKey := HashAPIKey(key)
	var userID string
	query := `SELECT user_id FROM api_keys WHERE key = $1 OR key = $2 LIMIT 1`
	err := DB.QueryRow(query, hashedKey, key).Scan(&userID)
	if err != nil {
		return "", err
	}
	return userID, nil
}

// ListAPIKeys lists all API keys for a user
func ListAPIKeys(userID string) ([]APIKeyStruct, error) {
	query := `SELECT key, name, created_at FROM api_keys WHERE user_id = $1 ORDER BY created_at DESC`
	rows, err := DB.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []APIKeyStruct
	for rows.Next() {
		var k APIKeyStruct
		if err := rows.Scan(&k.Key, &k.Name, &k.CreatedAt); err != nil {
			continue
		}
		keys = append(keys, k)
	}
	return keys, nil
}

// DeleteAPIKey deletes an API key matching a prefix or full key
func DeleteAPIKey(userID, prefix string) error {
	hashedPrefix := HashAPIKey(prefix)
	query := `DELETE FROM api_keys WHERE user_id = $1 AND (key LIKE $2 OR key = $3)`
	_, err := DB.Exec(query, userID, prefix+"%", hashedPrefix)
	return err
}

// SetWebhook sets a user's webhook URL
func SetWebhook(userID, url string) error {
	query := `INSERT INTO users (id, balance, webhook_url) VALUES ($1, 0, $2) 
			  ON CONFLICT(id) DO UPDATE SET webhook_url = $2`
	_, err := DB.Exec(query, userID, url)
	return err
}

// GetWebhook returns a user's webhook URL
func GetWebhook(userID string) (string, error) {
	var url sql.NullString
	query := prepareQuery("SELECT webhook_url FROM users WHERE id = ?")
	err := DB.QueryRow(query, userID).Scan(&url)
	if err != nil {
		return "", err
	}
	return url.String, nil
}

// Loan represents a loan in the database
type Loan struct {
	ID           string
	LenderID     string
	BorrowerID   string
	Amount       int
	InterestRate float64
	DueDate      time.Time
	TotalOwed    int
	Paid         bool
	CreatedAt    time.Time
	ChannelID    string
	GuildID      string
}

// SaveLoan saves a new loan in the database
func SaveLoan(loan *Loan) error {
	query := `INSERT INTO loans (id, lender_id, borrower_id, amount, interest_rate, due_date, total_owed, paid, created_at, channel_id, guild_id) 
			  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`
	_, err := DB.Exec(query, loan.ID, loan.LenderID, loan.BorrowerID, loan.Amount, 
		loan.InterestRate, loan.DueDate, loan.TotalOwed, loan.Paid, loan.CreatedAt, loan.ChannelID, loan.GuildID)
	return err
}

// AcceptLoanAtomic transfers funds from lender to borrower and registers the loan in a single atomic transaction.
func AcceptLoanAtomic(loan *Loan) error {
	if loan == nil || loan.LenderID == loan.BorrowerID || loan.Amount <= 0 {
		return fmt.Errorf("invalid loan parameters")
	}

	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 1. Ensure borrower user row exists
	_, err = tx.Exec(`INSERT INTO users (id, balance) VALUES ($1, 0) ON CONFLICT (id) DO NOTHING`, loan.BorrowerID)
	if err != nil {
		return err
	}

	// 2. Lock and verify lender balance
	var lenderBalance int
	err = tx.QueryRow(`SELECT balance FROM users WHERE id = $1 FOR UPDATE`, loan.LenderID).Scan(&lenderBalance)
	if err != nil {
		return err
	}
	if lenderBalance < loan.Amount {
		return sql.ErrNoRows // Insufficient balance
	}

	// 3. Deduct from lender and credit borrower
	_, err = tx.Exec(`UPDATE users SET balance = balance - $1 WHERE id = $2`, loan.Amount, loan.LenderID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`UPDATE users SET balance = balance + $1 WHERE id = $2`, loan.Amount, loan.BorrowerID)
	if err != nil {
		return err
	}

	// 4. Insert loan record
	insertLoanSQL := `INSERT INTO loans (id, lender_id, borrower_id, amount, interest_rate, due_date, total_owed, paid, created_at, channel_id, guild_id) 
					  VALUES ($1, $2, $3, $4, $5, $6, $7, false, $8, $9, $10)`
	_, err = tx.Exec(insertLoanSQL, loan.ID, loan.LenderID, loan.BorrowerID, loan.Amount,
		loan.InterestRate, loan.DueDate, loan.TotalOwed, loan.CreatedAt, loan.ChannelID, loan.GuildID)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// PayLoanAtomic processes a loan payment atomically using row-level locking to prevent race conditions.
func PayLoanAtomic(loanID, payerID string) (*Loan, error) {
	tx, err := DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// 1. Lock the loan row
	loan := &Loan{}
	queryLoan := `SELECT id, lender_id, borrower_id, amount, interest_rate, due_date, total_owed, paid, created_at, channel_id, guild_id 
				  FROM loans WHERE id = $1 FOR UPDATE`
	err = tx.QueryRow(queryLoan, loanID).Scan(
		&loan.ID, &loan.LenderID, &loan.BorrowerID, &loan.Amount,
		&loan.InterestRate, &loan.DueDate, &loan.TotalOwed, &loan.Paid,
		&loan.CreatedAt, &loan.ChannelID, &loan.GuildID,
	)
	if err != nil {
		return nil, err
	}

	if loan.Paid {
		return nil, fmt.Errorf("loan is already paid")
	}

	if payerID != "" && loan.BorrowerID != payerID {
		return nil, fmt.Errorf("only the borrower can pay this loan")
	}

	// 2. Lock borrower row and verify balance
	var borrowerBalance int
	err = tx.QueryRow(`SELECT balance FROM users WHERE id = $1 FOR UPDATE`, loan.BorrowerID).Scan(&borrowerBalance)
	if err != nil {
		return nil, err
	}
	if borrowerBalance < loan.TotalOwed {
		return nil, fmt.Errorf("insufficient balance")
	}

	// 3. Deduct from borrower and credit lender
	_, err = tx.Exec(`UPDATE users SET balance = balance - $1 WHERE id = $2`, loan.TotalOwed, loan.BorrowerID)
	if err != nil {
		return nil, err
	}
	_, err = tx.Exec(`UPDATE users SET balance = balance + $1 WHERE id = $2`, loan.TotalOwed, loan.LenderID)
	if err != nil {
		return nil, err
	}

	// 4. Mark loan as paid
	_, err = tx.Exec(`UPDATE loans SET paid = TRUE WHERE id = $1`, loan.ID)
	if err != nil {
		return nil, err
	}

	loan.Paid = true
	return loan, tx.Commit()
}

// AutoCollectDueLoan processes an overdue loan. If the borrower has full funds, it repays and marks paid.
// If the borrower has partial funds, it collects available funds, reduces total_owed, and keeps the loan overdue.
// Returns: loan, collectedAmount, remainingDebt, fullyPaid, error.
func AutoCollectDueLoan(loanID string) (*Loan, int, int, bool, error) {
	tx, err := DB.Begin()
	if err != nil {
		return nil, 0, 0, false, err
	}
	defer tx.Rollback()

	// 1. Lock the loan row
	loan := &Loan{}
	queryLoan := `SELECT id, lender_id, borrower_id, amount, interest_rate, due_date, total_owed, paid, created_at, channel_id, guild_id 
				  FROM loans WHERE id = $1 FOR UPDATE SKIP LOCKED`
	err = tx.QueryRow(queryLoan, loanID).Scan(
		&loan.ID, &loan.LenderID, &loan.BorrowerID, &loan.Amount,
		&loan.InterestRate, &loan.DueDate, &loan.TotalOwed, &loan.Paid,
		&loan.CreatedAt, &loan.ChannelID, &loan.GuildID,
	)
	if err != nil {
		return nil, 0, 0, false, err
	}

	if loan.Paid {
		return loan, 0, 0, true, nil
	}

	// 2. Lock borrower balance
	var borrowerBalance int
	err = tx.QueryRow(`SELECT balance FROM users WHERE id = $1 FOR UPDATE`, loan.BorrowerID).Scan(&borrowerBalance)
	if err != nil {
		return nil, 0, 0, false, err
	}

	if borrowerBalance >= loan.TotalOwed {
		// Full collection
		collected := loan.TotalOwed
		_, err = tx.Exec(`UPDATE users SET balance = balance - $1 WHERE id = $2`, collected, loan.BorrowerID)
		if err != nil {
			return nil, 0, 0, false, err
		}
		_, err = tx.Exec(`UPDATE users SET balance = balance + $1 WHERE id = $2`, collected, loan.LenderID)
		if err != nil {
			return nil, 0, 0, false, err
		}
		_, err = tx.Exec(`UPDATE loans SET paid = TRUE WHERE id = $1`, loan.ID)
		if err != nil {
			return nil, 0, 0, false, err
		}
		loan.Paid = true
		return loan, collected, 0, true, tx.Commit()
	}

	// Partial collection (collect whatever is available > 0, do NOT negative balance)
	collected := borrowerBalance
	if collected > 0 {
		_, err = tx.Exec(`UPDATE users SET balance = balance - $1 WHERE id = $2`, collected, loan.BorrowerID)
		if err != nil {
			return nil, 0, 0, false, err
		}
		_, err = tx.Exec(`UPDATE users SET balance = balance + $1 WHERE id = $2`, collected, loan.LenderID)
		if err != nil {
			return nil, 0, 0, false, err
		}
		// Reduce debt
		_, err = tx.Exec(`UPDATE loans SET total_owed = total_owed - $1 WHERE id = $2`, collected, loan.ID)
		if err != nil {
			return nil, 0, 0, false, err
		}
		loan.TotalOwed -= collected
	}

	remaining := loan.TotalOwed
	return loan, collected, remaining, false, tx.Commit()
}

// GetActiveLoansByUser returns active unpaid loans for a user (as borrower or lender)
func GetActiveLoansByUser(userID string, limit int) ([]*Loan, error) {
	if limit <= 0 {
		limit = 15
	}
	query := `SELECT id, lender_id, borrower_id, amount, interest_rate, due_date, total_owed, paid, created_at, channel_id, guild_id 
			  FROM loans 
			  WHERE (borrower_id = $1 OR lender_id = $1) AND paid = FALSE 
			  ORDER BY due_date ASC 
			  LIMIT $2`
	rows, err := DB.Query(query, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var loans []*Loan
	for rows.Next() {
		loan := &Loan{}
		err := rows.Scan(&loan.ID, &loan.LenderID, &loan.BorrowerID, &loan.Amount,
			&loan.InterestRate, &loan.DueDate, &loan.TotalOwed, &loan.Paid,
			&loan.CreatedAt, &loan.ChannelID, &loan.GuildID)
		if err != nil {
			continue
		}
		loans = append(loans, loan)
	}
	return loans, nil
}

// GetActiveLoansByBorrower returns all active unpaid loans where the user is the borrower
func GetActiveLoansByBorrower(borrowerID string) ([]*Loan, error) {
	query := `SELECT id, lender_id, borrower_id, amount, interest_rate, due_date, total_owed, paid, created_at, channel_id, guild_id 
			  FROM loans 
			  WHERE borrower_id = $1 AND paid = FALSE 
			  ORDER BY due_date ASC`
	rows, err := DB.Query(query, borrowerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var loans []*Loan
	for rows.Next() {
		loan := &Loan{}
		err := rows.Scan(&loan.ID, &loan.LenderID, &loan.BorrowerID, &loan.Amount,
			&loan.InterestRate, &loan.DueDate, &loan.TotalOwed, &loan.Paid,
			&loan.CreatedAt, &loan.ChannelID, &loan.GuildID)
		if err != nil {
			continue
		}
		loans = append(loans, loan)
	}
	return loans, nil
}

// GetLoanByID returns a single loan by ID
func GetLoanByID(loanID string) (*Loan, error) {
	loan := &Loan{}
	query := `SELECT id, lender_id, borrower_id, amount, interest_rate, due_date, total_owed, paid, created_at, channel_id, guild_id 
			  FROM loans WHERE id = $1`
	err := DB.QueryRow(query, loanID).Scan(
		&loan.ID, &loan.LenderID, &loan.BorrowerID, &loan.Amount,
		&loan.InterestRate, &loan.DueDate, &loan.TotalOwed, &loan.Paid,
		&loan.CreatedAt, &loan.ChannelID, &loan.GuildID,
	)
	if err != nil {
		return nil, err
	}
	return loan, nil
}

// GetOverdueUnpaidLoans returns all unpaid loans that are past their due date
func GetOverdueUnpaidLoans() ([]*Loan, error) {
	query := `SELECT id, lender_id, borrower_id, amount, interest_rate, due_date, total_owed, paid, created_at, channel_id, guild_id 
			  FROM loans 
			  WHERE paid = FALSE AND due_date <= NOW() 
			  ORDER BY due_date ASC 
			  LIMIT 50`
	rows, err := DB.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var loans []*Loan
	for rows.Next() {
		loan := &Loan{}
		err := rows.Scan(&loan.ID, &loan.LenderID, &loan.BorrowerID, &loan.Amount,
			&loan.InterestRate, &loan.DueDate, &loan.TotalOwed, &loan.Paid,
			&loan.CreatedAt, &loan.ChannelID, &loan.GuildID)
		if err != nil {
			continue
		}
		loans = append(loans, loan)
	}
	return loans, nil
}

// MarkLoanAsPaid marks a loan as paid
func MarkLoanAsPaid(loanID string) error {
	query := `UPDATE loans SET paid = TRUE WHERE id = $1`
	_, err := DB.Exec(query, loanID)
	return err
}

// GetActiveLoans returns all active (unpaid) loans
func GetActiveLoans() ([]*Loan, error) {
	query := `SELECT id, lender_id, borrower_id, amount, interest_rate, due_date, total_owed, paid, created_at, channel_id, guild_id FROM loans WHERE paid = FALSE`
	rows, err := DB.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var loans []*Loan
	for rows.Next() {
		loan := &Loan{}
		err := rows.Scan(&loan.ID, &loan.LenderID, &loan.BorrowerID, &loan.Amount,
			&loan.InterestRate, &loan.DueDate, &loan.TotalOwed, &loan.Paid,
			&loan.CreatedAt, &loan.ChannelID, &loan.GuildID)
		if err != nil {
			continue
		}
		loans = append(loans, loan)
	}
	return loans, nil
}

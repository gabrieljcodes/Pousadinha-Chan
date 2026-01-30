package database

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"time"

	"estudocoin/pkg/config"
)

// Initialize inicializa o banco de dados baseado na configuração
func Initialize() {
	var err error

	switch config.DBType {
	case "postgres":
		log.Println("Initializing PostgreSQL database...")
		DB, err = NewPostgres(config.ConnString)
	case "sqlite":
		fallthrough
	default:
		log.Println("Initializing SQLite database...")
		DB, err = NewSQLite(config.ConnString)
	}

	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}

	if err := DB.Ping(); err != nil {
		log.Fatalf("Failed to ping database: %v", err)
	}

	log.Printf("Database initialized successfully (type: %s)", config.DBType)
}

// ShouldSkipTableCreation verifica se deve pular a criação de tabelas
func ShouldSkipTableCreation() bool {
	return os.Getenv("DB_SKIP_TABLE_CREATION") == "true"
}

// NewSQLite cria e inicializa um banco SQLite
func NewSQLite(connString string) (Database, error) {
	db := NewSQLiteDatabase(connString)
	if err := db.Open(); err != nil {
		return nil, err
	}
	if err := db.CreateTables(); err != nil {
		return nil, err
	}
	return db, nil
}

// NewPostgres cria e inicializa um banco PostgreSQL
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

// Helper functions para facilitar a migração das queries existentes

// prepareQuery converte uma query com ? para o formato correto do driver
func prepareQuery(query string) string {
	if config.DBType == "postgres" {
		// Converter ? para $1, $2, etc.
		return convertPlaceholders(query)
	}
	return query
}

// convertPlaceholders converte ? placeholders para $N (PostgreSQL)
func convertPlaceholders(query string) string {
	if config.DBType != "postgres" {
		return query
	}

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

// GetBalance retorna o saldo de um usuário com retry em caso de erro
func GetBalance(userID string) int {
	var balance int
	query := prepareQuery("SELECT balance FROM users WHERE id = ?")
	
	// Tentar até 3 vezes com pequeno delay
	for i := 0; i < 3; i++ {
		err := DB.QueryRow(query, userID).Scan(&balance)
		if err == nil {
			return balance
		}
		
		if err == sql.ErrNoRows {
			// Usuário não existe, criar com saldo 0
			_, insertErr := DB.Exec(prepareQuery("INSERT INTO users (id, balance) VALUES (?, 0)"), userID)
			if insertErr != nil {
				log.Printf("[GetBalance] Error inserting user %s: %v (attempt %d)", userID, insertErr, i+1)
				time.Sleep(100 * time.Millisecond)
				continue
			}
			return 0
		}
		
		// Outro erro - logar e tentar novamente
		log.Printf("[GetBalance] Error getting balance for %s: %v (attempt %d)", userID, err, i+1)
		time.Sleep(100 * time.Millisecond)
	}
	
	log.Printf("[GetBalance] Failed to get balance for %s after 3 attempts, returning 0", userID)
	return 0
}

// GetLeaderboard retorna o ranking de saldos (excluindo o bot e incluindo investimentos)
func GetLeaderboard(limit int) ([]UserBalance, error) {
	// Buscar todos os usuários (exceto o bot) com seus saldos
	var rows *sql.Rows
	var err error
	
	if BotUserID != "" {
		query := prepareQuery("SELECT id, balance FROM users WHERE id != ? ORDER BY balance DESC")
		rows, err = DB.Query(query, BotUserID)
	} else {
		query := prepareQuery("SELECT id, balance FROM users ORDER BY balance DESC")
		rows, err = DB.Query(query)
	}
	
	if err != nil {
		log.Printf("[LEADERBOARD ERROR] Query failed: %v", err)
		return nil, err
	}
	defer rows.Close()

	var users []UserBalance
	for rows.Next() {
		var u UserBalance
		if err := rows.Scan(&u.ID, &u.Balance); err != nil {
			continue
		}
		// Pular o bot se ainda estiver na lista
		if BotUserID != "" && u.ID == BotUserID {
			continue
		}
		
		// Calcular valor em ações
		stockValue := 0
		stockInvestments, _ := GetAllInvestmentsByUser(u.ID)
		for _, inv := range stockInvestments {
			price, _ := GetStockPriceDB(inv.Ticker)
			stockValue += int(inv.Shares * price)
		}
		u.StockValue = stockValue
		
		// Para crypto, vamos apenas contar o número de cryptos diferentes
		// (os preços de crypto são voláteis e buscados em tempo real da API)
		cryptoInvestments, _ := GetAllCryptoInvestmentsByUser(u.ID)
		cryptoCount := 0
		for _, inv := range cryptoInvestments {
			if inv.Coins > 0 {
				cryptoCount++
			}
		}
		u.CryptoValue = cryptoCount // Usamos para armazenar a contagem por enquanto
		
		// Patrimônio total (balance + stocks, crypto não incluído por ser volátil)
		u.TotalNetWorth = u.Balance + u.StockValue
		
		users = append(users, u)
	}
	
	// Ordenar por patrimônio total (bubble sort simples)
	for i := 0; i < len(users); i++ {
		for j := i + 1; j < len(users); j++ {
			if users[j].TotalNetWorth > users[i].TotalNetWorth {
				users[i], users[j] = users[j], users[i]
			}
		}
	}
	
	// Limitar resultados
	if len(users) > limit {
		users = users[:limit]
	}
	
	return users, nil
}

// AddCoins adiciona moedas a um usuário
func AddCoins(userID string, amount int) error {
	if config.DBType == "postgres" {
		// PostgreSQL usa sintaxe diferente para upsert
		query := `INSERT INTO users (id, balance) VALUES ($1, $2) 
				  ON CONFLICT(id) DO UPDATE SET balance = users.balance + $2`
		_, err := DB.Exec(query, userID, amount)
		return err
	}
	query := "INSERT INTO users (id, balance) VALUES (?, ?) ON CONFLICT(id) DO UPDATE SET balance = balance + ?"
	_, err := DB.Exec(query, userID, amount, amount)
	return err
}

// RemoveCoins remove moedas de um usuário
func RemoveCoins(userID string, amount int) error {
	current := GetBalance(userID)
	if current < amount {
		return sql.ErrNoRows
	}
	query := prepareQuery("UPDATE users SET balance = balance - ? WHERE id = ?")
	_, err := DB.Exec(query, amount, userID)
	return err
}

// BotUserID é o ID do bot (deve ser definido no main.go)
var BotUserID string

// CollectLostBet envia o dinheiro perdido em apostas para o perfil do bot
func CollectLostBet(userID string, amount int) error {
	if BotUserID == "" {
		// Se o ID do bot não estiver definido, apenas remove as moedas do usuário
		return RemoveCoins(userID, amount)
	}
	
	// Transfere do usuário para o bot
	return TransferCoins(userID, BotUserID, amount)
}

// TransferCoins transfere moedas entre usuários
func TransferCoins(fromID, toID string, amount int) error {
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var fromBalance int
	err = tx.QueryRow(prepareQuery("SELECT balance FROM users WHERE id = ?"), fromID).Scan(&fromBalance)
	if err != nil {
		return err
	}

	if fromBalance < amount {
		return sql.ErrNoRows
	}

	_, err = tx.Exec(prepareQuery("UPDATE users SET balance = balance - ? WHERE id = ?"), amount, fromID)
	if err != nil {
		return err
	}

	if config.DBType == "postgres" {
		_, err = tx.Exec(`INSERT INTO users (id, balance) VALUES ($1, $2) 
						  ON CONFLICT(id) DO UPDATE SET balance = users.balance + $2`,
			toID, amount)
	} else {
		_, err = tx.Exec("INSERT INTO users (id, balance) VALUES (?, ?) ON CONFLICT(id) DO UPDATE SET balance = balance + ?",
			toID, amount, amount)
	}
	if err != nil {
		return err
	}

	return tx.Commit()
}

// DailyStreakInfo contém informações sobre a streak de daily do usuário
type DailyStreakInfo struct {
	Streak     int
	MaxStreak  int
	Reward     int
	CanClaim   bool
	NextDaily  time.Time
}

// GetDailyStreakInfo retorna informações completas sobre o daily do usuário
func GetDailyStreakInfo(userID string) *DailyStreakInfo {
	info := &DailyStreakInfo{
		Streak:    0,
		MaxStreak: 0,
		Reward:    100,
		CanClaim:  true,
		NextDaily: time.Now(),
	}

	var lastDaily sql.NullTime
	var streak sql.NullInt64
	var maxStreak sql.NullInt64

	query := prepareQuery("SELECT last_daily, daily_streak, max_daily_streak FROM users WHERE id = ?")
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

			// Verifica se perdeu a streak (mais de 48 horas desde o último claim)
			if timeSince > 48*time.Hour {
				info.Streak = 0
			}
		}
	}

	// Calcula a recompensa baseada na streak
	// Streak 0 = 100, Streak 1 = 200, ... até max 5000
	info.Reward = (info.Streak + 1) * 100
	if info.Reward > 5000 {
		info.Reward = 5000
	}

	return info
}

// CanDaily verifica se o usuário pode coletar o daily
func CanDaily(userID string) bool {
	return GetDailyStreakInfo(userID).CanClaim
}

// GetNextDailyTime retorna quando o próximo daily estará disponível
func GetNextDailyTime(userID string) time.Time {
	return GetDailyStreakInfo(userID).NextDaily
}

// GetDailyReward calcula a recompensa do daily baseada na streak atual
func GetDailyReward(userID string) int {
	return GetDailyStreakInfo(userID).Reward
}

// ClaimDaily coleta o daily e atualiza a streak
func ClaimDaily(userID string) (*DailyStreakInfo, error) {
	info := GetDailyStreakInfo(userID)

	if !info.CanClaim {
		return info, fmt.Errorf("daily not available yet")
	}

	now := time.Now()

	// Verifica se a streak continua (coletou entre 24h e 48h atrás)
	timeSinceLast := time.Since(info.NextDaily.Add(-24 * time.Hour))
	if timeSinceLast >= 0 && timeSinceLast <= 48*time.Hour {
		// Continua a streak
		info.Streak++
	} else {
		// Reseta a streak
		info.Streak = 0
	}

	// Atualiza max streak se necessário
	if info.Streak > info.MaxStreak {
		info.MaxStreak = info.Streak
	}

	// Recalcula a recompensa
	info.Reward = (info.Streak + 1) * 100
	if info.Reward > 5000 {
		info.Reward = 5000
	}

	// Atualiza no banco de dados
	if config.DBType == "postgres" {
		query := `INSERT INTO users (id, balance, last_daily, daily_streak, max_daily_streak) 
				  VALUES ($1, $2, $3, $4, $5) 
				  ON CONFLICT(id) DO UPDATE 
				  SET last_daily = $3, daily_streak = $4, max_daily_streak = $5`
		_, err := DB.Exec(query, userID, info.Reward, now, info.Streak, info.MaxStreak)
		if err != nil {
			return info, err
		}
	} else {
		query := `INSERT INTO users (id, balance, last_daily, daily_streak, max_daily_streak) 
				  VALUES (?, ?, ?, ?, ?) 
				  ON CONFLICT(id) DO UPDATE 
				  SET last_daily = ?, daily_streak = ?, max_daily_streak = ?`
		_, err := DB.Exec(query, userID, info.Reward, now, info.Streak, info.MaxStreak, now, info.Streak, info.MaxStreak)
		if err != nil {
			return info, err
		}
	}

	return info, nil
}

// CreateAPIKey cria uma nova chave de API
func CreateAPIKey(key, userID, name string) error {
	query := prepareQuery("INSERT INTO api_keys (key, user_id, name, created_at) VALUES (?, ?, ?, ?)")
	_, err := DB.Exec(query, key, userID, name, time.Now())
	return err
}

// GetUserByAPIKey retorna o userID de uma chave de API
func GetUserByAPIKey(key string) (string, error) {
	var userID string
	query := prepareQuery("SELECT user_id FROM api_keys WHERE key = ?")
	err := DB.QueryRow(query, key).Scan(&userID)
	if err != nil {
		return "", err
	}
	return userID, nil
}

// ListAPIKeys lista todas as chaves de API de um usuário
func ListAPIKeys(userID string) ([]APIKeyStruct, error) {
	query := prepareQuery("SELECT key, name, created_at FROM api_keys WHERE user_id = ?")
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

// DeleteAPIKey deleta uma chave de API
func DeleteAPIKey(userID, prefix string) error {
	if config.DBType == "postgres" {
		query := prepareQuery("DELETE FROM api_keys WHERE user_id = ? AND key LIKE ?")
		_, err := DB.Exec(query, userID, prefix+"%")
		return err
	}
	query := prepareQuery("DELETE FROM api_keys WHERE user_id = ? AND key LIKE ?")
	_, err := DB.Exec(query, userID, prefix+"%")
	return err
}

// SetWebhook define a URL de webhook de um usuário
func SetWebhook(userID, url string) error {
	if config.DBType == "postgres" {
		query := `INSERT INTO users (id, balance, webhook_url) VALUES ($1, 0, $2) 
				  ON CONFLICT(id) DO UPDATE SET webhook_url = $2`
		_, err := DB.Exec(query, userID, url)
		return err
	}
	query := "INSERT INTO users (id, balance, webhook_url) VALUES (?, 0, ?) ON CONFLICT(id) DO UPDATE SET webhook_url = ?"
	_, err := DB.Exec(query, userID, url, url)
	return err
}

// GetWebhook retorna a URL de webhook de um usuário
func GetWebhook(userID string) (string, error) {
	var url sql.NullString
	query := prepareQuery("SELECT webhook_url FROM users WHERE id = ?")
	err := DB.QueryRow(query, userID).Scan(&url)
	if err != nil {
		return "", err
	}
	return url.String, nil
}

// Loan representa um empréstimo no banco de dados
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

// SaveLoan salva um novo empréstimo no banco de dados
func SaveLoan(loan *Loan) error {
	if config.DBType == "postgres" {
		query := `INSERT INTO loans (id, lender_id, borrower_id, amount, interest_rate, due_date, total_owed, paid, created_at, channel_id, guild_id) 
				  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`
		_, err := DB.Exec(query, loan.ID, loan.LenderID, loan.BorrowerID, loan.Amount, 
			loan.InterestRate, loan.DueDate, loan.TotalOwed, loan.Paid, loan.CreatedAt, loan.ChannelID, loan.GuildID)
		return err
	}
	query := `INSERT INTO loans (id, lender_id, borrower_id, amount, interest_rate, due_date, total_owed, paid, created_at, channel_id, guild_id) 
			  VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := DB.Exec(query, loan.ID, loan.LenderID, loan.BorrowerID, loan.Amount,
		loan.InterestRate, loan.DueDate, loan.TotalOwed, loan.Paid, loan.CreatedAt, loan.ChannelID, loan.GuildID)
	return err
}

// MarkLoanAsPaid marca um empréstimo como pago
func MarkLoanAsPaid(loanID string) error {
	query := prepareQuery("UPDATE loans SET paid = ? WHERE id = ?")
	_, err := DB.Exec(query, true, loanID)
	return err
}

// GetActiveLoans retorna todos os empréstimos ativos (não pagos)
func GetActiveLoans() ([]*Loan, error) {
	query := prepareQuery("SELECT id, lender_id, borrower_id, amount, interest_rate, due_date, total_owed, paid, created_at, channel_id, guild_id FROM loans WHERE paid = ?")
	rows, err := DB.Query(query, false)
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

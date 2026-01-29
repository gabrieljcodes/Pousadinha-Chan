package config

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
)

type EconomyConfig struct {
	DailyAmount              int     `json:"daily_amount"`
	VoiceCoinsPerMinute      int     `json:"voice_coins_per_minute"`
	CostNicknameSelf         int     `json:"cost_nickname_self"`
	CostNicknameOther        int     `json:"cost_nickname_other"`
	CostPerMinutePunishment  int     `json:"cost_per_minute_punishment"`
	CostPerMinuteMute        int     `json:"cost_per_minute_mute"`
	StockPriceMultiplier     float64 `json:"stock_price_multiplier"`
	RouletteIntervalMinutes  int     `json:"roulette_interval_minutes"`
	RouletteChannelID        string  `json:"roulette_channel_id"`
}

type DatabaseConfig struct {
	Type string `json:"type"` // "sqlite" ou "postgres"
}

type GeneralConfig struct {
	BotName         string         `json:"bot_name"`
	CurrencyName    string         `json:"currency_name"`
	CurrencySymbol  string         `json:"currency_symbol"`
	EnableAPI       bool           `json:"enable_api"`
	ApiPort         string         `json:"api_port"`
	AllowedChannels []string       `json:"allowed_channels"`
	Database        DatabaseConfig `json:"database"`
}

var (
	Economy    EconomyConfig
	Bot        GeneralConfig
	DBType     string
	ConnString string
)

func Load() {
	loadJSON("economy.json", &Economy)
	loadJSON("config.json", &Bot)
	
	// Configurar database defaults
	setupDatabaseConfig()
}

func setupDatabaseConfig() {
	// DB_TYPE do .env sobrescreve o config.json
	DBType = os.Getenv("DB_TYPE")
	if DBType == "" {
		DBType = Bot.Database.Type
	}
	if DBType == "" {
		DBType = "sqlite"
	}

	switch DBType {
	case "postgres":
		ConnString = buildPostgresConnectionString()
	case "sqlite":
		fallthrough
	default:
		// Caminho do SQLite vem do .env ou usa default
		ConnString = os.Getenv("SQLITE_PATH")
		if ConnString == "" {
			ConnString = "./pousadinha-chan.db"
		}
		DBType = "sqlite"
	}
}

func buildPostgresConnectionString() string {
	// Para Supabase, usar a DATABASE_URL completa se disponível (funciona com pgx)
	if dbURL := os.Getenv("DATABASE_URL"); dbURL != "" {
		log.Println("Using DATABASE_URL from environment")
		// pgx funciona bem com o pooler do Supabase, sem necessidade de parâmetros extras
		return dbURL
	}

	// Caso contrário, construir a string de conexão a partir das variáveis individuais
	host := os.Getenv("DB_HOST")
	if host == "" {
		log.Fatal("DB_HOST is required for PostgreSQL. Set it in .env file or use DATABASE_URL")
	}

	portStr := os.Getenv("DB_PORT")
	port := 5432
	if portStr != "" {
		if p, err := strconv.Atoi(portStr); err == nil {
			port = p
		}
	}

	user := os.Getenv("DB_USER")
	if user == "" {
		log.Fatal("DB_USER is required for PostgreSQL. Set it in .env file")
	}

	password := os.Getenv("DB_PASSWORD")
	if password == "" {
		log.Fatal("DB_PASSWORD is required for PostgreSQL. Set it in .env file")
	}

	dbname := os.Getenv("DB_NAME")
	if dbname == "" {
		dbname = "postgres"
	}

	sslmode := os.Getenv("DB_SSLMODE")
	if sslmode == "" {
		sslmode = "require"
	}

	return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		host, port, user, password, dbname, sslmode)
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func loadJSON(filename string, target interface{}) {
	file, err := os.ReadFile(filename)
	if err != nil {
		log.Fatalf("Error reading %s: %v", filename, err)
	}

	err = json.Unmarshal(file, target)
	if err != nil {
		log.Fatalf("Error parsing %s: %v", filename, err)
	}
}

// IsChannelAllowed checks if a channel ID is in the allowed channels list
// Returns true if the list is empty (all channels allowed) or if the channel is in the list
func (c *GeneralConfig) IsChannelAllowed(channelID string) bool {
	if len(c.AllowedChannels) == 0 {
		return true
	}
	for _, id := range c.AllowedChannels {
		if id == channelID {
			return true
		}
	}
	return false
}

// GetAdjustedStockPrice applies the multiplier to a real stock price
func (e *EconomyConfig) GetAdjustedStockPrice(realPrice float64) float64 {
	multiplier := e.StockPriceMultiplier
	if multiplier <= 0 {
		multiplier = 1
	}
	return realPrice * multiplier
}
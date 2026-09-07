package config

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
)

type EconomyConfig struct {
	DailyAmount             int     `json:"daily_amount"`
	VoiceCoinsPerMinute     int     `json:"voice_coins_per_minute"`
	CostNicknameSelf        int     `json:"cost_nickname_self"`
	CostNicknameOther       int     `json:"cost_nickname_other"`
	CostPerMinutePunishment int     `json:"cost_per_minute_punishment"`
	CostPerMinuteMute       int     `json:"cost_per_minute_mute"`
	StockPriceMultiplier    float64 `json:"stock_price_multiplier"`
	RouletteEnabled         bool    `json:"roulette_enabled"`
	RouletteIntervalMinutes int     `json:"roulette_interval_minutes"`
}

type DatabaseConfig struct {
	Type string `json:"type"` // "postgres"
}

type GeneralConfig struct {
	BotName           string         `json:"bot_name"`
	CurrencyName      string         `json:"currency_name"`
	CurrencySymbol    string         `json:"currency_symbol"`
	EnableAPI         bool           `json:"enable_api"`
	ApiPort           string         `json:"api_port"`
	AllowedChannels   []string       `json:"allowed_channels"`
	RouletteChannelID string         `json:"roulette_channel_id"`
	Database          DatabaseConfig `json:"database"`
}

var (
	Economy    EconomyConfig
	Bot        GeneralConfig
	ConnString string
)

func Load() {
	loadJSON("economy.json", &Economy)
	loadJSON("config.json", &Bot)

	// Environment variable overrides
	if apiPort := os.Getenv("API_PORT"); apiPort != "" {
		Bot.ApiPort = apiPort
	}
	if enableAPI := os.Getenv("ENABLE_API"); enableAPI != "" {
		if val, err := strconv.ParseBool(enableAPI); err == nil {
			Bot.EnableAPI = val
		}
	}

	// Configurar database defaults
	setupDatabaseConfig()
}

func setupDatabaseConfig() {
	ConnString = buildPostgresConnectionString()
}

func buildPostgresConnectionString() string {
	// For Supabase or full database connection string
	if dbURL := os.Getenv("DATABASE_URL"); dbURL != "" {
		log.Println("Using DATABASE_URL from environment")
		// Clean up any legacy or invalid parameters
		dbURL = strings.ReplaceAll(dbURL, "statement_cache_mode=describe", "")
		dbURL = strings.TrimSuffix(dbURL, "?")
		dbURL = strings.TrimSuffix(dbURL, "&")

		// Add pgx parameters to disable prepared statement caching when using poolers (port 6543 / PgBouncer)
		sep := "?"
		if strings.Contains(dbURL, "?") {
			sep = "&"
		}
		if !strings.Contains(dbURL, "default_query_exec_mode") {
			dbURL = dbURL + sep + "default_query_exec_mode=exec"
			sep = "&"
		}
		if !strings.Contains(dbURL, "statement_cache_capacity") {
			dbURL = dbURL + sep + "statement_cache_capacity=0"
		}
		return dbURL
	}

	// Otherwise, construct connection string from individual variables
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
		if filename == "config.json" {
			if altFile, altErr := os.ReadFile("config.example.json"); altErr == nil {
				log.Printf("Notice: %s not found, falling back to config.example.json", filename)
				file = altFile
				err = nil
			}
		}
		if err != nil {
			log.Fatalf("Error reading %s: %v", filename, err)
		}
	}

	err = json.Unmarshal(file, target)
	if err != nil {
		log.Fatalf("Error parsing %s: %v", filename, err)
	}

	log.Printf("Loaded config from %s", filename)
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
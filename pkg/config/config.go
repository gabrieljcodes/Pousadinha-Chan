package config

import (
	"encoding/json"
	"log"
	"os"
)

type EconomyConfig struct {
	DailyAmount             int     `json:"daily_amount"`
	VoiceCoinsPerMinute     int     `json:"voice_coins_per_minute"`
	CostNicknameSelf        int     `json:"cost_nickname_self"`
	CostNicknameOther       int     `json:"cost_nickname_other"`
	CostPerMinuteMute       int     `json:"cost_per_minute_mute"`
	StockPriceMultiplier    float64 `json:"stock_price_multiplier"`
	RouletteIntervalMinutes int     `json:"roulette_interval_minutes"`
	RouletteChannelID       string  `json:"roulette_channel_id"`
}

type GeneralConfig struct {
	BotName         string   `json:"bot_name"`
	CurrencyName    string   `json:"currency_name"`
	CurrencySymbol  string   `json:"currency_symbol"`
	EnableAPI       bool     `json:"enable_api"`
	ApiPort         string   `json:"api_port"`
	AllowedChannels []string `json:"allowed_channels"`
}

var (
	Economy EconomyConfig
	Bot     GeneralConfig
)

func Load() {
	loadJSON("economy.json", &Economy)
	loadJSON("config.json", &Bot)
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
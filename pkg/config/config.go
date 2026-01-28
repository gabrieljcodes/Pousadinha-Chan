package config

import (
	"encoding/json"
	"log"
	"os"
)

type EconomyConfig struct {
	DailyAmount         int `json:"daily_amount"`
	VoiceCoinsPerMinute int `json:"voice_coins_per_minute"`
	CostNicknameSelf    int `json:"cost_nickname_self"`
	CostNicknameOther   int `json:"cost_nickname_other"`
	CostPerMinuteMute   int `json:"cost_per_minute_mute"`
}

type GeneralConfig struct {
	BotName        string `json:"bot_name"`
	CurrencyName   string `json:"currency_name"`
	CurrencySymbol string `json:"currency_symbol"`
	EnableAPI      bool   `json:"enable_api"`
	ApiPort        string `json:"api_port"`
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
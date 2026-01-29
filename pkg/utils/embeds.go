package utils

import (
	"bytes"
	"encoding/json"
	"estudocoin/internal/database"
	"net/http"
	"time"

	"github.com/bwmarrin/discordgo"
)

const (
	ColorGold  = 0xFFD700
	ColorGreen = 0x00FF00
	ColorRed   = 0xFF0000
	ColorBlue  = 0x0000FF
)

func NewEmbed() *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{}
}

func ErrorEmbed(description string) *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{
		Title:       "❌ Error",
		Description: description,
		Color:       ColorRed,
	}
}

func SuccessEmbed(title, description string) *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{
		Title:       "✅ " + title,
		Description: description,
		Color:       ColorGreen,
	}
}

func InfoEmbed(title, description string) *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{
		Title:       "ℹ️ " + title,
		Description: description,
		Color:       ColorBlue,
	}
}

func GoldEmbed(title, description string) *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{
		Title:       "💰 " + title,
		Description: description,
		Color:       ColorGold,
	}
}

// SendWebhookNotification sends a simple message notification to user's webhook
func SendWebhookNotification(userID string, message string) {
	url, err := database.GetWebhook(userID)
	if err != nil || url == "" {
		return // No webhook configured
	}

	payload := map[string]string{
		"content": message,
	}

	go func(targetURL string, p map[string]string) {
		jsonBytes, _ := json.Marshal(p)
		client := http.Client{Timeout: 5 * time.Second}
		client.Post(targetURL, "application/json", bytes.NewBuffer(jsonBytes))
	}(url, payload)
}
package utils

import "github.com/bwmarrin/discordgo"

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
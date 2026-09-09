package commands

import (
	"bot/internal/database"
	"bot/internal/webhook"
	"bot/pkg/utils"

	"github.com/bwmarrin/discordgo"
)

func HandleSlashWebhook(s *discordgo.Session, i *discordgo.InteractionCreate) {
	options := i.ApplicationCommandData().Options
	if len(options) == 0 {
		return
	}
	subCommand := options[0].Name
	userID := ""
	if i.Member != nil && i.Member.User != nil {
		userID = i.Member.User.ID
	} else if i.User != nil {
		userID = i.User.ID
	}

	switch subCommand {
	case "set":
		rawURL := options[0].Options[0].StringValue()

		// Validate URL with SSRF protection
		if err := webhook.ValidateWebhookURL(rawURL); err != nil {
			respondEmbed(s, i, utils.ErrorEmbed("Invalid Webhook URL: "+err.Error()))
			return
		}

		if err := database.SetWebhook(userID, rawURL); err != nil {
			respondEmbed(s, i, utils.ErrorEmbed("Database error saving webhook."))
			return
		}

		respondEmbed(s, i, utils.SuccessEmbed("Webhook Configured", "Your webhook URL has been saved."))

	case "test":
		targetURL, err := database.GetWebhook(userID)
		if err != nil || targetURL == "" {
			respondEmbed(s, i, utils.ErrorEmbed("You don't have a webhook configured."))
			return
		}

		err = webhook.TestWebhook(targetURL)
		if err != nil {
			respondEmbed(s, i, utils.ErrorEmbed("Test Failed: "+err.Error()))
			return
		}

		respondEmbed(s, i, utils.SuccessEmbed("Test Sent", "We sent a test payload to your URL."))

	case "delete":
		err := database.SetWebhook(userID, "") // Setting empty removes it effectively
		if err != nil {
			respondEmbed(s, i, utils.ErrorEmbed("Error removing webhook."))
			return
		}
		respondEmbed(s, i, utils.SuccessEmbed("Webhook Removed", "You will no longer receive notifications."))
	}
}

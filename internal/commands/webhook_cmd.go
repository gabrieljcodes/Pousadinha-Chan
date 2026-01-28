package commands

import (
	"estudocoin/internal/database"
	"estudocoin/internal/webhook"
	"estudocoin/pkg/utils"
	"net/url"

	"github.com/bwmarrin/discordgo"
)

func HandleSlashWebhook(s *discordgo.Session, i *discordgo.InteractionCreate) {
	options := i.ApplicationCommandData().Options
	subCommand := options[0].Name
	userID := i.Member.User.ID

	switch subCommand {
	case "set":
		rawURL := options[0].Options[0].StringValue()
		
		// Validate URL
		parsed, err := url.ParseRequestURI(rawURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			respondEmbed(s, i, utils.ErrorEmbed("Invalid URL. Must start with http:// or https://"))
			return
		}

		err = database.SetWebhook(userID, rawURL)
		if err != nil {
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

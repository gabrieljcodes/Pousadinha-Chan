package commands

import (
	"bot/internal/database"
	"bot/internal/locale"
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
			respondEmbed(s, i, utils.ErrorEmbed(locale.Text("commands.webhook_cmd.invalid_webhook_url")+err.Error()))
			return
		}

		if err := database.SetWebhook(userID, rawURL); err != nil {
			respondEmbed(s, i, utils.ErrorEmbed(locale.Text("commands.webhook_cmd.database_error_saving_webhook")))
			return
		}

		respondEmbed(s, i, utils.SuccessEmbed(locale.Text("commands.webhook_cmd.webhook_configured"), locale.Text("commands.webhook_cmd.your_webhook_url_has_been_saved")))

	case "test":
		targetURL, err := database.GetWebhook(userID)
		if err != nil || targetURL == "" {
			respondEmbed(s, i, utils.ErrorEmbed(locale.Text("commands.webhook_cmd.you_don_t_have_a_webhook_configured")))
			return
		}

		err = webhook.TestWebhook(targetURL)
		if err != nil {
			respondEmbed(s, i, utils.ErrorEmbed(locale.Text("commands.webhook_cmd.test_failed")+err.Error()))
			return
		}

		respondEmbed(s, i, utils.SuccessEmbed(locale.Text("commands.webhook_cmd.test_sent"), locale.Text("commands.webhook_cmd.we_sent_a_test_payload_to_your")))

	case "delete":
		err := database.SetWebhook(userID, "") // Setting empty removes it effectively
		if err != nil {
			respondEmbed(s, i, utils.ErrorEmbed(locale.Text("commands.webhook_cmd.error_removing_webhook")))
			return
		}
		respondEmbed(s, i, utils.SuccessEmbed(locale.Text("commands.webhook_cmd.webhook_removed"), locale.Text("commands.webhook_cmd.you_will_no_longer_receive_notifications")))
	}
}

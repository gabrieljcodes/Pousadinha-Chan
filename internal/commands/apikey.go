package commands

import (
	"bot/internal/database"
	"bot/internal/locale"
	"bot/pkg/utils"
	"crypto/rand"
	"encoding/hex"

	"strings"

	"github.com/bwmarrin/discordgo"
)

func generateSecureAPIKey() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return "ec_live_" + hex.EncodeToString(b)
}

func HandleSlashApiKey(s *discordgo.Session, i *discordgo.InteractionCreate) {
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
	case "create":
		name := locale.Text("commands.apikey.default_key")
		if len(options[0].Options) > 0 {
			name = strings.TrimSpace(options[0].Options[0].StringValue())
			if name == "" {
				name = locale.Text("commands.apikey.default_key")
			}
		}

		key := generateSecureAPIKey()
		err := database.CreateAPIKey(key, userID, name)
		if err != nil {
			respondEmbed(s, i, utils.ErrorEmbed(locale.Text("commands.apikey.could_not_create_api_key.formatted", locale.Data{"Err": err})))
			return
		}

		// Send as an Ephemeral response visible only to the caller in Discord
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Flags: discordgo.MessageFlagsEphemeral,
				Embeds: []*discordgo.MessageEmbed{
					utils.SuccessEmbed(locale.Text("commands.apikey.api_key_created"),
						locale.Text("commands.apikey.name_your_secret_key_save_this_key.formatted", locale.Data{"Name": name, "Key": key})),
				},
			},
		})

	case "list":
		keys, err := database.ListAPIKeys(userID)
		if err != nil {
			respondEmbed(s, i, utils.ErrorEmbed(locale.Text("commands.apikey.error_listing_api_keys")))
			return
		}

		if len(keys) == 0 {
			respondEmbed(s, i, utils.InfoEmbed(locale.Text("commands.apikey.api_keys"), locale.Text("commands.apikey.you_don_t_have_any_api_keys")))
			return
		}

		var desc strings.Builder
		desc.WriteString(locale.Text("commands.apikey.use_apikey_delete_prefix_to_revoke_a"))
		for _, k := range keys {
			desc.WriteString(locale.Text("commands.apikey.created.formatted", locale.Data{"Name": k.Name, "KeyPrefix": k.KeyPrefix, "CreatedAt": k.CreatedAt.Format("2006-01-02")}))
		}

		respondEmbed(s, i, utils.GoldEmbed(locale.Text("commands.apikey.your_api_keys"), desc.String()))

	case "delete":
		prefix := strings.TrimSpace(options[0].Options[0].StringValue())
		if len(prefix) < 3 {
			respondEmbed(s, i, utils.ErrorEmbed(locale.Text("commands.apikey.please_provide_at_least_characters_of_the")))
			return
		}

		err := database.DeleteAPIKey(userID, prefix)
		if err != nil {
			respondEmbed(s, i, utils.ErrorEmbed(locale.Text("commands.apikey.no_active_api_key_found_matching_that")))
			return
		}

		respondEmbed(s, i, utils.SuccessEmbed(locale.Text("commands.apikey.key_revoked"), locale.Text("commands.apikey.api_key_matching_has_been_revoked.formatted", locale.Data{"Prefix": prefix})))
	}
}

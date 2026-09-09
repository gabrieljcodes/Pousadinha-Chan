package commands

import (
	"crypto/rand"
	"encoding/hex"
	"bot/internal/database"
	"bot/pkg/utils"
	"fmt"
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
		name := "Default Key"
		if len(options[0].Options) > 0 {
			name = strings.TrimSpace(options[0].Options[0].StringValue())
			if name == "" {
				name = "Default Key"
			}
		}

		key := generateSecureAPIKey()
		err := database.CreateAPIKey(key, userID, name)
		if err != nil {
			respondEmbed(s, i, utils.ErrorEmbed(fmt.Sprintf("Could not create API key: %v", err)))
			return
		}

		// Send as an Ephemeral response visible only to the caller in Discord
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Flags: discordgo.MessageFlagsEphemeral,
				Embeds: []*discordgo.MessageEmbed{
					utils.SuccessEmbed("🔑 API Key Created",
						fmt.Sprintf("**Name:** %s\n\n**Your Secret Key:**\n`%s`\n\n⚠️ **Save this key now!** It will not be shown again.\nUse `/apikey list` to see active key prefixes.", name, key)),
				},
			},
		})

	case "list":
		keys, err := database.ListAPIKeys(userID)
		if err != nil {
			respondEmbed(s, i, utils.ErrorEmbed("Error listing API keys."))
			return
		}

		if len(keys) == 0 {
			respondEmbed(s, i, utils.InfoEmbed("API Keys", "You don't have any API keys. Use `/apikey create` to generate one."))
			return
		}

		var desc strings.Builder
		desc.WriteString("Use `/apikey delete <prefix>` to revoke a key.\n\n")
		for _, k := range keys {
			desc.WriteString(fmt.Sprintf("• **%s**: `%s...` (Created: %s)\n", k.Name, k.KeyPrefix, k.CreatedAt.Format("2006-01-02")))
		}

		respondEmbed(s, i, utils.GoldEmbed("🔑 Your API Keys", desc.String()))

	case "delete":
		prefix := strings.TrimSpace(options[0].Options[0].StringValue())
		if len(prefix) < 3 {
			respondEmbed(s, i, utils.ErrorEmbed("Please provide at least 3 characters of the key prefix."))
			return
		}

		err := database.DeleteAPIKey(userID, prefix)
		if err != nil {
			respondEmbed(s, i, utils.ErrorEmbed("No active API key found matching that prefix."))
			return
		}

		respondEmbed(s, i, utils.SuccessEmbed("Key Revoked", fmt.Sprintf("API key matching `%s...` has been revoked.", prefix)))
	}
}

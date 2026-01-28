package commands

import (
	"estudocoin/internal/database"
	"estudocoin/pkg/utils"
	"fmt"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/google/uuid"
)

func HandleSlashApiKey(s *discordgo.Session, i *discordgo.InteractionCreate) {
	options := i.ApplicationCommandData().Options
	subCommand := options[0].Name
	userID := i.Member.User.ID

	switch subCommand {
	case "create":
		// Create a new key
		key := uuid.New().String()
		name := "My Key"
		if len(options[0].Options) > 0 {
			name = options[0].Options[0].StringValue()
		}

		err := database.CreateAPIKey(key, userID, name)
		if err != nil {
			respondEmbed(s, i, utils.ErrorEmbed("Error creating API key."))
			return
		}

		// Send via DM
		channel, err := s.UserChannelCreate(userID)
		if err != nil {
			respondEmbed(s, i, utils.ErrorEmbed("I cannot DM you. Please open your DMs."))
			return
		}

		msg, err := s.ChannelMessageSend(channel.ID, fmt.Sprintf("🔑 **Your API Key** (%s)\n\n`%s`\n\n⚠️ This message will be deleted in 60 seconds.", name, key))
		
		if err == nil {
			respondEmbed(s, i, utils.SuccessEmbed("Check your DM!", "I sent your API Key securely."))
			
			// Auto-delete routine
			go func() {
				time.Sleep(60 * time.Second)
				s.ChannelMessageDelete(channel.ID, msg.ID)
			}()
		} else {
			respondEmbed(s, i, utils.ErrorEmbed("Failed to send DM."))
		}

	case "list":
		keys, err := database.ListAPIKeys(userID)
		if err != nil {
			respondEmbed(s, i, utils.ErrorEmbed("Error listing keys."))
			return
		}

		if len(keys) == 0 {
			respondEmbed(s, i, utils.InfoEmbed("No Keys", "You don't have any API keys."))
			return
		}

		var desc strings.Builder
		for _, k := range keys {
			masked := k.Key[:5] + "..."
			desc.WriteString(fmt.Sprintf("**%s**: `%s` (Created: %s)\n", k.Name, masked, k.CreatedAt.Format("2006-01-02")))
		}
		
		respondEmbed(s, i, utils.GoldEmbed("Your API Keys", desc.String()))

	case "delete":
		prefix := options[0].Options[0].StringValue()
		if len(prefix) < 5 {
			respondEmbed(s, i, utils.ErrorEmbed("Provide at least the first 5 characters of the key."))
			return
		}

		err := database.DeleteAPIKey(userID, prefix)
		if err != nil {
			respondEmbed(s, i, utils.ErrorEmbed("Error deleting key."))
			return
		}
		
		respondEmbed(s, i, utils.SuccessEmbed("Key Deleted", "If a key matched that prefix, it has been revoked."))
	}
}

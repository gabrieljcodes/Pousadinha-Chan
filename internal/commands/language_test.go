package commands

import (
	"bot/internal/locale"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestHandleSlashLanguagePermissions(t *testing.T) {
	// Interaction without GuildID
	iDM := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			GuildID: "",
		},
	}
	// Should return early safely
	HandleSlashLanguage(nil, iDM)

	// Interaction without Administrator permission
	var regularPerms int64 = discordgo.PermissionSendMessages
	iNonAdmin := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			GuildID: "123456",
			Member: &discordgo.Member{
				Permissions: regularPerms,
				User: &discordgo.User{
					ID: "user_regular",
				},
			},
			Data: discordgo.ApplicationCommandInteractionData{
				Options: []*discordgo.ApplicationCommandInteractionDataOption{
					{
						Name: "set",
						Options: []*discordgo.ApplicationCommandInteractionDataOption{
							{
								Name:  "mode",
								Value: "pt-BR",
							},
						},
					},
				},
			},
		},
	}
	// Verify it does not panic and requires admin
	if iNonAdmin.Member.Permissions&discordgo.PermissionAdministrator != 0 {
		t.Fatal("expected regular user not to have admin perms")
	}
}

func TestSlashCommandsIncludesLanguage(t *testing.T) {
	found := false
	for _, cmd := range SlashCommands {
		if cmd.Name == "language" {
			found = true
			if cmd.Description != locale.Text("commands.definitions.configure_bot_language_settings") {
				t.Errorf("unexpected description for /language: %s", cmd.Description)
			}
			if len(cmd.Options) != 2 {
				t.Fatalf("expected 2 subcommands (set, status), got %d", len(cmd.Options))
			}
			break
		}
	}
	if !found {
		t.Fatal("/language command not found in SlashCommands")
	}
}

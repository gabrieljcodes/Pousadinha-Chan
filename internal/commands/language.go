package commands

import (
	"bot/internal/database"
	"bot/internal/locale"
	"bot/pkg/config"
	"bot/pkg/utils"
	"os"

	"github.com/bwmarrin/discordgo"
)

// HandleSlashLanguage handles /language commands.
func HandleSlashLanguage(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.GuildID == "" || i.Member == nil {
		respondEmbed(s, i, utils.ErrorEmbed(locale.Text("common.server_only")))
		return
	}

	options := i.ApplicationCommandData().Options
	if len(options) == 0 {
		showLanguageStatus(s, i)
		return
	}

	subCmd := options[0].Name
	switch subCmd {
	case "set":
		// Require Administrator permission
		if i.Member.Permissions&discordgo.PermissionAdministrator == 0 {
			respondEmbed(s, i, utils.ErrorEmbed(locale.Text("commands.language.admin_permission_required")))
			return
		}

		mode := ""
		if len(options[0].Options) > 0 {
			mode = options[0].Options[0].StringValue()
		}

		if mode != "auto" && mode != "pt-BR" && mode != "en" {
			respondEmbed(s, i, utils.ErrorEmbed(locale.Text("commands.language.invalid_mode")))
			return
		}

		if err := database.SetGuildLanguage(i.GuildID, mode); err != nil {
			respondEmbed(s, i, utils.ErrorEmbed(locale.Text("commands.language.failed_to_save")))
			return
		}

		respondEmbed(s, i, utils.SuccessEmbed(
			locale.Text("commands.language.title"),
			locale.Text("commands.language.server_language_set.formatted", locale.Data{"Mode": mode}),
		))

	case "status":
		showLanguageStatus(s, i)
	}
}

func showLanguageStatus(s *discordgo.Session, i *discordgo.InteractionCreate) {
	mode := database.GetGuildLanguageCached(i.GuildID)
	if mode == "" {
		mode = "auto"
	}

	// Calculate effective language for this server
	effective := mode
	if effective == "auto" {
		if botLang := config.Bot.Language; botLang != "" && botLang != "auto" {
			effective = botLang
		} else if envLang := os.Getenv("BOT_LANGUAGE"); envLang != "" && envLang != "auto" {
			effective = envLang
		} else if i.GuildLocale != nil && *i.GuildLocale != "" {
			effective = string(*i.GuildLocale)
		} else {
			effective = "auto (per-user)"
		}
	}

	respondEmbed(s, i, utils.InfoEmbed(
		locale.Text("commands.language.title"),
		locale.Text("commands.language.server_language_status.formatted", locale.Data{
			"Mode":      mode,
			"Effective": effective,
		}),
	))
}

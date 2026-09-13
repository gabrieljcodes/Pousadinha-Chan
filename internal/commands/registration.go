package commands

import (
	"bot/internal/locale"
	"github.com/bwmarrin/discordgo"
)

// ApplicationCommands returns the complete guild-only slash command catalog.
// It automatically attaches Discord localized descriptions for any loaded translations (e.g. pt-BR).
// Bulk overwrite at startup also removes obsolete names from global registration.
func ApplicationCommands() []*discordgo.ApplicationCommand {
	out := make([]*discordgo.ApplicationCommand, len(SlashCommands))
	for n, command := range SlashCommands {
		cmdCopy := *command
		dm := false
		cmdCopy.DMPermission = &dm
		cmdCopy.DescriptionLocalizations = locale.DescriptionLocalizations(cmdCopy.Description)
		if len(cmdCopy.Options) > 0 {
			cmdCopy.Options = localizeOptions(cmdCopy.Options)
		}
		out[n] = &cmdCopy
	}
	return out
}

func localizeOptions(options []*discordgo.ApplicationCommandOption) []*discordgo.ApplicationCommandOption {
	out := make([]*discordgo.ApplicationCommandOption, len(options))
	for i, opt := range options {
		optCopy := *opt
		optCopy.DescriptionLocalizations = locale.OptionDescriptionLocalizations(optCopy.Description)
		if len(optCopy.Choices) > 0 {
			optCopy.Choices = localizeChoices(optCopy.Choices)
		}
		if len(optCopy.Options) > 0 {
			optCopy.Options = localizeOptions(optCopy.Options)
		}
		out[i] = &optCopy
	}
	return out
}

func localizeChoices(choices []*discordgo.ApplicationCommandOptionChoice) []*discordgo.ApplicationCommandOptionChoice {
	out := make([]*discordgo.ApplicationCommandOptionChoice, len(choices))
	for j, choice := range choices {
		choiceCopy := *choice
		choiceCopy.NameLocalizations = locale.ChoiceNameLocalizations(choiceCopy.Name)
		out[j] = &choiceCopy
	}
	return out
}

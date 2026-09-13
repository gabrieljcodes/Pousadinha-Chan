package commands

import "github.com/bwmarrin/discordgo"

// ApplicationCommands returns the complete guild-only slash command catalog.
// Bulk overwrite at startup also removes obsolete names from global registration.
func ApplicationCommands() []*discordgo.ApplicationCommand {
	out := make([]*discordgo.ApplicationCommand, len(SlashCommands))
	for n, command := range SlashCommands {
		copy := *command
		dm := false
		copy.DMPermission = &dm
		out[n] = &copy
	}
	return out
}

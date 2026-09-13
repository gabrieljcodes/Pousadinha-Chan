package commands

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"

	"bot/internal/database"
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
		cmdCopy.NameLocalizations = locale.CommandNameLocalizations(cmdCopy.Name)
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

// CatalogHash returns a deterministic SHA-256 hash representing the current command catalog.
func CatalogHash() string {
	cmds := ApplicationCommands()
	raw, err := json.Marshal(cmds)
	if err != nil {
		return ""
	}
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}

// SyncCommands synchronizes slash commands with Discord only when the catalog schema changes.
// Skipping redundant overwrites preserves Discord's client cache and prevents "This command is outdated" errors on restarts.
func SyncCommands(s *discordgo.Session, appID string, devGuildID string) error {
	currentHash := CatalogHash()
	targetScope := "global"
	if devGuildID != "" {
		targetScope = "guild:" + devGuildID
	}
	metaKey := "slash_commands_hash_" + targetScope

	storedHash, err := database.GetBotMetadata(metaKey)
	if err == nil && storedHash != "" && storedHash == currentHash {
		log.Printf("[Commands] Slash command schema is up to date (%s, hash: %s). Skipping registration to preserve Discord client cache.", targetScope, currentHash[:8])
		return nil
	}

	log.Printf("[Commands] Slash command schema updated (scope: %s, hash: %s). Synchronizing with Discord...", targetScope, currentHash[:8])
	cmds := ApplicationCommands()
	_, err = s.ApplicationCommandBulkOverwrite(appID, devGuildID, cmds)
	if err != nil {
		return fmt.Errorf("bulk overwrite slash commands: %w", err)
	}

	if err := database.SetBotMetadata(metaKey, currentHash); err != nil {
		log.Printf("[Commands] Warning: failed to save schema hash in database: %v", err)
	}

	log.Printf("[Commands] Successfully registered %d slash commands with Discord (%s).", len(cmds), targetScope)
	return nil
}

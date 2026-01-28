package commands

import (
	"estudocoin/internal/games"
	"strings"

	"github.com/bwmarrin/discordgo"
)

func ComponentsHandler(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionMessageComponent {
		return
	}

	customID := i.MessageComponentData().CustomID

	if strings.HasPrefix(customID, "aviator_stop_") {
		games.HandleButton(s, i)
	} else if strings.HasPrefix(customID, "cup_") {
		games.HandleCupInteraction(s, i)
	}
}

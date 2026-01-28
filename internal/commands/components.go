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
	} else if strings.HasPrefix(customID, "bj_hit_") {
		userID := strings.TrimPrefix(customID, "bj_hit_")
		games.HandleBlackjackHit(s, i, userID)
	} else if strings.HasPrefix(customID, "bj_stand_") {
		userID := strings.TrimPrefix(customID, "bj_stand_")
		games.HandleBlackjackStand(s, i, userID)
	} else if strings.HasPrefix(customID, "bj_double_") {
		userID := strings.TrimPrefix(customID, "bj_double_")
		games.HandleBlackjackDouble(s, i, userID)
	} else if strings.HasPrefix(customID, "bj_insurance_") {
		userID := strings.TrimPrefix(customID, "bj_insurance_")
		games.HandleBlackjackInsurance(s, i, userID)
	} else if strings.HasPrefix(customID, "rr_") {
		games.HandleRussianRouletteInteraction(s, i)
	} else if strings.HasPrefix(customID, "slots_spin_") {
		games.HandleSlotsInteraction(s, i)
	}
}
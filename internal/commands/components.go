package commands

import (
	"bot/internal/gacha"
	"bot/internal/games"
	"bot/pkg/config"
	"bot/pkg/utils"
	"strings"

	"github.com/bwmarrin/discordgo"
)

func ComponentsHandler(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type == discordgo.InteractionModalSubmit {
		if strings.HasPrefix(i.ModalSubmitData().CustomID, "poly_modal_") {
			HandlePolymarketModalSubmit(s, i)
		} else if strings.HasPrefix(i.ModalSubmitData().CustomID, "bicho_modal_") {
			HandleBichoModalSubmit(s, i)
		}
		return
	}

	if i.Type != discordgo.InteractionMessageComponent {
		return
	}

	customID := i.MessageComponentData().CustomID

	// Polymarket and Bicho component interactions are always allowed on their respective embeds
	if !strings.HasPrefix(customID, "poly_") && !strings.HasPrefix(customID, "bicho_") && !config.Bot.IsChannelAllowed(i.ChannelID) {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Embeds: []*discordgo.MessageEmbed{utils.ErrorEmbed("❌ This bot can only be used in designated channels.")},
				Flags:  discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	if strings.HasPrefix(customID, "gacha_claim_") {
		gacha.HandleClaim(s, i)
	} else if strings.HasPrefix(customID, "aviator_stop_") {
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
	} else if strings.HasPrefix(customID, "bj_surrender_") {
		userID := strings.TrimPrefix(customID, "bj_surrender_")
		games.HandleBlackjackSurrender(s, i, userID)
	} else if strings.HasPrefix(customID, "bj_split_") {
		userID := strings.TrimPrefix(customID, "bj_split_")
		games.HandleBlackjackSplit(s, i, userID)
	} else if strings.HasPrefix(customID, "rr_") {
		games.HandleRussianRouletteInteraction(s, i)
	} else if strings.HasPrefix(customID, "slots_") {
		games.HandleSlotsInteraction(s, i)
	} else if strings.HasPrefix(customID, "mines_") {
		games.HandleMinesInteraction(s, i)
	} else if strings.HasPrefix(customID, "poly_") {
		HandlePolymarketButton(s, i)
	} else if strings.HasPrefix(customID, "bicho_") {
		HandleBichoButton(s, i)
	} else if strings.HasPrefix(customID, "help_nav_") {
		HandleHelpNavigation(s, i, customID)
	} else if strings.HasPrefix(customID, "loan_accept_") {
		loanID := strings.TrimPrefix(customID, "loan_accept_")
		HandleLoanAccept(s, i, loanID)
	} else if strings.HasPrefix(customID, "loan_decline_") {
		loanID := strings.TrimPrefix(customID, "loan_decline_")
		HandleLoanDecline(s, i, loanID)
	}
}

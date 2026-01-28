package commands

import (
	"estudocoin/internal/games"
	"estudocoin/pkg/utils"
	"strconv"
	"strings"

	"github.com/bwmarrin/discordgo"
)

func CmdBet(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	if len(args) < 2 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.InfoEmbed("Gambling", "Usage: `!bet aviator <amount>`"))
		return
	}

	game := strings.ToLower(args[0])
	amountStr := args[1]
	amount, err := strconv.Atoi(amountStr)

	if err != nil || amount <= 0 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Invalid amount."))
		return
	}

	switch game {
	case "aviator":
		games.StartAviatorText(s, m, amount)
	case "cups":
		games.StartCupGameText(s, m, amount)
	default:
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Game not found. Try: `aviator`, `cups`"))
	}
}

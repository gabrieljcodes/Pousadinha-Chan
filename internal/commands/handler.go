package commands

import (
	"estudocoin/internal/games"
	"estudocoin/internal/stockmarket"
	"estudocoin/pkg/config"
	"estudocoin/pkg/utils"
	"strconv"
	"strings"

	"github.com/bwmarrin/discordgo"
)

func MessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.ID == s.State.User.ID {
		return
	}

	if !strings.HasPrefix(m.Content, "!") {
		return
	}

	// Check if channel is allowed
	if !config.Bot.IsChannelAllowed(m.ChannelID) {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("❌ This bot can only be used in designated channels."))
		return
	}

	args := strings.Fields(m.Content)
	command := strings.ToLower(args[0])
	args = args[1:]

	switch command {
	case "!help", "!ajuda":
		CmdHelp(s, m)
	case "!daily":
		CmdDaily(s, m)
	case "!balance", "!saldo", "!coins", "!money":
		CmdBalance(s, m)
	case "!leaderboard", "!top", "!rank":
		CmdLeaderboard(s, m)
	case "!pay", "!transfer", "!pagar":
		CmdPay(s, m, args)
	case "!shop", "!store", "!loja":
		CmdShop(s, m)
	case "!buy", "!purchase", "!comprar":
		CmdBuy(s, m, args)
	case "!bet", "!apostar":
		CmdBet(s, m, args)
	case "!roulette", "!roleta":
		games.CmdRussianRoulette(s, m, args)
	case "!slots", "!slot":
		if len(args) < 1 {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Usage: `!slots <amount>`"))
			return
		}
		amount, err := strconv.Atoi(args[0])
		if err != nil || amount <= 0 {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Invalid amount."))
			return
		}
		games.StartSlotsText(s, m, amount)
		return
	case "!stock", "!mercado", "!market":
		stockmarket.CmdStock(s, m, args)
	case "!wheel", "!roleta-cassino":
		games.CmdRoulette(s, m, args)
	}
}
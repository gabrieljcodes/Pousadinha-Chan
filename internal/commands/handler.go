package commands

import (
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
	case "!pay", "!transfer", "!pagar":
		CmdPay(s, m, args)
	case "!shop", "!store", "!loja":
		CmdShop(s, m)
	case "!buy", "!purchase", "!comprar":
		CmdBuy(s, m, args)
	case "!bet", "!apostar":
		CmdBet(s, m, args)
	}
}
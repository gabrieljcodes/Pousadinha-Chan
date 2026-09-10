package commands

import (
	"bot/internal/crypto"
	"bot/internal/database"
	"bot/internal/gacha"
	"bot/internal/games"
	"bot/internal/stockmarket"
	"bot/pkg/config"
	"bot/pkg/utils"
	"fmt"
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

	args := strings.Fields(m.Content)
	if len(args) == 0 {
		return
	}
	command := strings.ToLower(args[0])
	args = args[1:]

	// Check if channel is allowed
	if !config.Bot.IsChannelAllowed(m.ChannelID) {
		allowedInSpecial := false
		if strings.HasPrefix(command, "!poly") && m.GuildID != "" {
			settings, _ := database.GetGuildPolymarketSettings(m.GuildID)
			if settings != nil && settings.ChannelID == m.ChannelID {
				allowedInSpecial = true
			}
		}
		if (strings.HasPrefix(command, "!bicho") || command == "!jb" || command == "!jogodobicho") && m.GuildID != "" {
			bichoSettings, _ := database.GetBichoSettings(m.GuildID)
			if bichoSettings != nil && bichoSettings.ChannelID == m.ChannelID {
				allowedInSpecial = true
			}
		}
		if !allowedInSpecial {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("❌ This bot can only be used in designated channels."))
			return
		}
	}

	switch command {
	case "!gacha":
		gacha.Text(s, m, args)
	case "!help", "!ajuda":
		CmdHelp(s, m)
	case "!daily":
		CmdDaily(s, m)
	case "!balance", "!saldo", "!coins", "!money":
		CmdBalance(s, m)
	case "!leaderboard", "!top", "!rank":
		CmdLeaderboard(s, m, args)
	case "!pay", "!transfer", "!pagar":
		CmdPay(s, m, args)
	case "!shop", "!store", "!loja":
		CmdShop(s, m)
	case "!buy", "!purchase", "!comprar":
		CmdBuy(s, m, args)
	case "!bet", "!apostar":
		CmdBet(s, m, args)
	case "!bj", "!blackjack":
		if len(args) == 0 {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.InfoEmbed("Blackjack",
				"**Commands:**\n"+
					"`!bj <amount>` - Start a new game\n"+
					"`!bj hit` - Draw another card\n"+
					"`!bj stand` - Keep your hand\n"+
					"`!bj double` - Double bet, draw one card and stand\n"+
					"`!bj split` - Split matching cards into two hands\n"+
					"`!bj insurance` - Buy insurance against dealer Ace\n"+
					"`!bj surrender` - Surrender hand and recover 50% of bet"))
			return
		}
		subCmd := strings.ToLower(args[0])
		switch subCmd {
		case "hit", "pedir":
			games.HandleBlackjackTextAction(s, m, "hit")
		case "stand", "parar", "ficar":
			games.HandleBlackjackTextAction(s, m, "stand")
		case "double", "dobrar":
			games.HandleBlackjackTextAction(s, m, "double")
		case "split", "dividir":
			games.HandleBlackjackTextAction(s, m, "split")
		case "insurance", "seguro":
			games.HandleBlackjackTextAction(s, m, "insurance")
		case "surrender", "desistir":
			games.HandleBlackjackTextAction(s, m, "surrender")
		default:
			if bet, err := strconv.Atoi(args[0]); err == nil && bet > 0 {
				games.StartBlackjackText(s, m, bet)
				return
			}
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Invalid command. Use `!bj <amount>` or `!bj <hit|stand|double|split|insurance|surrender>`"))
		}
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
	case "!mines", "!mina", "!campo-minado":
		if len(args) < 1 {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.InfoEmbed("💣 Mines (Campo Minado)", "Uso: `!mines <aposta> [minas]`\nExemplo: `!mines 100 3` (Padrão: 3 minas)"))
			return
		}
		amount, err := strconv.Atoi(args[0])
		if err != nil || amount <= 0 {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Valor de aposta inválido."))
			return
		}
		minesCount := games.MinesDefaultCount
		if len(args) >= 2 {
			if parsed, err := strconv.Atoi(args[1]); err == nil && parsed >= games.MinesMinCount && parsed <= games.MinesMaxCount {
				minesCount = parsed
			} else {
				s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed(fmt.Sprintf("Quantidade de minas inválida! Escolha entre %d e %d.", games.MinesMinCount, games.MinesMaxCount)))
				return
			}
		}
		games.StartMinesText(s, m, amount, minesCount)
		return
	case "!stock", "!mercado", "!market":
		stockmarket.CmdStock(s, m, args)
	case "!crypto":
		crypto.CmdCrypto(s, m, args)
	case "!wheel", "!roleta-cassino":
		games.CmdRoulette(s, m, args)
	case "!createevent":
		games.CmdCreateEvent(s, m, args)
	case "!betevent":
		games.CmdPlaceBet(s, m, args)
	case "!result":
		games.CmdSetResult(s, m, args)
	case "!events":
		games.CmdListEvents(s, m, args)
	case "!event":
		games.CmdViewEvent(s, m, args)
	case "!closeevent":
		games.CmdCloseEvent(s, m, args)
	case "!cancelevent":
		games.CmdCancelEvent(s, m, args)
	case "!poly", "!polymarket":
		CmdPolymarket(s, m, args)
	case "!bicho", "!jb", "!jogodobicho":
		CmdBicho(s, m, args)
	case "!loan":
		if len(args) < 1 {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.InfoEmbed("Loan System",
				"**Commands:**\n"+
					"`!loan offer @user <amount> <interest> <days>` - Offer a loan\n"+
					"`!loan pay [loan_id]` - Pay a loan\n"+
					"`!loan list [@user]` - List active loans"))
			return
		}
		subCommand := strings.ToLower(args[0])
		switch subCommand {
		case "offer":
			CmdLoanOffer(s, m, args)
		case "pay":
			CmdLoanPay(s, m, args)
		case "list":
			CmdLoanList(s, m, args)
		default:
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Unknown loan command. Use `!loan offer`, `!loan pay`, or `!loan list`"))
		}
	}
}

package commands

import (
	"bot/internal/crypto"
	"bot/internal/database"
	"bot/internal/gacha"
	"bot/internal/games"
	"bot/internal/stockmarket"
	"bot/internal/webhook"
	"bot/pkg/config"
	"bot/pkg/utils"
	"fmt"

	"github.com/bwmarrin/discordgo"
)

// Helper to send interaction response easily
func respondEmbed(s *discordgo.Session, i *discordgo.InteractionCreate, embed *discordgo.MessageEmbed) {
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds: []*discordgo.MessageEmbed{embed},
		},
	})
}

func SlashHandler(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionApplicationCommand {
		return
	}

	// Check if channel is allowed
	if !config.Bot.IsChannelAllowed(i.ChannelID) {
		allowedInSpecial := false
		if i.ApplicationCommandData().Name == "poly" && i.GuildID != "" {
			settings, _ := database.GetGuildPolymarketSettings(i.GuildID)
			if settings != nil && settings.ChannelID == i.ChannelID {
				allowedInSpecial = true
			}
		}
		if i.ApplicationCommandData().Name == "bicho" && i.GuildID != "" {
			bichoSettings, _ := database.GetBichoSettings(i.GuildID)
			if bichoSettings != nil && bichoSettings.ChannelID == i.ChannelID {
				allowedInSpecial = true
			}
		}
		if !allowedInSpecial {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Embeds: []*discordgo.MessageEmbed{utils.ErrorEmbed("❌ This bot can only be used in designated channels.")},
					Flags:  discordgo.MessageFlagsEphemeral,
				},
			})
			return
		}
	}

	switch i.ApplicationCommandData().Name {
	case "gacha":
		gacha.Slash(s, i)
	case "help":
		HandleSlashHelp(s, i)
	case "daily":
		handleSlashDaily(s, i)
	case "balance":
		handleSlashBalance(s, i)
	case "leaderboard":
		handleSlashLeaderboard(s, i)
	case "pay":
		handleSlashPay(s, i)
	case "shop":
		handleSlashShop(s, i)
	case "buy":
		handleSlashBuy(s, i)
	case "apikey":
		HandleSlashApiKey(s, i)
	case "webhook":
		HandleSlashWebhook(s, i)
	case "bet":
		handleSlashBet(s, i)
	case "slots":
		handleSlashSlots(s, i)
	case "wheel":
		handleSlashWheel(s, i)
	case "roulette":
		handleSlashRoulette(s, i)
	case "blackjack":
		handleSlashBlackjack(s, i)
	case "mines":
		handleSlashMines(s, i)
	case "loan":
		handleSlashLoan(s, i)
	case "stock":
		handleSlashStock(s, i)
	case "crypto":
		handleSlashCrypto(s, i)
	case "poly":
		HandleSlashPolymarket(s, i)
	case "bicho":
		HandleSlashBicho(s, i)
	}
}

func handleSlashRoulette(s *discordgo.Session, i *discordgo.InteractionCreate) {
	options := i.ApplicationCommandData().Options
	if len(options) == 0 {
		return
	}

	subCmd := options[0].Name
	if subCmd == "challenge" {
		targetUser := options[0].Options[0].UserValue(s)
		amount := int(options[0].Options[1].IntValue())
		games.StartRussianRouletteInteraction(s, i, targetUser, amount)
	}
}

func handleSlashWheel(s *discordgo.Session, i *discordgo.InteractionCreate) {
	options := i.ApplicationCommandData().Options
	if len(options) == 0 {
		return
	}

	subCmd := options[0].Name
	if subCmd == "status" {
		endTime, active, betsCount, totalAmount := games.GetCurrentRoundInfo()
		if !active {
			respondEmbed(s, i, utils.InfoEmbed("Casino Roulette", "The wheel is currently spinning! Please wait for the next round."))
			return
		}

		embed := &discordgo.MessageEmbed{
			Title: "🎰 Casino Roulette - Status",
			Description: fmt.Sprintf("Next spin <t:%d:R> (<t:%d:T>).\n\n"+
				"**Bets Placed:** %d\n"+
				"**Total Wagered:** %d %s",
				endTime.Unix(), endTime.Unix(), betsCount, totalAmount, config.Bot.CurrencySymbol),
			Color: utils.ColorGold,
			Footer: &discordgo.MessageEmbedFooter{
				Text: "Place your bets with /wheel bet or !wheel <type> <amount>",
			},
		}
		respondEmbed(s, i, embed)
		return
	}

	if subCmd == "bet" {
		betOptions := options[0].Options
		betTypeChoice := ""
		amount := 0
		specificNumber := -1

		for _, opt := range betOptions {
			switch opt.Name {
			case "type":
				betTypeChoice = opt.StringValue()
			case "amount":
				amount = int(opt.IntValue())
			case "number":
				specificNumber = int(opt.IntValue())
			}
		}

		var betType games.BetType
		var value string

		switch betTypeChoice {
		case "number":
			if specificNumber < 0 || specificNumber > 36 {
				respondEmbed(s, i, utils.ErrorEmbed("Please provide a valid number between 0 and 36 for number bets!"))
				return
			}
			betType = games.BetNumber
			value = fmt.Sprintf("%d", specificNumber)
		case "red", "black":
			betType = games.BetColor
			value = betTypeChoice
		case "even", "odd":
			betType = games.BetEvenOdd
			value = betTypeChoice
		case "low":
			betType = games.BetHalf
			value = "1-18"
		case "high":
			betType = games.BetHalf
			value = "19-36"
		case "1st", "2nd", "3rd":
			betType = games.BetDozen
			value = betTypeChoice
		default:
			respondEmbed(s, i, utils.ErrorEmbed("Invalid bet type."))
			return
		}

		userID := i.Member.User.ID
		username := i.Member.User.Username

		success, msg := games.PlaceRouletteBet(userID, username, betType, value, amount)
		if !success {
			respondEmbed(s, i, utils.ErrorEmbed(msg))
			return
		}

		endTime, _, _, _ := games.GetCurrentRoundInfo()
		respondEmbed(s, i, utils.SuccessEmbed("Bet Placed!",
			fmt.Sprintf("You bet **%d %s** on **%s**.\nNext spin: <t:%d:R>",
				amount, config.Bot.CurrencySymbol, value, endTime.Unix())))
	}
}

func handleSlashBet(s *discordgo.Session, i *discordgo.InteractionCreate) {
	options := i.ApplicationCommandData().Options
	subCommand := options[0].Name

	switch subCommand {
	case "aviator":
		amount := int(options[0].Options[0].IntValue())
		var autoCashout float64
		if len(options[0].Options) > 1 {
			autoCashout = options[0].Options[1].FloatValue()
		}
		games.StartAviatorInteraction(s, i, amount, autoCashout)
	case "cups":
		amount := int(options[0].Options[0].IntValue())
		games.StartCupGameInteraction(s, i, amount)
	case "slots":
		amount := int(options[0].Options[0].IntValue())
		games.StartSlotsInteraction(s, i, amount)
	case "mines":
		amount := int(options[0].Options[0].IntValue())
		minesCount := games.MinesDefaultCount
		if len(options[0].Options) > 1 {
			minesCount = int(options[0].Options[1].IntValue())
		}
		games.StartMinesInteraction(s, i, amount, minesCount)
	}
}

func handleSlashSlots(s *discordgo.Session, i *discordgo.InteractionCreate) {
	options := i.ApplicationCommandData().Options
	bet := int(options[0].IntValue())

	games.StartSlotsInteraction(s, i, bet)
}

func handleSlashBlackjack(s *discordgo.Session, i *discordgo.InteractionCreate) {
	options := i.ApplicationCommandData().Options
	bet := int(options[0].IntValue())

	games.StartBlackjackGame(s, i, bet)
}

func handleSlashMines(s *discordgo.Session, i *discordgo.InteractionCreate) {
	options := i.ApplicationCommandData().Options
	bet := int(options[0].IntValue())
	minesCount := games.MinesDefaultCount
	if len(options) > 1 {
		minesCount = int(options[1].IntValue())
	}

	games.StartMinesInteraction(s, i, bet, minesCount)
}

func handleSlashDaily(s *discordgo.Session, i *discordgo.InteractionCreate) {
	respondEmbed(s, i, ExecuteDaily(i.Member.User.ID))
}

func handleSlashBalance(s *discordgo.Session, i *discordgo.InteractionCreate) {
	targetUser := i.Member.User
	options := i.ApplicationCommandData().Options
	if len(options) > 0 {
		targetUser = options[0].UserValue(s)
	}

	balance := database.GetBalance(targetUser.ID)
	respondEmbed(s, i, utils.GoldEmbed("Balance", fmt.Sprintf("**%s** has **%d %s**.", targetUser.Username, balance, config.Bot.CurrencyName)))
}

func handleSlashLeaderboard(s *discordgo.Session, i *discordgo.InteractionCreate) {
	category := "networth"
	options := i.ApplicationCommandData().Options
	if len(options) > 0 {
		category = options[0].StringValue()
	}
	userID := ""
	if i.Member != nil && i.Member.User != nil {
		userID = i.Member.User.ID
	} else if i.User != nil {
		userID = i.User.ID
	}
	respondEmbed(s, i, ExecuteLeaderboard(s, i.GuildID, userID, category))
}

func handleSlashPay(s *discordgo.Session, i *discordgo.InteractionCreate) {
	options := i.ApplicationCommandData().Options
	toUser := options[0].UserValue(s)
	amount := int(options[1].IntValue())
	fromID := i.Member.User.ID

	if toUser.ID == fromID {
		respondEmbed(s, i, utils.ErrorEmbed("You cannot pay yourself."))
		return
	}

	err := database.TransferCoins(fromID, toUser.ID, amount)
	if err != nil {
		respondEmbed(s, i, utils.ErrorEmbed("Insufficient funds or transaction error."))
		return
	}

	webhook.SendTransferNotification(fromID, toUser.ID, amount)

	respondEmbed(s, i, utils.SuccessEmbed("Transfer Successful", fmt.Sprintf("You sent **%d %s** to **%s**.", amount, config.Bot.CurrencyName, toUser.Username)))
}

func handleSlashShop(s *discordgo.Session, i *discordgo.InteractionCreate) {
	respondEmbed(s, i, ExecuteShop())
}

func handleSlashBuy(s *discordgo.Session, i *discordgo.InteractionCreate) {
	options := i.ApplicationCommandData().Options
	if len(options) == 0 {
		return
	}
	subCommand := options[0].Name
	userID := ""
	if i.Member != nil && i.Member.User != nil {
		userID = i.Member.User.ID
	} else if i.User != nil {
		userID = i.User.ID
	}
	guildID := i.GuildID

	subOpts := options[0].Options
	switch subCommand {
	case "nickname":
		if len(subOpts) < 1 {
			return
		}
		newName := subOpts[0].StringValue()
		respondEmbed(s, i, ExecuteBuyNickname(s, guildID, userID, newName))

	case "rename":
		if len(subOpts) < 2 {
			return
		}
		targetUser := subOpts[0].UserValue(s)
		newName := subOpts[1].StringValue()
		respondEmbed(s, i, ExecuteBuyRename(s, guildID, userID, targetUser, newName))

	case "timeout", "punishment":
		if len(subOpts) < 2 {
			return
		}
		targetUser := subOpts[0].UserValue(s)
		minutes := int(subOpts[1].IntValue())
		respondEmbed(s, i, ExecuteBuyTimeout(s, guildID, userID, targetUser, minutes))

	case "mute":
		if len(subOpts) < 2 {
			return
		}
		targetUser := subOpts[0].UserValue(s)
		minutes := int(subOpts[1].IntValue())
		respondEmbed(s, i, ExecuteBuyMute(s, guildID, userID, targetUser, minutes))
	}
}

func handleSlashLoan(s *discordgo.Session, i *discordgo.InteractionCreate) {
	options := i.ApplicationCommandData().Options
	subCommand := options[0].Name

	switch subCommand {
	case "offer":
		targetUser := options[0].Options[0].UserValue(s)
		amount := int(options[0].Options[1].IntValue())
		interest := options[0].Options[2].FloatValue()
		days := int(options[0].Options[3].IntValue())

		ExecuteLoanOffer(s, i.ChannelID, i.GuildID, i.Member.User, targetUser, amount, interest, days, i)

	case "pay":
		loanID := ""
		if len(options[0].Options) > 0 {
			loanID = options[0].Options[0].StringValue()
		}

		ExecuteLoanPay(s, i.ChannelID, i.Member.User.ID, loanID, i)

	case "list":
		ExecuteLoanList(s, i.ChannelID, i.Member.User, true, i)
	}
}

func handleSlashStock(s *discordgo.Session, i *discordgo.InteractionCreate) {
	options := i.ApplicationCommandData().Options
	if len(options) == 0 {
		return
	}

	userID := i.Member.User.ID
	subCommand := options[0].Name

	switch subCommand {
	case "market":
		respondEmbed(s, i, stockmarket.ExecuteMarket())
	case "buy":
		ticker := options[0].Options[0].StringValue()
		amount := int(options[0].Options[1].IntValue())
		respondEmbed(s, i, stockmarket.ExecuteBuy(userID, ticker, amount))
	case "sell":
		ticker := options[0].Options[0].StringValue()
		shares := options[0].Options[1].StringValue()
		respondEmbed(s, i, stockmarket.ExecuteSell(userID, ticker, shares))
	case "portfolio":
		respondEmbed(s, i, stockmarket.ExecutePortfolio(userID))
	}
}

func handleSlashCrypto(s *discordgo.Session, i *discordgo.InteractionCreate) {
	options := i.ApplicationCommandData().Options
	if len(options) == 0 {
		return
	}

	userID := i.Member.User.ID
	subCommand := options[0].Name

	switch subCommand {
	case "market":
		respondEmbed(s, i, crypto.ExecuteCryptoMarket())
	case "buy":
		symbol := options[0].Options[0].StringValue()
		amount := int(options[0].Options[1].IntValue())
		respondEmbed(s, i, crypto.ExecuteCryptoBuy(userID, symbol, amount))
	case "sell":
		symbol := options[0].Options[0].StringValue()
		coins := options[0].Options[1].StringValue()
		respondEmbed(s, i, crypto.ExecuteCryptoSell(userID, symbol, coins))
	case "portfolio":
		respondEmbed(s, i, crypto.ExecuteCryptoPortfolio(userID))
	}
}

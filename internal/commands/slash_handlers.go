package commands

import (
	"bot/internal/crypto"
	"bot/internal/database"
	"bot/internal/gacha"
	"bot/internal/games"
	"bot/internal/locale"
	"bot/internal/stockmarket"
	"bot/internal/webhook"
	"bot/pkg/config"
	"bot/pkg/utils"
	"fmt"
	"log"

	"github.com/bwmarrin/discordgo"
)

// Helper to send interaction response easily
func respondEmbed(s *discordgo.Session, i *discordgo.InteractionCreate, embed *discordgo.MessageEmbed) {
	if s == nil || i == nil || i.Interaction == nil {
		return
	}
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds: []*discordgo.MessageEmbed{embed},
		},
	})
}

// respondDeferredEmbed acknowledges the interaction before starting database
// work. A failed acknowledgement must not trigger a monetary operation.
func respondDeferredEmbed(s *discordgo.Session, i *discordgo.InteractionCreate, build func() *discordgo.MessageEmbed) {
	if s == nil || i == nil || i.Interaction == nil {
		return
	}
	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	}); err != nil {
		log.Printf("[commands] interaction %s acknowledgement failed: %v", i.ID, err)
		return
	}
	embeds := []*discordgo.MessageEmbed{build()}
	if _, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Embeds: &embeds, AllowedMentions: &discordgo.MessageAllowedMentions{}}); err != nil {
		log.Printf("[commands] interaction %s response delivery failed: %v", i.ID, err)
	}
}

func isGachaCommand(name string) bool {
	switch name {
	case "roll", "rolls", "top", "info", "harem", "profile", "gallery", "wishlist", "wish", "unwish", "wishclear", "trade", "gift", "divorce", "keys", "offers", "search", "harem-ranking", "alias", "series", "gachashop", "inventory", "gachabuy", "use", "open":
		return true
	default:
		return false
	}
}

func SlashHandler(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionApplicationCommand {
		return
	}

	defer locale.EnterInteraction(i)()
	loc := locale.FromInteraction(i)

	if i.GuildID == "" || i.Member == nil || i.Member.User == nil {
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{Content: loc.Text("common.server_only"), Flags: discordgo.MessageFlagsEphemeral},
		})
		return
	}

	// Check if channel is allowed
	if !config.Bot.IsChannelAllowed(i.ChannelID) {
		allowedInSpecial := false
		cmdName := i.ApplicationCommandData().Name
		if isGachaCommand(cmdName) {
			allowedInSpecial = true
		}
		if cmdName == "poly" && i.GuildID != "" {
			settings, _ := database.GetGuildPolymarketSettings(i.GuildID)
			if settings != nil && settings.ChannelID == i.ChannelID {
				allowedInSpecial = true
			}
		}
		if cmdName == "bicho" && i.GuildID != "" {
			bichoSettings, _ := database.GetBichoSettings(i.GuildID)
			if bichoSettings != nil && bichoSettings.ChannelID == i.ChannelID {
				allowedInSpecial = true
			}
		}
		if cmdName == "gachaconfig" && i.Member != nil && (i.Member.Permissions&(discordgo.PermissionAdministrator|discordgo.PermissionManageServer) != 0) {
			allowedInSpecial = true
		}
		if !allowedInSpecial {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Embeds: []*discordgo.MessageEmbed{utils.ErrorEmbed(loc.Text("commands.components.this_bot_can_only_be_used_in"))},
					Flags:  discordgo.MessageFlagsEphemeral,
				},
			})
			return
		}
	}

	switch i.ApplicationCommandData().Name {
	case "roll", "rolls", "top", "info", "harem", "profile", "gallery", "wishlist", "wish", "unwish", "wishclear", "trade", "gift", "divorce", "keys", "offers", "search", "harem-ranking", "alias", "series":
		gacha.Slash(s, i)
	case "gachaconfig":
		gacha.HandleConfigSlash(s, i)
	case "event":
		games.HandleEventCommand(s, i)
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
	case "language":
		HandleSlashLanguage(s, i)
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
			respondEmbed(s, i, utils.InfoEmbed(locale.Text("commands.slash_handlers.casino_roulette"), locale.Text("commands.slash_handlers.the_wheel_is_currently_spinning_please_wait")))
			return
		}

		embed := &discordgo.MessageEmbed{
			Title:       locale.Text("commands.slash_handlers.casino_roulette_status"),
			Description: locale.Text("commands.slash_handlers.next_spin_t_r_t_t_bets.formatted", locale.Data{"EndTime": endTime.Unix(), "EndTime2": endTime.Unix(), "BetsCount": betsCount, "TotalAmount": totalAmount, "CurrencySymbol": config.Bot.CurrencySymbol}),
			Color:       utils.ColorGold,
			Footer: &discordgo.MessageEmbedFooter{
				Text: locale.Text("commands.slash_handlers.place_your_bets_with_wheel_bet_or"),
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
				respondEmbed(s, i, utils.ErrorEmbed(locale.Text("commands.slash_handlers.please_provide_a_valid_number_between_and")))
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
			respondEmbed(s, i, utils.ErrorEmbed(locale.Text("commands.slash_handlers.invalid_bet_type")))
			return
		}

		if i.GuildID == "" {
			respondEmbed(s, i, utils.ErrorEmbed(locale.Text("commands.economy.this_command_can_only_be_used_within")))
			return
		}

		userID := ""
		username := ""
		if i.Member != nil && i.Member.User != nil {
			userID = i.Member.User.ID
			username = i.Member.User.Username
		} else if i.User != nil {
			userID = i.User.ID
			username = i.User.Username
		}

		success, msg := games.PlaceRouletteBet(i.GuildID, userID, username, betType, value, amount)
		if !success {
			respondEmbed(s, i, utils.ErrorEmbed(msg))
			return
		}

		endTime, _, _, _ := games.GetCurrentRoundInfo()
		respondEmbed(s, i, utils.SuccessEmbed(locale.Text("commands.slash_handlers.bet_placed"),
			locale.Text("commands.slash_handlers.you_bet_on_next_spin_t_r.formatted", locale.Data{"Amount": amount, "CurrencySymbol": config.Bot.CurrencySymbol, "Value": value, "EndTime": endTime.Unix()})))
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
	if i.GuildID == "" {
		respondEmbed(s, i, utils.ErrorEmbed(locale.Text("commands.economy.this_command_can_only_be_used_within")))
		return
	}
	userID := ""
	if i.Member != nil && i.Member.User != nil {
		userID = i.Member.User.ID
	} else if i.User != nil {
		userID = i.User.ID
	}
	respondDeferredEmbed(s, i, func() *discordgo.MessageEmbed {
		return ExecuteDaily(i.GuildID, userID)
	})
}

func handleSlashBalance(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.GuildID == "" {
		respondEmbed(s, i, utils.ErrorEmbed(locale.Text("commands.economy.this_command_can_only_be_used_within")))
		return
	}
	var targetUser *discordgo.User
	if i.Member != nil && i.Member.User != nil {
		targetUser = i.Member.User
	} else if i.User != nil {
		targetUser = i.User
	}
	options := i.ApplicationCommandData().Options
	if len(options) > 0 {
		targetUser = options[0].UserValue(s)
	}

	balance := database.GetBalance(i.GuildID, targetUser.ID)
	respondEmbed(s, i, utils.GoldEmbed(locale.Text("commands.slash_handlers.balance"), locale.Text("commands.slash_handlers.has.formatted", locale.Data{"Username": targetUser.Username, "Balance": balance, "CurrencyName": config.Bot.CurrencyName})))
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
	if i.GuildID == "" {
		respondEmbed(s, i, utils.ErrorEmbed(locale.Text("commands.economy.this_command_can_only_be_used_within")))
		return
	}
	options := i.ApplicationCommandData().Options
	toUser := options[0].UserValue(s)
	amount := int(options[1].IntValue())
	fromID := ""
	if i.Member != nil && i.Member.User != nil {
		fromID = i.Member.User.ID
	} else if i.User != nil {
		fromID = i.User.ID
	}

	if toUser.ID == fromID {
		respondEmbed(s, i, utils.ErrorEmbed(locale.Text("commands.slash_handlers.you_cannot_pay_yourself")))
		return
	}

	err := database.TransferCoins(i.GuildID, fromID, toUser.ID, amount)
	if err != nil {
		respondEmbed(s, i, utils.ErrorEmbed(locale.Text("commands.slash_handlers.insufficient_funds_or_transaction_error")))
		return
	}

	webhook.SendTransferNotification(fromID, toUser.ID, amount)

	respondEmbed(s, i, utils.SuccessEmbed(locale.Text("commands.slash_handlers.transfer_successful"), locale.Text("commands.slash_handlers.you_sent_to.formatted", locale.Data{"Amount": amount, "CurrencyName": config.Bot.CurrencyName, "Username": toUser.Username})))
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

	case "timeout":
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

		ExecuteLoanPay(s, i.Member.User.ID, loanID, i)

	case "list":
		ExecuteLoanList(s, i.Member.User, true, i)
	}
}

func handleSlashStock(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.GuildID == "" {
		respondEmbed(s, i, utils.ErrorEmbed(locale.Text("commands.economy.this_command_can_only_be_used_within")))
		return
	}
	options := i.ApplicationCommandData().Options
	if len(options) == 0 {
		return
	}

	userID := ""
	if i.Member != nil && i.Member.User != nil {
		userID = i.Member.User.ID
	} else if i.User != nil {
		userID = i.User.ID
	}
	subCommand := options[0].Name

	switch subCommand {
	case "market":
		respondEmbed(s, i, stockmarket.ExecuteMarket())
	case "buy":
		ticker := options[0].Options[0].StringValue()
		amount := int(options[0].Options[1].IntValue())
		respondEmbed(s, i, stockmarket.ExecuteBuy(i.GuildID, userID, ticker, amount))
	case "sell":
		ticker := options[0].Options[0].StringValue()
		shares := options[0].Options[1].StringValue()
		respondEmbed(s, i, stockmarket.ExecuteSell(i.GuildID, userID, ticker, shares))
	case "portfolio":
		respondEmbed(s, i, stockmarket.ExecutePortfolio(i.GuildID, userID))
	}
}

func handleSlashCrypto(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.GuildID == "" {
		respondEmbed(s, i, utils.ErrorEmbed(locale.Text("commands.economy.this_command_can_only_be_used_within")))
		return
	}
	options := i.ApplicationCommandData().Options
	if len(options) == 0 {
		return
	}

	userID := ""
	if i.Member != nil && i.Member.User != nil {
		userID = i.Member.User.ID
	} else if i.User != nil {
		userID = i.User.ID
	}
	subCommand := options[0].Name

	switch subCommand {
	case "market":
		respondEmbed(s, i, crypto.ExecuteCryptoMarket())
	case "buy":
		symbol := options[0].Options[0].StringValue()
		amount := int(options[0].Options[1].IntValue())
		respondEmbed(s, i, crypto.ExecuteCryptoBuy(i.GuildID, userID, symbol, amount))
	case "sell":
		symbol := options[0].Options[0].StringValue()
		coins := options[0].Options[1].StringValue()
		respondEmbed(s, i, crypto.ExecuteCryptoSell(i.GuildID, userID, symbol, coins))
	case "portfolio":
		respondEmbed(s, i, crypto.ExecuteCryptoPortfolio(i.GuildID, userID))
	}
}

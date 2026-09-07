package commands

import (
	"estudocoin/internal/crypto"
	"estudocoin/internal/database"
	"estudocoin/internal/games"
	"estudocoin/internal/stockmarket"
	"estudocoin/internal/webhook"
	"estudocoin/pkg/config"
	"estudocoin/pkg/utils"
	"fmt"
	"time"

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
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Embeds: []*discordgo.MessageEmbed{utils.ErrorEmbed("❌ This bot can only be used in designated channels.")},
				Flags:  discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	switch i.ApplicationCommandData().Name {
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
	case "loan":
		handleSlashLoan(s, i)
	case "stock":
		handleSlashStock(s, i)
	case "crypto":
		handleSlashCrypto(s, i)
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
	users, err := database.GetLeaderboard(10)
	if err != nil {
		respondEmbed(s, i, utils.ErrorEmbed("Could not retrieve leaderboard."))
		return
	}

	if len(users) == 0 {
		respondEmbed(s, i, utils.InfoEmbed("Leaderboard", "No users found."))
		return
	}

	var description string
	for i, u := range users {
		// Try to get user from cache or API to display name
		discordUser, err := s.User(u.ID)
		name := u.ID
		if err == nil {
			name = discordUser.Username
		}
		
		// Show total net worth with details
		description += fmt.Sprintf("**%d.** %s - **%d %s** 💰 (🪙 %d | 📈 %d)\n", 
			i+1, name, u.TotalNetWorth, config.Bot.CurrencyName, u.Balance, u.StockValue)
	}
	
	description += "\n💰 = Total | 🪙 = Wallet | 📈 = Stocks"

	respondEmbed(s, i, utils.GoldEmbed("🏆 Richest Users (Net Worth)", description))
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
	sym := config.Bot.CurrencySymbol
	desc := fmt.Sprintf("**Available Items:**\n\n"+
		"1. **Change Own Nickname**\n"+
		"   Cost: %d %s\n"+
		"   Command: `/buy nickname new_name:...`\n\n"+
		"2. **Change Other's Nickname**\n"+
		"   Cost: %d %s\n"+
		"   Command: `/buy rename user:... new_name:...`\n\n"+
		"3. **Mute/Timeout User**\n"+
		"   Cost: %d %s per minute\n"+
		"   Command: `/buy mute user:... minutes:...`",
		config.Economy.CostNicknameSelf, sym, config.Economy.CostNicknameOther, sym, config.Economy.CostPerMinuteMute, sym)

	respondEmbed(s, i, utils.GoldEmbed(fmt.Sprintf("🛒 %s Shop", config.Bot.BotName), desc))
}

func handleSlashBuy(s *discordgo.Session, i *discordgo.InteractionCreate) {
	options := i.ApplicationCommandData().Options
	subCommand := options[0].Name
	userID := i.Member.User.ID
	guildID := i.GuildID

	switch subCommand {
	case "nickname":
		newName := options[0].Options[0].StringValue()
		
		if database.GetBalance(userID) < config.Economy.CostNicknameSelf {
			respondEmbed(s, i, utils.ErrorEmbed("Insufficient funds."))
			return
		}

		err := s.GuildMemberNickname(guildID, userID, newName)
		if err != nil {
			respondEmbed(s, i, utils.ErrorEmbed("Could not change nickname (check my permissions)."))
			return
		}

		database.CollectLostBet(userID, config.Economy.CostNicknameSelf)
		respondEmbed(s, i, utils.SuccessEmbed("Purchase Successful", "Your nickname has been changed!"))

	case "rename":
		targetUser := options[0].Options[0].UserValue(s)
		newName := options[0].Options[1].StringValue()

		if database.GetBalance(userID) < config.Economy.CostNicknameOther {
			respondEmbed(s, i, utils.ErrorEmbed("Insufficient funds."))
			return
		}

		err := s.GuildMemberNickname(guildID, targetUser.ID, newName)
		if err != nil {
			respondEmbed(s, i, utils.ErrorEmbed("Error changing nickname (check permissions/hierarchy)."))
			return
		}

		database.CollectLostBet(userID, config.Economy.CostNicknameOther)
		respondEmbed(s, i, utils.SuccessEmbed("Purchase Successful", fmt.Sprintf("Nickname of %s changed.", targetUser.Username)))

	case "mute":
		targetUser := options[0].Options[0].UserValue(s)
		minutes := int(options[0].Options[1].IntValue())
		
		cost := minutes * config.Economy.CostPerMinuteMute
		if database.GetBalance(userID) < cost {
			respondEmbed(s, i, utils.ErrorEmbed(fmt.Sprintf("Insufficient funds. Cost: %d %s.", cost, config.Bot.CurrencySymbol)))
			return
		}

		// Check if user is in an active game
		if games.IsUserInGame(targetUser.ID) {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Embeds: []*discordgo.MessageEmbed{utils.InfoEmbed("⏳ Waiting", 
						fmt.Sprintf("%s is in an active game. Waiting for the game to finish to apply punishment...", targetUser.Username))},
				},
			})
			
			// Wait for game to finish
			games.WaitForGameFinish(targetUser.ID)
			
			// Update to application message
			defer func() {
				s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
					Embeds: &[]*discordgo.MessageEmbed{utils.SuccessEmbed("Punishment Applied!", 
						fmt.Sprintf("%s has been timed out until %s.", targetUser.Username, time.Now().Add(time.Duration(minutes)*time.Minute).Format("15:04:05")))},
				})
			}()
		}

		// Check existing timeout
		member, err := s.GuildMember(guildID, targetUser.ID)
		if err != nil {
			respondEmbed(s, i, utils.ErrorEmbed("Member not found."))
			return
		}

		var until time.Time
		if member.CommunicationDisabledUntil != nil && member.CommunicationDisabledUntil.After(time.Now()) {
			until = member.CommunicationDisabledUntil.Add(time.Duration(minutes) * time.Minute)
		} else {
			until = time.Now().Add(time.Duration(minutes) * time.Minute)
		}

		err = s.GuildMemberTimeout(guildID, targetUser.ID, &until)
		if err != nil {
			respondEmbed(s, i, utils.ErrorEmbed("Error applying timeout (check permissions/hierarchy)."))
			return
		}

		database.CollectLostBet(userID, cost)
		respondEmbed(s, i, utils.SuccessEmbed("Silenced!", fmt.Sprintf("%s silenced until %s.", targetUser.Username, until.Format("15:04:05"))))
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
package commands

import (
	"estudocoin/internal/database"
	"estudocoin/internal/games"
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
	case "blackjack":
		handleSlashBlackjack(s, i)
	case "loan":
		handleSlashLoan(s, i)
	}
}

func handleSlashBet(s *discordgo.Session, i *discordgo.InteractionCreate) {
	options := i.ApplicationCommandData().Options
	subCommand := options[0].Name

	switch subCommand {
	case "aviator":
		amount := int(options[0].Options[0].IntValue())
		games.StartAviatorInteraction(s, i, amount)
	case "cups":
		amount := int(options[0].Options[0].IntValue())
		games.StartCupGameInteraction(s, i, amount)
	}
}

func handleSlashBlackjack(s *discordgo.Session, i *discordgo.InteractionCreate) {
	options := i.ApplicationCommandData().Options
	bet := int(options[0].IntValue())
	
	games.StartBlackjackGame(s, i, bet)
}

func handleSlashDaily(s *discordgo.Session, i *discordgo.InteractionCreate) {
	userID := i.Member.User.ID
	info := database.GetDailyStreakInfo(userID)

	if !info.CanClaim {
		discordTime := fmt.Sprintf("<t:%d:R>", info.NextDaily.Unix())
		respondEmbed(s, i, utils.ErrorEmbed(fmt.Sprintf("You already collected your daily reward! Come back %s.", discordTime)))
		return
	}

	info, err := database.ClaimDaily(userID)
	if err != nil {
		respondEmbed(s, i, utils.ErrorEmbed("Error claiming daily reward."))
		return
	}

	// Adiciona as moedas
	err = database.AddCoins(userID, info.Reward)
	if err != nil {
		respondEmbed(s, i, utils.ErrorEmbed("Error adding coins."))
		return
	}

	streakText := ""
	if info.Streak > 0 {
		streakText = fmt.Sprintf("\n\n🔥 **Streak: %d days**", info.Streak+1)
		if info.Streak+1 >= 50 {
			streakText += " (MAX)"
		}
	}
	if info.MaxStreak > 0 {
		streakText += fmt.Sprintf("\n🏆 Max Streak: %d", info.MaxStreak)
	}

	respondEmbed(s, i, utils.SuccessEmbed("Daily Collected!", 
		fmt.Sprintf("You received **%d %s**!%s", info.Reward, config.Bot.CurrencyName, streakText)))
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
		
description += fmt.Sprintf("**%d.** %s - **%d %s**\n", i+1, name, u.Balance, config.Bot.CurrencyName)
	}

	respondEmbed(s, i, utils.GoldEmbed("Richest Users", description))
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
		// Converter para formato compatível com CmdLoanOffer
		targetUser := options[0].Options[0].UserValue(s)
		amount := int(options[0].Options[1].IntValue())
		interest := options[0].Options[2].FloatValue()
		days := int(options[0].Options[3].IntValue())

		// Criar mensagem falsa para compatibilidade
		m := &discordgo.MessageCreate{
			Message: &discordgo.Message{
				Author:    i.Member.User,
				ChannelID: i.ChannelID,
				GuildID:   i.GuildID,
				Content:   fmt.Sprintf("/loan offer @%s %d %.2f %d", targetUser.Username, amount, interest, days),
			},
		}
		m.Mentions = []*discordgo.User{targetUser}

		// Criar args
		args := []string{"offer", fmt.Sprintf("%d", amount), fmt.Sprintf("%.2f", interest), fmt.Sprintf("%d", days)}

		CmdLoanOffer(s, m, args)

		// Responder que a oferta foi criada
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: fmt.Sprintf("📩 Loan offer sent to <@%s>!", targetUser.ID),
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})

	case "pay":
		loanID := ""
		if len(options[0].Options) > 0 {
			loanID = options[0].Options[0].StringValue()
		}

		m := &discordgo.MessageCreate{
			Message: &discordgo.Message{
				Author:    i.Member.User,
				ChannelID: i.ChannelID,
				GuildID:   i.GuildID,
				Content:   "!loan pay " + loanID,
			},
		}

		args := []string{"pay"}
		if loanID != "" {
			args = append(args, loanID)
		}

		CmdLoanPay(s, m, args)

		// Responder
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "💰 Processing loan payment...",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})

	case "list":
		m := &discordgo.MessageCreate{
			Message: &discordgo.Message{
				Author:    i.Member.User,
				ChannelID: i.ChannelID,
				GuildID:   i.GuildID,
				Content:   "!loan list",
			},
		}

		CmdLoanList(s, m, []string{"list"})

		// Responder
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "📋 Listing your active loans...",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
	}
}
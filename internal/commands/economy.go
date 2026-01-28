package commands

import (
	"estudocoin/internal/database"
	"estudocoin/internal/webhook"
	"estudocoin/pkg/config"
	"estudocoin/pkg/utils"
	"fmt"
	"strconv"

	"github.com/bwmarrin/discordgo"
)

func CmdDaily(s *discordgo.Session, m *discordgo.MessageCreate) {
	userID := m.Author.ID
	if !database.CanDaily(userID) {
		nextTime := database.GetNextDailyTime(userID)
		discordTime := fmt.Sprintf("<t:%d:R>", nextTime.Unix())
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed(fmt.Sprintf("You already collected your daily reward! Come back %s.", discordTime)))
		return
	}

	amount := config.Economy.DailyAmount
	err := database.AddCoins(userID, amount)
	if err != nil {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Error adding coins."))
		return
	}
	database.SetDaily(userID)

	s.ChannelMessageSendEmbed(m.ChannelID, utils.SuccessEmbed("Daily Collected!", fmt.Sprintf("You received **%d %s**!", amount, config.Bot.CurrencyName)))
}

func CmdBalance(s *discordgo.Session, m *discordgo.MessageCreate) {
	targetUser := m.Author
	if len(m.Mentions) > 0 {
		targetUser = m.Mentions[0]
	}

	balance := database.GetBalance(targetUser.ID)
	s.ChannelMessageSendEmbed(m.ChannelID, utils.GoldEmbed("Balance", fmt.Sprintf("**%s** has **%d %s**.", targetUser.Username, balance, config.Bot.CurrencyName)))
}

func CmdPay(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	if len(m.Mentions) == 0 || len(args) < 2 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.InfoEmbed("Usage", "!pay @user <amount>"))
		return
	}

	toUser := m.Mentions[0]
	if toUser.ID == m.Author.ID {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("You cannot pay yourself."))
		return
	}

	var amount int
	
	// Find amount in args
	found := false
	for _, arg := range args {
		if val, err := strconv.Atoi(arg); err == nil {
			amount = val
			found = true
			break
		}
	}
	
	if !found || amount <= 0 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Invalid amount."))
		return
	}

	err := database.TransferCoins(m.Author.ID, toUser.ID, amount)
	if err != nil {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Insufficient funds or transaction error."))
		return
	}

	// Trigger Webhook
	webhook.SendTransferNotification(m.Author.ID, toUser.ID, amount)

	s.ChannelMessageSendEmbed(m.ChannelID, utils.SuccessEmbed("Transfer Successful", fmt.Sprintf("You sent **%d %s** to **%s**.", amount, config.Bot.CurrencyName, toUser.Username)))
}
package commands

import (
	"estudocoin/internal/database"
	"estudocoin/internal/webhook"
	"estudocoin/pkg/config"
	"estudocoin/pkg/utils"
	"fmt"
	"log"
	"strconv"

	"github.com/bwmarrin/discordgo"
)

// ExecuteDaily processes a daily claim and returns a formatted Discord embed
func ExecuteDaily(userID string) *discordgo.MessageEmbed {
	info, err := database.ClaimDaily(userID)
	if err != nil {
		if info != nil && !info.CanClaim {
			discordTime := fmt.Sprintf("<t:%d:R>", info.NextDaily.Unix())
			return utils.ErrorEmbed(fmt.Sprintf("You already collected your daily reward! Come back %s.", discordTime))
		}
		return utils.ErrorEmbed("Error claiming daily reward. Please try again.")
	}

	dayUnit := "days"
	if info.Streak == 1 {
		dayUnit = "day"
	}
	streakText := fmt.Sprintf("\n\n🔥 **Streak: %d %s**", info.Streak, dayUnit)
	if info.Streak >= 50 {
		streakText += " (MAX)"
	}
	if info.IsNewRecord && info.Streak > 1 {
		streakText += " 🏆 **New Personal Record!**"
	} else if info.MaxStreak > 0 {
		streakText += fmt.Sprintf("\n🏆 Max Streak: %d days", info.MaxStreak)
	}

	if info.StreakReset {
		streakText += "\n⚠️ *Your previous streak was reset because more than 48 hours passed.*"
	}

	return utils.SuccessEmbed("Daily Collected!",
		fmt.Sprintf("You received **%d %s**!%s", info.Reward, config.Bot.CurrencyName, streakText))
}

func CmdDaily(s *discordgo.Session, m *discordgo.MessageCreate) {
	s.ChannelMessageSendEmbed(m.ChannelID, ExecuteDaily(m.Author.ID))
}

func CmdBalance(s *discordgo.Session, m *discordgo.MessageCreate) {
	targetUser := m.Author
	if len(m.Mentions) > 0 {
		targetUser = m.Mentions[0]
	}

	balance := database.GetBalance(targetUser.ID)
	
	// Debug log
	log.Printf("[BALANCE] User: %s (ID: %s), Balance: %d", targetUser.Username, targetUser.ID, balance)
	
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

func CmdLeaderboard(s *discordgo.Session, m *discordgo.MessageCreate) {
	users, err := database.GetLeaderboard(10)
	if err != nil {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Could not retrieve leaderboard."))
		return
	}

	if len(users) == 0 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.InfoEmbed("Leaderboard", "No users found."))
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

	s.ChannelMessageSendEmbed(m.ChannelID, utils.GoldEmbed("🏆 Richest Users (Net Worth)", description))
}
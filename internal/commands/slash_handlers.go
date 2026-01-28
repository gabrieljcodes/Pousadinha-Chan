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

	switch i.ApplicationCommandData().Name {
	case "help":
		HandleSlashHelp(s, i)
	case "daily":
		handleSlashDaily(s, i)
	case "balance":
		handleSlashBalance(s, i)
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

func handleSlashDaily(s *discordgo.Session, i *discordgo.InteractionCreate) {
	userID := i.Member.User.ID
	if !database.CanDaily(userID) {
		nextTime := database.GetNextDailyTime(userID)
		discordTime := fmt.Sprintf("<t:%d:R>", nextTime.Unix())
		respondEmbed(s, i, utils.ErrorEmbed(fmt.Sprintf("You already collected your daily reward! Come back %s.", discordTime)))
		return
	}

	amount := config.Economy.DailyAmount
	err := database.AddCoins(userID, amount)
	if err != nil {
		respondEmbed(s, i, utils.ErrorEmbed("Error adding coins."))
		return
	}
	database.SetDaily(userID)
	respondEmbed(s, i, utils.SuccessEmbed("Daily Collected!", fmt.Sprintf("You received **%d %s**!", amount, config.Bot.CurrencyName)))
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
	desc := fmt.Sprintf(`
**Available Items:**

1. **Change Own Nickname**
   Cost: %d %s
   Command: `+"`/buy nickname new_name:...`"+`

2. **Change Other's Nickname**
   Cost: %d %s
   Command: `+"`/buy rename user:... new_name:...`"+`

3. **Mute/Timeout User**
   Cost: %d %s per minute
   Command: `+"`/buy mute user:... minutes:...`"+`
`, config.Economy.CostNicknameSelf, sym, config.Economy.CostNicknameOther, sym, config.Economy.CostPerMinuteMute, sym)

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

		database.RemoveCoins(userID, config.Economy.CostNicknameSelf)
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

		database.RemoveCoins(userID, config.Economy.CostNicknameOther)
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

		database.RemoveCoins(userID, cost)
		respondEmbed(s, i, utils.SuccessEmbed("Silenced!", fmt.Sprintf("%s silenced until %s.", targetUser.Username, until.Format("15:04:05"))))
	}
}

package valorant

import (
    "estudocoin/internal/database"
    "estudocoin/pkg/config"
    "estudocoin/pkg/utils"
    "fmt"
    "strconv"
    "strings"

    "github.com/bwmarrin/discordgo"
)

func CmdValorantBet(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
    if len(args) < 3 {
        s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Usage: !valorantbet <RiotID#TAG> <amount> <loss> (ex: Player#BR1 200 loss)"))
        return
    }

    riotID := args[0]
    amountStr := args[1]
    betType := strings.ToLower(args[2])

    amount, err := strconv.Atoi(amountStr)
    if err != nil || amount < config.Economy.ValorantMinBet {
        s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed(fmt.Sprintf("Invalid amount (min %d).", config.Economy.ValorantMinBet)))
        return
    }

    balance := database.GetBalance(m.Author.ID)
    if balance < amount {
        s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Insufficient funds."))
        return
    }

    betOnLoss := betType == "loss"

    // Deduzir saldo
    database.RemoveCoins(m.Author.ID, amount)

    // Salvar aposta
    bet := ValorantBet{
        UserID:    m.Author.ID,
        RiotID:    riotID,
        BetOnLoss: betOnLoss,
        Amount:    amount,
    }
    err = database.AddValorantBet(bet)
    if err != nil {
        database.AddCoins(m.Author.ID, amount) // Refund
        s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Error saving bet."))
        return
    }

    s.ChannelMessageSendEmbed(m.ChannelID, utils.SuccessEmbed("Bet Placed!", fmt.Sprintf("You bet %d on %s %s the next match.", amount, riotID, betType)))
}
package stockmarket

import (
	"estudocoin/internal/database"
	"estudocoin/pkg/config"
	"estudocoin/pkg/utils"
	"fmt"
	"strconv"
	"strings"

	"github.com/bwmarrin/discordgo"
)

func CmdStock(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	if len(args) == 0 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.InfoEmbed("Stock Market", "Usage: `!stock <market|buy|sell|portfolio>`"))
		return
	}

	subcmd := strings.ToLower(args[0])

	switch subcmd {
	case "market", "list":
		handleMarket(s, m)
	case "buy":
		handleBuy(s, m, args[1:])
	case "sell":
		handleSell(s, m, args[1:])
	case "portfolio", "p":
		handlePortfolio(s, m)
	default:
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Unknown subcommand. Use `market`, `buy`, `sell`, or `portfolio`."))
	}
}

func handleMarket(s *discordgo.Session, m *discordgo.MessageCreate) {
	var sb strings.Builder
	sb.WriteString("Current Market Prices (Updates every 10m):\n\n")

	for _, company := range Companies {
		price, _ := database.GetStockPriceDB(company.Ticker)
		// If price is 0, maybe try to fetch it now or just show "Loading..."
		priceStr := fmt.Sprintf("%.2f", price)
		if price == 0 {
			priceStr = "Fetching..."
		}
		sb.WriteString(fmt.Sprintf("**%s** (%s): %s %s\n", company.Name, company.Ticker, priceStr, config.Bot.CurrencyName))
	}

	s.ChannelMessageSendEmbed(m.ChannelID, utils.GoldEmbed("Stock Market", sb.String()))
}

func handleBuy(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	if len(args) < 2 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Usage: `!stock buy <ticker> <amount>`"))
		return
	}

	ticker := strings.ToUpper(args[0])
	amountStr := args[1]
	amount, err := strconv.Atoi(amountStr)
	if err != nil || amount <= 0 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Invalid amount."))
		return
	}

	// Verify ticker
	valid := false
	for _, c := range Companies {
		if c.Ticker == ticker {
			valid = true
			break
		}
	}
	if !valid {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Invalid Ticker. Check `!stock market`."))
		return
	}

	// Check Balance
	balance := database.GetBalance(m.Author.ID)
	if balance < amount {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Insufficient funds."))
		return
	}

	// Get Price
	price, err := database.GetStockPriceDB(ticker)
	if err != nil || price <= 0 {
		// Try fetching live if DB is empty
		data, err := GetStockPrice(ticker)
		if err != nil {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Could not fetch stock price. Try again later."))
			return
		}
		price = data.Price
		database.SetStockPriceDB(ticker, price)
	}

	shares := float64(amount) / price

	// Transaction
	if err := database.RemoveCoins(m.Author.ID, amount); err != nil {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Transaction failed."))
		return
	}

	if err := database.AddShares(m.Author.ID, ticker, shares); err != nil {
		// Refund
		database.AddCoins(m.Author.ID, amount)
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Database error. Refunded."))
		return
	}

	s.ChannelMessageSendEmbed(m.ChannelID, utils.SuccessEmbed("Investment Successful", fmt.Sprintf("You bought **%.4f** shares of **%s** for **%d %s**.", shares, ticker, amount, config.Bot.CurrencyName)))
}

func handleSell(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	if len(args) < 2 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Usage: `!stock sell <ticker> <shares>` (To sell by amount of coins is hard due to price fluctuations, so selling Shares is safer)"))
		// Actually, let's stick to selling SHARES to be precise, or handle "sell all".
		// But earlier I thought about selling by amount.
		// "Sell 100 coins worth" -> calculates shares = 100 / current_price.
		// "Sell 2 shares" -> 2 * current_price.
		// Let's check the args. If it's "all", sell all.
		return
	}

	ticker := strings.ToUpper(args[0])
	amountStr := args[1] // Can be "all" or a number (shares)

	// Verify ticker
	// ... (same check)
	valid := false
	for _, c := range Companies {
		if c.Ticker == ticker {
			valid = true
			break
		}
	}
	if !valid {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Invalid Ticker."))
		return
	}

	ownedShares, _ := database.GetInvestment(m.Author.ID, ticker)
	if ownedShares <= 0 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("You don't own any shares of this company."))
		return
	}

	var sharesToSell float64

	if strings.ToLower(amountStr) == "all" {
		sharesToSell = ownedShares
	} else {
		val, err := strconv.ParseFloat(amountStr, 64)
		if err != nil || val <= 0 {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Invalid number of shares."))
			return
		}
		sharesToSell = val
	}

	if sharesToSell > ownedShares {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("You don't have that many shares."))
		return
	}

	// Get Price
	price, err := database.GetStockPriceDB(ticker)
	if err != nil || price <= 0 {
		data, err := GetStockPrice(ticker)
		if err != nil {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Could not fetch stock price."))
			return
		}
		price = data.Price
		database.SetStockPriceDB(ticker, price)
	}

	payout := int(sharesToSell * price)

	if err := database.RemoveShares(m.Author.ID, ticker, sharesToSell); err != nil {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Database error."))
		return
	}

	database.AddCoins(m.Author.ID, payout)
	s.ChannelMessageSendEmbed(m.ChannelID, utils.SuccessEmbed("Sale Successful", fmt.Sprintf("You sold **%.4f** shares of **%s** for **%d %s**.", sharesToSell, ticker, payout, config.Bot.CurrencyName)))
}

func handlePortfolio(s *discordgo.Session, m *discordgo.MessageCreate) {
	investments, err := database.GetAllInvestmentsByUser(m.Author.ID)
	if err != nil {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Database error."))
		return
	}

	if len(investments) == 0 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.InfoEmbed("Portfolio", "You have no investments."))
		return
	}

	var sb strings.Builder
	totalVal := 0.0

	for _, inv := range investments {
		price, _ := database.GetStockPriceDB(inv.Ticker)
		val := inv.Shares * price
		totalVal += val
		sb.WriteString(fmt.Sprintf("**%s**: %.4f shares (~%d %s)\n", inv.Ticker, inv.Shares, int(val), config.Bot.CurrencyName))
	}

	sb.WriteString(fmt.Sprintf("\n**Total Value**: ~%d %s", int(totalVal), config.Bot.CurrencyName))
	s.ChannelMessageSendEmbed(m.ChannelID, utils.GoldEmbed("Your Portfolio", sb.String()))
}

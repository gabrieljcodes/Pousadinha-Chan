package stockmarket

import (
	"estudocoin/internal/database"
	"estudocoin/pkg/config"
	"estudocoin/pkg/utils"
	"fmt"
	"math"
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
		s.ChannelMessageSendEmbed(m.ChannelID, ExecuteMarket())
	case "buy":
		if len(args) < 3 {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Usage: `!stock buy <ticker> <amount>`"))
			return
		}
		amount, err := strconv.Atoi(args[2])
		if err != nil || amount <= 0 {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Invalid amount."))
			return
		}
		s.ChannelMessageSendEmbed(m.ChannelID, ExecuteBuy(m.Author.ID, args[1], amount))
	case "sell":
		if len(args) < 3 {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Usage: `!stock sell <ticker> <shares|all>`"))
			return
		}
		s.ChannelMessageSendEmbed(m.ChannelID, ExecuteSell(m.Author.ID, args[1], args[2]))
	case "portfolio", "p":
		s.ChannelMessageSendEmbed(m.ChannelID, ExecutePortfolio(m.Author.ID))
	default:
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Unknown subcommand. Use `market`, `buy`, `sell`, or `portfolio`."))
	}
}

// ExecuteMarket returns current market stock prices embed
func ExecuteMarket() *discordgo.MessageEmbed {
	var sb strings.Builder
	sb.WriteString("Current Market Prices (Real-Time Yahoo Finance):\n\n")

	for _, company := range Companies {
		price, _ := database.GetStockPriceDB(company.Ticker)
		if price <= 0 {
			data, err := GetStockPrice(company.Ticker)
			if err == nil {
				price = data.Price
				_ = database.SetStockPriceDB(company.Ticker, price)
			}
		}

		priceStr := fmt.Sprintf("%.2f", price)
		if price <= 0 {
			priceStr = "Fetching..."
		}
		sb.WriteString(fmt.Sprintf("**%s** (%s): $%s\n", company.Name, company.Ticker, priceStr))
	}

	return utils.GoldEmbed("Stock Market", sb.String())
}

// ExecuteBuy processes a stock purchase and returns an embed response
func ExecuteBuy(userID, ticker string, amount int) *discordgo.MessageEmbed {
	ticker = strings.ToUpper(strings.TrimSpace(ticker))
	if amount <= 0 {
		return utils.ErrorEmbed("Invalid amount.")
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
		return utils.ErrorEmbed("Invalid Ticker. Check `/stock market` or `!stock market`.")
	}

	// Check Balance
	balance := database.GetBalance(userID)
	if balance < amount {
		return utils.ErrorEmbed("Insufficient funds.")
	}

	// Get Price
	price, err := database.GetStockPriceDB(ticker)
	if err != nil || price <= 0 {
		data, err := GetStockPrice(ticker)
		if err != nil {
			return utils.ErrorEmbed("Could not fetch stock price. Try again later.")
		}
		price = data.Price
		_ = database.SetStockPriceDB(ticker, price)
	}

	shares := float64(amount) / price

	// Transaction
	if err := database.RemoveCoins(userID, amount); err != nil {
		return utils.ErrorEmbed("Transaction failed.")
	}

	if err := database.AddShares(userID, ticker, shares, amount); err != nil {
		// Refund
		_ = database.AddCoins(userID, amount)
		return utils.ErrorEmbed("Database error. Refunded.")
	}

	return utils.SuccessEmbed("Investment Successful",
		fmt.Sprintf("You bought **%.4f** shares of **%s** for **%d %s** (at $%.2f/share).",
			shares, ticker, amount, config.Bot.CurrencyName, price))
}

// ExecuteSell processes a stock sale and returns an embed response
func ExecuteSell(userID, ticker, amountStr string) *discordgo.MessageEmbed {
	ticker = strings.ToUpper(strings.TrimSpace(ticker))

	// Verify ticker
	valid := false
	for _, c := range Companies {
		if c.Ticker == ticker {
			valid = true
			break
		}
	}
	if !valid {
		return utils.ErrorEmbed("Invalid Ticker.")
	}

	ownedShares, _ := database.GetInvestment(userID, ticker)
	if ownedShares <= 0 {
		return utils.ErrorEmbed("You don't own any shares of this company.")
	}

	var sharesToSell float64

	if strings.ToLower(amountStr) == "all" {
		sharesToSell = ownedShares
	} else {
		val, err := strconv.ParseFloat(amountStr, 64)
		if err != nil || val <= 0 {
			return utils.ErrorEmbed("Invalid number of shares.")
		}
		sharesToSell = val
	}

	if sharesToSell > ownedShares {
		return utils.ErrorEmbed(fmt.Sprintf("You don't have that many shares. You own **%.4f**.", ownedShares))
	}

	// Get Price
	price, err := database.GetStockPriceDB(ticker)
	if err != nil || price <= 0 {
		data, err := GetStockPrice(ticker)
		if err != nil {
			return utils.ErrorEmbed("Could not fetch stock price.")
		}
		price = data.Price
		_ = database.SetStockPriceDB(ticker, price)
	}

	payout := int(math.Round(sharesToSell * price))

	if err := database.RemoveShares(userID, ticker, sharesToSell); err != nil {
		return utils.ErrorEmbed("Database error.")
	}

	_ = database.AddCoins(userID, payout)
	return utils.SuccessEmbed("Sale Successful",
		fmt.Sprintf("You sold **%.4f** shares of **%s** for **%d %s** (at $%.2f/share).",
			sharesToSell, ticker, payout, config.Bot.CurrencyName, price))
}

// ExecutePortfolio generates a user's stock portfolio embed
func ExecutePortfolio(userID string) *discordgo.MessageEmbed {
	investments, err := database.GetAllInvestmentsByUser(userID)
	if err != nil {
		return utils.ErrorEmbed("Database error.")
	}

	if len(investments) == 0 {
		return utils.InfoEmbed("Portfolio", "You have no stock investments.")
	}

	var sb strings.Builder
	totalVal := 0.0
	totalInvested := 0.0

	for _, inv := range investments {
		if inv.Shares <= 0.000001 {
			continue
		}

		price, _ := database.GetStockPriceDB(inv.Ticker)
		if price <= 0 {
			data, err := GetStockPrice(inv.Ticker)
			if err == nil {
				price = data.Price
				_ = database.SetStockPriceDB(inv.Ticker, price)
			}
		}

		val := inv.Shares * price
		cost := inv.TotalInvested
		totalVal += val
		totalInvested += cost

		pnl := val - cost
		pnlPct := 0.0
		if cost > 0 {
			pnlPct = (pnl / cost) * 100.0
		}

		pnlStr := ""
		if pnl > 0.5 {
			pnlStr = fmt.Sprintf("+%d %s (+%.1f%%) 🟢", int(math.Round(pnl)), config.Bot.CurrencyName, pnlPct)
		} else if pnl < -0.5 {
			pnlStr = fmt.Sprintf("-%d %s (%.1f%%) 🔴", int(math.Round(-pnl)), config.Bot.CurrencyName, pnlPct)
		} else {
			pnlStr = fmt.Sprintf("0 %s (0.0%%) ⚪", config.Bot.CurrencyName)
		}

		sb.WriteString(fmt.Sprintf("**%s**: %.4f shares (~%d %s @ $%.2f)\n  ↳ Invested: %d %s | P/L: %s\n",
			inv.Ticker, inv.Shares, int(math.Round(val)), config.Bot.CurrencyName, price,
			int(math.Round(cost)), config.Bot.CurrencyName, pnlStr))
	}

	overallPnl := totalVal - totalInvested
	overallPct := 0.0
	if totalInvested > 0 {
		overallPct = (overallPnl / totalInvested) * 100.0
	}
	overallPnlStr := ""
	if overallPnl > 0.5 {
		overallPnlStr = fmt.Sprintf("+%d %s (+%.1f%%) 🟢", int(math.Round(overallPnl)), config.Bot.CurrencyName, overallPct)
	} else if overallPnl < -0.5 {
		overallPnlStr = fmt.Sprintf("-%d %s (%.1f%%) 🔴", int(math.Round(-overallPnl)), config.Bot.CurrencyName, overallPct)
	} else {
		overallPnlStr = fmt.Sprintf("0 %s (0.0%%) ⚪", config.Bot.CurrencyName)
	}

	sb.WriteString(fmt.Sprintf("\n**Total Value**: ~%d %s\n**Total Invested**: %d %s\n**Net Return**: %s",
		int(math.Round(totalVal)), config.Bot.CurrencyName,
		int(math.Round(totalInvested)), config.Bot.CurrencyName,
		overallPnlStr))

	return utils.GoldEmbed("Your Stock Portfolio", sb.String())
}

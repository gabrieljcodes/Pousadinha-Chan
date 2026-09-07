package crypto

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

func CmdCrypto(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	if len(args) == 0 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.InfoEmbed("Crypto Market", "Usage: `!crypto <market|buy|sell|portfolio>`"))
		return
	}

	subcmd := strings.ToLower(args[0])

	switch subcmd {
	case "market", "list", "prices":
		s.ChannelMessageSendEmbed(m.ChannelID, ExecuteCryptoMarket())
	case "buy":
		if len(args) < 3 {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Usage: `!crypto buy <SYMBOL> <amount>`\nExample: `!crypto buy BTC 1000`"))
			return
		}
		amount, err := strconv.Atoi(args[2])
		if err != nil || amount <= 0 {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Invalid amount."))
			return
		}
		s.ChannelMessageSendEmbed(m.ChannelID, ExecuteCryptoBuy(m.Author.ID, args[1], amount))
	case "sell":
		if len(args) < 3 {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Usage: `!crypto sell <SYMBOL> <amount|all>`\nExample: `!crypto sell BTC all` or `!crypto sell BTC 0.5`"))
			return
		}
		s.ChannelMessageSendEmbed(m.ChannelID, ExecuteCryptoSell(m.Author.ID, args[1], args[2]))
	case "portfolio", "p":
		s.ChannelMessageSendEmbed(m.ChannelID, ExecuteCryptoPortfolio(m.Author.ID))
	default:
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Unknown subcommand. Use `market`, `buy`, `sell`, or `portfolio`."))
	}
}

// ExecuteCryptoMarket returns the crypto market prices embed
func ExecuteCryptoMarket() *discordgo.MessageEmbed {
	prices, err := GetCryptoPrices()
	if err != nil {
		return utils.ErrorEmbed("Error fetching crypto prices. Try again later.")
	}

	var sb strings.Builder
	sb.WriteString("**Major Cryptocurrencies:**\n")
	for _, c := range AvailableCryptos {
		if c.Type != "major" {
			continue
		}
		price := prices[c.ID]
		if price > 0 {
			priceStr := formatPrice(price)
			sb.WriteString(fmt.Sprintf("**%s** (%s): $%s\n", c.Name, c.Symbol, priceStr))
		}
	}

	sb.WriteString("\n**Meme Coins (High Volatility!):**\n")
	for _, c := range AvailableCryptos {
		if c.Type != "meme" {
			continue
		}
		price := prices[c.ID]
		if price > 0 {
			priceStr := formatPrice(price)
			sb.WriteString(fmt.Sprintf("**%s** (%s): $%s\n", c.Name, c.Symbol, priceStr))
		}
	}

	return utils.GoldEmbed("Crypto Market", sb.String())
}

func formatPrice(price float64) string {
	if price >= 1 {
		return fmt.Sprintf("%.2f", price)
	} else if price >= 0.01 {
		return fmt.Sprintf("%.4f", price)
	} else if price >= 0.0001 {
		return fmt.Sprintf("%.6f", price)
	}
	return fmt.Sprintf("%.8f", price)
}

// ExecuteCryptoBuy processes a cryptocurrency purchase and returns an embed response
func ExecuteCryptoBuy(userID, symbol string, amount int) *discordgo.MessageEmbed {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if amount <= 0 {
		return utils.ErrorEmbed("Invalid amount.")
	}

	// Check if crypto exists
	c := GetCryptoBySymbol(symbol)
	if c == nil {
		return utils.ErrorEmbed("Invalid cryptocurrency symbol. Use `/crypto market` or `!crypto market` to see available options.")
	}

	// Check balance
	balance := database.GetBalance(userID)
	if balance < amount {
		return utils.ErrorEmbed("Insufficient funds.")
	}

	// Fetch current price
	price, err := GetSingleCryptoPrice(c.ID)
	if err != nil || price <= 0 {
		return utils.ErrorEmbed("Could not fetch crypto price. Try again later.")
	}

	// Calculate coins quantity
	coins := float64(amount) / price

	// Transaction
	if err := database.RemoveCoins(userID, amount); err != nil {
		return utils.ErrorEmbed("Transaction failed.")
	}

	if err := database.AddCryptoShares(userID, symbol, coins, amount); err != nil {
		// Refund
		_ = database.AddCoins(userID, amount)
		return utils.ErrorEmbed("Database error. Refunded.")
	}

	// Special message for meme coins
	emoji := "🚀"
	warning := ""
	if c.Type == "meme" {
		emoji = "🎰"
		warning = "\n⚠️ **Meme coins are highly volatile! Invest at your own risk.**"
	}

	return utils.SuccessEmbed("Crypto Purchase Successful!",
		fmt.Sprintf("%s You bought **%s %s** for **%d %s** (at $%s/coin).%s",
			emoji, formatCryptoAmount(coins), symbol, amount, config.Bot.CurrencyName, formatPrice(price), warning))
}

// ExecuteCryptoSell processes a cryptocurrency sale and returns an embed response
func ExecuteCryptoSell(userID, symbol, amountStr string) *discordgo.MessageEmbed {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))

	// Check if crypto exists
	c := GetCryptoBySymbol(symbol)
	if c == nil {
		return utils.ErrorEmbed("Invalid cryptocurrency symbol.")
	}

	// Check owned amount
	ownedCoins, _ := database.GetCryptoInvestment(userID, symbol)
	if ownedCoins <= 0 {
		return utils.ErrorEmbed(fmt.Sprintf("You don't own any %s.", symbol))
	}

	var coinsToSell float64

	if strings.ToLower(amountStr) == "all" {
		coinsToSell = ownedCoins
	} else {
		val, err := strconv.ParseFloat(amountStr, 64)
		if err != nil || val <= 0 {
			return utils.ErrorEmbed("Invalid amount.")
		}
		coinsToSell = val
	}

	if coinsToSell > ownedCoins {
		return utils.ErrorEmbed(fmt.Sprintf("You only own %s %s.", formatCryptoAmount(ownedCoins), symbol))
	}

	// Fetch current price
	price, err := GetSingleCryptoPrice(c.ID)
	if err != nil || price <= 0 {
		return utils.ErrorEmbed("Could not fetch crypto price. Try again later.")
	}

	payout := int(math.Round(coinsToSell * price))

	if err := database.RemoveCryptoShares(userID, symbol, coinsToSell); err != nil {
		return utils.ErrorEmbed("Database error.")
	}

	_ = database.AddCoins(userID, payout)

	emoji := "💰"
	if c.Type == "meme" {
		emoji = "🎰"
	}

	return utils.SuccessEmbed("Crypto Sale Successful!",
		fmt.Sprintf("%s You sold **%s %s** for **%d %s** (at $%s/coin).",
			emoji, formatCryptoAmount(coinsToSell), symbol, payout, config.Bot.CurrencyName, formatPrice(price)))
}

// ExecuteCryptoPortfolio generates a user's crypto portfolio embed
func ExecuteCryptoPortfolio(userID string) *discordgo.MessageEmbed {
	investments, err := database.GetAllCryptoInvestmentsByUser(userID)
	if err != nil {
		return utils.ErrorEmbed("Database error.")
	}

	if len(investments) == 0 {
		return utils.InfoEmbed("Crypto Portfolio", "You have no cryptocurrency investments.")
	}

	// Fetch current prices
	prices, err := GetCryptoPrices()
	if err != nil {
		return utils.ErrorEmbed("Could not fetch current prices.")
	}

	var sb strings.Builder
	totalValue := 0.0
	totalInvested := 0.0

	for _, inv := range investments {
		c := GetCryptoBySymbol(inv.Symbol)
		if c == nil {
			continue
		}

		price := prices[c.ID]
		if price <= 0 {
			continue
		}

		val := inv.Coins * price
		cost := inv.TotalInvested
		totalValue += val
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

		statusEmoji := "🟢"
		if c.Type == "meme" {
			statusEmoji = "🎰"
		}

		sb.WriteString(fmt.Sprintf("%s **%s** (%s): %s coins (~%d %s @ $%s)\n  ↳ Invested: %d %s | P/L: %s\n",
			statusEmoji, c.Name, inv.Symbol, formatCryptoAmount(inv.Coins),
			int(math.Round(val)), config.Bot.CurrencyName, formatPrice(price),
			int(math.Round(cost)), config.Bot.CurrencyName, pnlStr))
	}

	overallPnl := totalValue - totalInvested
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
		int(math.Round(totalValue)), config.Bot.CurrencyName,
		int(math.Round(totalInvested)), config.Bot.CurrencyName,
		overallPnlStr))

	return utils.GoldEmbed("Your Crypto Portfolio", sb.String())
}

func formatCryptoAmount(amount float64) string {
	if amount >= 1 {
		return fmt.Sprintf("%.4f", amount)
	} else if amount >= 0.0001 {
		return fmt.Sprintf("%.6f", amount)
	}
	return fmt.Sprintf("%.8f", amount)
}

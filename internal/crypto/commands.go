package crypto

import (
	"bot/internal/database"
	"bot/internal/locale"
	"bot/pkg/config"
	"bot/pkg/utils"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/bwmarrin/discordgo"
)

// ExecuteCryptoMarket returns the crypto market prices embed
func ExecuteCryptoMarket() *discordgo.MessageEmbed {
	prices, err := GetCryptoPrices()
	if err != nil {
		return utils.ErrorEmbed(locale.Text("crypto.commands.error_fetching_crypto_prices_try_again_later"))
	}

	var sb strings.Builder
	sb.WriteString(locale.Text("crypto.commands.major_cryptocurrencies"))
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

	sb.WriteString(locale.Text("crypto.commands.meme_coins_high_volatility"))
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

	return utils.GoldEmbed(locale.Text("crypto.commands.crypto_market"), sb.String())
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
func ExecuteCryptoBuy(guildID, userID, symbol string, amount int) *discordgo.MessageEmbed {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if amount <= 0 {
		return utils.ErrorEmbed(locale.Text("crypto.commands.invalid_amount"))
	}

	// Check if crypto exists
	c := GetCryptoBySymbol(symbol)
	if c == nil {
		return utils.ErrorEmbed(locale.Text("crypto.commands.invalid_cryptocurrency_symbol_use_crypto_market_or"))
	}

	// Check balance
	balance := database.GetBalance(guildID, userID)
	if balance < amount {
		return utils.ErrorEmbed(locale.Text("crypto.commands.insufficient_funds"))
	}

	// Fetch current price
	price, err := GetSingleCryptoPrice(c.ID)
	if err != nil || price <= 0 {
		return utils.ErrorEmbed(locale.Text("crypto.commands.could_not_fetch_crypto_price_try_again"))
	}

	// Calculate coins quantity
	coins := float64(amount) / price

	// Transaction
	if err := database.RemoveCoins(guildID, userID, amount); err != nil {
		return utils.ErrorEmbed(locale.Text("crypto.commands.transaction_failed"))
	}

	if err := database.AddCryptoShares(guildID, userID, symbol, coins, amount); err != nil {
		// Refund
		_ = database.AddCoins(guildID, userID, amount)
		return utils.ErrorEmbed(locale.Text("crypto.commands.database_error_refunded"))
	}

	// Special message for meme coins
	emoji := "🚀"
	warning := ""
	if c.Type == "meme" {
		emoji = "🎰"
		warning = locale.Text("crypto.commands.meme_coins_are_highly_volatile_invest_at")
	}

	return utils.SuccessEmbed(locale.Text("crypto.commands.crypto_purchase_successful"),
		locale.Text("crypto.commands.you_bought_for_at_coin.formatted", locale.Data{"Emoji": emoji, "Value2": formatCryptoAmount(coins), "Symbol": symbol, "Amount": amount, "CurrencyName": config.Bot.CurrencyName, "Value6": formatPrice(price), "Warning": warning}))
}

// ExecuteCryptoSell processes a cryptocurrency sale and returns an embed response
func ExecuteCryptoSell(guildID, userID, symbol, amountStr string) *discordgo.MessageEmbed {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))

	// Check if crypto exists
	c := GetCryptoBySymbol(symbol)
	if c == nil {
		return utils.ErrorEmbed(locale.Text("crypto.commands.invalid_cryptocurrency_symbol"))
	}

	// Check owned amount
	ownedCoins, _ := database.GetCryptoInvestment(guildID, userID, symbol)
	if ownedCoins <= 0 {
		return utils.ErrorEmbed(locale.Text("crypto.commands.you_don_t_own_any.formatted", locale.Data{"Symbol": symbol}))
	}

	var coinsToSell float64

	if strings.ToLower(amountStr) == "all" {
		coinsToSell = ownedCoins
	} else {
		val, err := strconv.ParseFloat(amountStr, 64)
		if err != nil || val <= 0 {
			return utils.ErrorEmbed(locale.Text("crypto.commands.invalid_amount"))
		}
		coinsToSell = val
	}

	if coinsToSell > ownedCoins {
		return utils.ErrorEmbed(locale.Text("crypto.commands.you_only_own.formatted", locale.Data{"Value1": formatCryptoAmount(ownedCoins), "Symbol": symbol}))
	}

	// Fetch current price
	price, err := GetSingleCryptoPrice(c.ID)
	if err != nil || price <= 0 {
		return utils.ErrorEmbed(locale.Text("crypto.commands.could_not_fetch_crypto_price_try_again"))
	}

	payout := int(math.Round(coinsToSell * price))

	if err := database.RemoveCryptoShares(guildID, userID, symbol, coinsToSell); err != nil {
		return utils.ErrorEmbed(locale.Text("crypto.commands.database_error"))
	}

	_ = database.AddCoins(guildID, userID, payout)

	emoji := "💰"
	if c.Type == "meme" {
		emoji = "🎰"
	}

	return utils.SuccessEmbed(locale.Text("crypto.commands.crypto_sale_successful"),
		locale.Text("crypto.commands.you_sold_for_at_coin.formatted", locale.Data{"Emoji": emoji, "Value2": formatCryptoAmount(coinsToSell), "Symbol": symbol, "Payout": payout, "CurrencyName": config.Bot.CurrencyName, "Value6": formatPrice(price)}))
}

// ExecuteCryptoPortfolio generates a user's crypto portfolio embed
func ExecuteCryptoPortfolio(guildID, userID string) *discordgo.MessageEmbed {
	investments, err := database.GetAllCryptoInvestmentsByUser(guildID, userID)
	if err != nil {
		return utils.ErrorEmbed(locale.Text("crypto.commands.database_error"))
	}

	if len(investments) == 0 {
		return utils.InfoEmbed(locale.Text("crypto.commands.crypto_portfolio"), locale.Text("crypto.commands.you_have_no_cryptocurrency_investments"))
	}

	// Fetch current prices
	prices, err := GetCryptoPrices()
	if err != nil {
		return utils.ErrorEmbed(locale.Text("crypto.commands.could_not_fetch_current_prices"))
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

		sb.WriteString(locale.Text("crypto.commands.coins_invested_p_l.formatted", locale.Data{"StatusEmoji": statusEmoji, "Name": c.Name, "Symbol": inv.Symbol, "Value4": formatCryptoAmount(inv.Coins), "Value5": int(math.Round(val)), "CurrencyName": config.Bot.CurrencyName, "Value7": formatPrice(price), "Value8": int(math.Round(cost)), "CurrencyName9": config.Bot.CurrencyName, "PnlStr": pnlStr}))
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

	sb.WriteString(locale.Text("crypto.commands.total_value_total_invested_net_return.formatted", locale.Data{"Value1": int(math.Round(totalValue)), "CurrencyName": config.Bot.CurrencyName, "Value3": int(math.Round(totalInvested)), "CurrencyName4": config.Bot.CurrencyName, "OverallPnlStr": overallPnlStr}))

	return utils.GoldEmbed(locale.Text("crypto.commands.your_crypto_portfolio"), sb.String())
}

func formatCryptoAmount(amount float64) string {
	if amount >= 1 {
		return fmt.Sprintf("%.4f", amount)
	} else if amount >= 0.0001 {
		return fmt.Sprintf("%.6f", amount)
	}
	return fmt.Sprintf("%.8f", amount)
}

package stockmarket

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

// ExecuteMarket returns current market stock prices embed
func ExecuteMarket() *discordgo.MessageEmbed {
	var sb strings.Builder
	sb.WriteString(locale.Text("stockmarket.commands.current_market_prices_real_time_yahoo_finance"))

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
			priceStr = locale.Text("stockmarket.commands.fetching")
		}
		sb.WriteString(fmt.Sprintf("**%s** (%s): $%s\n", company.Name, company.Ticker, priceStr))
	}

	return utils.GoldEmbed(locale.Text("stockmarket.commands.stock_market"), sb.String())
}

// ExecuteBuy processes a stock purchase and returns an embed response
func ExecuteBuy(guildID, userID, ticker string, amount int) *discordgo.MessageEmbed {
	ticker = strings.ToUpper(strings.TrimSpace(ticker))
	if amount <= 0 {
		return utils.ErrorEmbed(locale.Text("stockmarket.commands.invalid_amount"))
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
		return utils.ErrorEmbed(locale.Text("stockmarket.commands.invalid_ticker_check_stock_market"))
	}

	// Check Balance
	balance := database.GetBalance(guildID, userID)
	if balance < amount {
		return utils.ErrorEmbed(locale.Text("stockmarket.commands.insufficient_funds"))
	}

	// Get Price
	price, err := database.GetStockPriceDB(ticker)
	if err != nil || price <= 0 {
		data, err := GetStockPrice(ticker)
		if err != nil {
			return utils.ErrorEmbed(locale.Text("stockmarket.commands.could_not_fetch_stock_price_try_again"))
		}
		price = data.Price
		_ = database.SetStockPriceDB(ticker, price)
	}

	shares := float64(amount) / price

	// Transaction
	if err := database.RemoveCoins(guildID, userID, amount); err != nil {
		return utils.ErrorEmbed(locale.Text("stockmarket.commands.transaction_failed"))
	}

	if err := database.AddShares(guildID, userID, ticker, shares, amount); err != nil {
		// Refund
		_ = database.AddCoins(guildID, userID, amount)
		return utils.ErrorEmbed(locale.Text("stockmarket.commands.database_error_refunded"))
	}

	return utils.SuccessEmbed(locale.Text("stockmarket.commands.investment_successful"),
		locale.Text("stockmarket.commands.you_bought_shares_of_for_at_share.formatted", locale.Data{"Shares": shares, "Ticker": ticker, "Amount": amount, "CurrencyName": config.Bot.CurrencyName, "Price": price}))
}

// ExecuteSell processes a stock sale and returns an embed response
func ExecuteSell(guildID, userID, ticker, amountStr string) *discordgo.MessageEmbed {
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
		return utils.ErrorEmbed(locale.Text("stockmarket.commands.invalid_ticker"))
	}

	ownedShares, _ := database.GetInvestment(guildID, userID, ticker)
	if ownedShares <= 0 {
		return utils.ErrorEmbed(locale.Text("stockmarket.commands.you_don_t_own_any_shares_of"))
	}

	var sharesToSell float64

	if strings.ToLower(amountStr) == "all" {
		sharesToSell = ownedShares
	} else {
		val, err := strconv.ParseFloat(amountStr, 64)
		if err != nil || val <= 0 {
			return utils.ErrorEmbed(locale.Text("stockmarket.commands.invalid_number_of_shares"))
		}
		sharesToSell = val
	}

	if sharesToSell > ownedShares {
		return utils.ErrorEmbed(locale.Text("stockmarket.commands.you_don_t_have_that_many_shares.formatted", locale.Data{"OwnedShares": ownedShares}))
	}

	// Get Price
	price, err := database.GetStockPriceDB(ticker)
	if err != nil || price <= 0 {
		data, err := GetStockPrice(ticker)
		if err != nil {
			return utils.ErrorEmbed(locale.Text("stockmarket.commands.could_not_fetch_stock_price"))
		}
		price = data.Price
		_ = database.SetStockPriceDB(ticker, price)
	}

	payout := int(math.Round(sharesToSell * price))

	if err := database.RemoveShares(guildID, userID, ticker, sharesToSell); err != nil {
		return utils.ErrorEmbed(locale.Text("stockmarket.commands.database_error"))
	}

	_ = database.AddCoins(guildID, userID, payout)
	return utils.SuccessEmbed(locale.Text("stockmarket.commands.sale_successful"),
		locale.Text("stockmarket.commands.you_sold_shares_of_for_at_share.formatted", locale.Data{"SharesToSell": sharesToSell, "Ticker": ticker, "Payout": payout, "CurrencyName": config.Bot.CurrencyName, "Price": price}))
}

// ExecutePortfolio generates a user's stock portfolio embed
func ExecutePortfolio(guildID, userID string) *discordgo.MessageEmbed {
	investments, err := database.GetAllInvestmentsByUser(guildID, userID)
	if err != nil {
		return utils.ErrorEmbed(locale.Text("stockmarket.commands.database_error"))
	}

	if len(investments) == 0 {
		return utils.InfoEmbed(locale.Text("stockmarket.commands.portfolio"), locale.Text("stockmarket.commands.you_have_no_stock_investments"))
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

		sb.WriteString(locale.Text("stockmarket.commands.shares_invested_p_l.formatted", locale.Data{"Ticker": inv.Ticker, "Shares": inv.Shares, "Value3": int(math.Round(val)), "CurrencyName": config.Bot.CurrencyName, "Price": price, "Value6": int(math.Round(cost)), "CurrencyName7": config.Bot.CurrencyName, "PnlStr": pnlStr}))
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

	sb.WriteString(locale.Text("stockmarket.commands.total_value_total_invested_net_return.formatted", locale.Data{"Value1": int(math.Round(totalVal)), "CurrencyName": config.Bot.CurrencyName, "Value3": int(math.Round(totalInvested)), "CurrencyName4": config.Bot.CurrencyName, "OverallPnlStr": overallPnlStr}))

	return utils.GoldEmbed(locale.Text("stockmarket.commands.your_stock_portfolio"), sb.String())
}

package crypto

import (
	"estudocoin/internal/database"
	"estudocoin/pkg/config"
	"estudocoin/pkg/utils"
	"fmt"
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
		handleCryptoMarket(s, m)
	case "buy":
		handleCryptoBuy(s, m, args[1:])
	case "sell":
		handleCryptoSell(s, m, args[1:])
	case "portfolio", "p":
		handleCryptoPortfolio(s, m)
	default:
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Unknown subcommand. Use `market`, `buy`, `sell`, or `portfolio`."))
	}
}

func handleCryptoMarket(s *discordgo.Session, m *discordgo.MessageCreate) {
	prices, err := GetCryptoPrices()
	if err != nil {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Error fetching crypto prices. Try again later."))
		return
	}

	var sb strings.Builder
	sb.WriteString("**Major Cryptocurrencies:**\n")
	for _, crypto := range AvailableCryptos {
		if crypto.Type != "major" {
			continue
		}
		price := prices[crypto.ID]
		if price > 0 {
			priceStr := formatPrice(price)
			sb.WriteString(fmt.Sprintf("**%s** (%s): $%s\n", crypto.Name, crypto.Symbol, priceStr))
		}
	}

	sb.WriteString("\n**Meme Coins (High Volatility!):**\n")
	for _, crypto := range AvailableCryptos {
		if crypto.Type != "meme" {
			continue
		}
		price := prices[crypto.ID]
		if price > 0 {
			priceStr := formatPrice(price)
			sb.WriteString(fmt.Sprintf("**%s** (%s): $%s\n", crypto.Name, crypto.Symbol, priceStr))
		}
	}

	s.ChannelMessageSendEmbed(m.ChannelID, utils.GoldEmbed("Crypto Market", sb.String()))
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

func handleCryptoBuy(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	if len(args) < 2 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Usage: `!crypto buy <SYMBOL> <amount>`\nExample: `!crypto buy BTC 1000`"))
		return
	}

	symbol := strings.ToUpper(args[0])
	amountStr := args[1]
	amount, err := strconv.Atoi(amountStr)
	if err != nil || amount <= 0 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Invalid amount."))
		return
	}

	// Verificar se a crypto existe
	crypto := GetCryptoBySymbol(symbol)
	if crypto == nil {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Invalid cryptocurrency symbol. Use `!crypto market` to see available options."))
		return
	}

	// Verificar saldo
	balance := database.GetBalance(m.Author.ID)
	if balance < amount {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Insufficient funds."))
		return
	}

	// Buscar preço atual
	price, err := GetSingleCryptoPrice(crypto.ID)
	if err != nil || price <= 0 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Could not fetch crypto price. Try again later."))
		return
	}

	// Calcular quantidade de coins
	coins := float64(amount) / price

	// Transação
	if err := database.RemoveCoins(m.Author.ID, amount); err != nil {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Transaction failed."))
		return
	}

	if err := database.AddCryptoShares(m.Author.ID, symbol, coins); err != nil {
		// Refund
		database.AddCoins(m.Author.ID, amount)
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Database error. Refunded."))
		return
	}

	// Mensagem especial para meme coins
	emoji := "🚀"
	warning := ""
	if crypto.Type == "meme" {
		emoji = "🎰"
		warning = "\n⚠️ **Meme coins are highly volatile! Invest at your own risk.**"
	}

	s.ChannelMessageSendEmbed(m.ChannelID, utils.SuccessEmbed("Crypto Purchase Successful!", 
		fmt.Sprintf("%s You bought **%s %s** for **%d %s** (at $%s/coin).%s",
			emoji, formatCryptoAmount(coins), symbol, amount, config.Bot.CurrencyName, formatPrice(price), warning)))
}

func handleCryptoSell(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	if len(args) < 2 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Usage: `!crypto sell <SYMBOL> <amount|all>`\nExample: `!crypto sell BTC all` or `!crypto sell BTC 0.5`"))
		return
	}

	symbol := strings.ToUpper(args[0])
	amountStr := args[1]

	// Verificar se a crypto existe
	crypto := GetCryptoBySymbol(symbol)
	if crypto == nil {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Invalid cryptocurrency symbol."))
		return
	}

	// Verificar quantidade possuída
	ownedCoins, _ := database.GetCryptoInvestment(m.Author.ID, symbol)
	if ownedCoins <= 0 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed(fmt.Sprintf("You don't own any %s.", symbol)))
		return
	}

	var coinsToSell float64

	if strings.ToLower(amountStr) == "all" {
		coinsToSell = ownedCoins
	} else {
		val, err := strconv.ParseFloat(amountStr, 64)
		if err != nil || val <= 0 {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Invalid amount."))
			return
		}
		coinsToSell = val
	}

	if coinsToSell > ownedCoins {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed(fmt.Sprintf("You only own %s %s.", formatCryptoAmount(ownedCoins), symbol)))
		return
	}

	// Buscar preço atual
	price, err := GetSingleCryptoPrice(crypto.ID)
	if err != nil || price <= 0 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Could not fetch crypto price. Try again later."))
		return
	}

	payout := int(coinsToSell * price)

	if err := database.RemoveCryptoShares(m.Author.ID, symbol, coinsToSell); err != nil {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Database error."))
		return
	}

	database.AddCoins(m.Author.ID, payout)

	emoji := "💰"
	if crypto.Type == "meme" {
		emoji = "🎰"
	}

	s.ChannelMessageSendEmbed(m.ChannelID, utils.SuccessEmbed("Crypto Sale Successful!",
		fmt.Sprintf("%s You sold **%s %s** for **%d %s** (at $%s/coin).",
			emoji, formatCryptoAmount(coinsToSell), symbol, payout, config.Bot.CurrencyName, formatPrice(price))))
}

func handleCryptoPortfolio(s *discordgo.Session, m *discordgo.MessageCreate) {
	investments, err := database.GetAllCryptoInvestmentsByUser(m.Author.ID)
	if err != nil {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Database error."))
		return
	}

	if len(investments) == 0 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.InfoEmbed("Crypto Portfolio", "You have no cryptocurrency investments."))
		return
	}

	// Buscar preços atuais
	prices, err := GetCryptoPrices()
	if err != nil {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Could not fetch current prices."))
		return
	}

	var sb strings.Builder
	totalValue := 0.0

	for _, inv := range investments {
		crypto := GetCryptoBySymbol(inv.Symbol)
		if crypto == nil {
			continue
		}

		price := prices[crypto.ID]
		if price <= 0 {
			continue
		}

		value := inv.Coins * price
		totalValue += value

		// Calcular valor investido (média seria ideal, mas vamos simplificar)
		// Por simplicidade, não rastreamos preço médio de compra

		emoji := "🟢"
		if crypto.Type == "meme" {
			emoji = "🔴"
		}

		sb.WriteString(fmt.Sprintf("%s **%s** (%s): %s coins (~%d %s @ $%s)\n",
			emoji, crypto.Name, inv.Symbol, formatCryptoAmount(inv.Coins), int(value), config.Bot.CurrencyName, formatPrice(price)))
	}

	sb.WriteString(fmt.Sprintf("\n**Total Value**: ~%d %s", int(totalValue), config.Bot.CurrencyName))

	s.ChannelMessageSendEmbed(m.ChannelID, utils.GoldEmbed("Your Crypto Portfolio", sb.String()))
}

func formatCryptoAmount(amount float64) string {
	if amount >= 1 {
		return fmt.Sprintf("%.4f", amount)
	} else if amount >= 0.0001 {
		return fmt.Sprintf("%.6f", amount)
	}
	return fmt.Sprintf("%.8f", amount)
}

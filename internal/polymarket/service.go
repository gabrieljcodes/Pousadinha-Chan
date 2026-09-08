package polymarket

import (
	"estudocoin/internal/database"
	"estudocoin/pkg/config"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

// CalculateShareCost computes the cost and fees to purchase a number of shares
func CalculateShareCost(outcomePrice float64, shares int64, houseEdge float64) ShareCost {
	if shares <= 0 {
		shares = 1
	}
	if outcomePrice < 0.01 {
		outcomePrice = 0.01
	} else if outcomePrice > 0.99 {
		outcomePrice = 0.99
	}
	if houseEdge < 0 {
		houseEdge = 0
	} else if houseEdge > 0.20 {
		houseEdge = 0.20
	}

	pricePerShare := math.Round(outcomePrice*10000) / 100.0 // in EC (e.g. 65.00)
	rawCost := int64(math.Round(pricePerShare * float64(shares)))
	fee := int64(math.Round(float64(rawCost) * houseEdge))
	if fee < 1 && houseEdge > 0 && rawCost > 0 {
		fee = 1
	}

	totalCost := rawCost + fee
	potentialPayout := shares * 100
	potentialProfit := potentialPayout - totalCost

	return ShareCost{
		Shares:          shares,
		PricePerShare:   pricePerShare,
		RawCost:         rawCost,
		Fee:             fee,
		TotalCost:       totalCost,
		PotentialPayout: potentialPayout,
		PotentialProfit: potentialProfit,
	}
}

// CalculateSharesFromBudget calculates how many whole shares can be bought with a given EC budget
func CalculateSharesFromBudget(outcomePrice float64, budget int64, houseEdge float64) ShareCost {
	if budget <= 0 {
		return CalculateShareCost(outcomePrice, 1, houseEdge)
	}

	effectiveMultiplier := outcomePrice * (1.0 + houseEdge)
	if effectiveMultiplier <= 0 {
		effectiveMultiplier = 0.01
	}

	shares := int64(float64(budget) / (effectiveMultiplier * 100))
	if shares <= 0 {
		shares = 1
	}

	cost := CalculateShareCost(outcomePrice, shares, houseEdge)
	// If cost slightly exceeds budget due to rounding, step back 1 share if possible
	if cost.TotalCost > budget && shares > 1 {
		shares--
		cost = CalculateShareCost(outcomePrice, shares, houseEdge)
	}
	return cost
}

// FormatProbabilityBar renders a visual progress bar of Yes vs No probabilities
func FormatProbabilityBar(yesPrice, noPrice float64) string {
	total := yesPrice + noPrice
	if total <= 0 {
		total = 1.0
	}
	yesPct := int(math.Round((yesPrice / total) * 100))
	noPct := 100 - yesPct

	const barLength = 12
	yesBlocks := int(math.Round((float64(yesPct) / 100.0) * float64(barLength)))
	if yesBlocks < 0 {
		yesBlocks = 0
	} else if yesBlocks > barLength {
		yesBlocks = barLength
	}
	noBlocks := barLength - yesBlocks

	bar := strings.Repeat("█", yesBlocks) + strings.Repeat("░", noBlocks)
	return fmt.Sprintf("🟢 **SIM**: `%d%%` [%s] `%d%%` :**NÃO** 🔴", yesPct, bar, noPct)
}

// CreateMarketEmbed builds the rich Discord embed for an active or resolved Polymarket market
func CreateMarketEmbed(market *database.DBPolymarketMarket, settings *database.DBPolymarketSettings) *discordgo.MessageEmbed {
	embedColor := 0x2b2d31 // Dark theme
	statusTitle := "📊 Mercado Polymarket"

	switch market.Status {
	case "open":
		embedColor = 0x5865f2 // Blurple
		statusTitle = "📊 Mercado Aberto - Polymarket"
	case "closed":
		embedColor = 0xe67e22 // Orange
		statusTitle = "🔒 Mercado Fechado (Aguardando Desfecho)"
	case "resolved":
		embedColor = 0x2ecc71 // Green
		statusTitle = fmt.Sprintf("🏆 Mercado Resolvido: Venceu %s!", strings.ToUpper(market.Winner))
	case "cancelled":
		embedColor = 0x95a5a6 // Gray
		statusTitle = "❌ Mercado Cancelado (Apostas Reembolsadas)"
	}

	probBar := FormatProbabilityBar(market.YesPrice, market.NoPrice)
	yesCost := CalculateShareCost(market.YesPrice, 1, settings.HouseEdge)
	noCost := CalculateShareCost(market.NoPrice, 1, settings.HouseEdge)

	description := fmt.Sprintf("### %s\n\n%s\n\n%s", market.Question, probBar, market.Description)
	if len(description) > 2000 {
		description = description[:1997] + "..."
	}

	fields := []*discordgo.MessageEmbedField{
		{
			Name:   "🟢 Cotação Sim",
			Value:  fmt.Sprintf("**%d EC** / ação\n*Retorno: 100 EC*", yesCost.TotalCost),
			Inline: true,
		},
		{
			Name:   "🔴 Cotação Não",
			Value:  fmt.Sprintf("**%d EC** / ação\n*Retorno: 100 EC*", noCost.TotalCost),
			Inline: true,
		},
		{
			Name:   "🏷️ Categoria",
			Value:  fmt.Sprintf("`%s`", market.Category),
			Inline: true,
		},
	}

	if market.EndDate != nil {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   "📅 Previsão de Encerramento",
			Value:  fmt.Sprintf("<t:%d:F> (<t:%d:R>)", market.EndDate.Unix(), market.EndDate.Unix()),
			Inline: true,
		})
	}

	fields = append(fields, &discordgo.MessageEmbedField{
		Name:   "🏛️ Taxa da Casa",
		Value:  fmt.Sprintf("%.1f%%", settings.HouseEdge*100),
		Inline: true,
	})

	footerText := fmt.Sprintf("ID: %s | Polymarket ID: %s | Atualizado em tempo real", market.ID, market.PolymarketID)

	embed := &discordgo.MessageEmbed{
		Title:       statusTitle,
		Description: description,
		Color:       embedColor,
		Fields:      fields,
		Footer: &discordgo.MessageEmbedFooter{
			Text: footerText,
		},
		Timestamp: time.Now().Format(time.RFC3339),
	}

	if market.ImageURL != "" {
		embed.Thumbnail = &discordgo.MessageEmbedThumbnail{
			URL: market.ImageURL,
		}
	}

	return embed
}

// CreateMarketComponents builds action buttons for the market embed
func CreateMarketComponents(market *database.DBPolymarketMarket, settings *database.DBPolymarketSettings) []discordgo.MessageComponent {
	if market.Status != "open" {
		// Market closed or resolved
		return []discordgo.MessageComponent{
			discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					discordgo.Button{
						CustomID: fmt.Sprintf("poly_pos_%s", market.ID),
						Label:    "💼 Minhas Ações",
						Style:    discordgo.SecondaryButton,
						Emoji:    &discordgo.ComponentEmoji{Name: "💼"},
					},
					discordgo.Button{
						CustomID: fmt.Sprintf("poly_status_%s", market.ID),
						Label:    fmt.Sprintf("Status: %s", strings.ToUpper(market.Status)),
						Style:    discordgo.SecondaryButton,
						Disabled: true,
					},
				},
			},
		}
	}

	yesCost := CalculateShareCost(market.YesPrice, 1, settings.HouseEdge)
	noCost := CalculateShareCost(market.NoPrice, 1, settings.HouseEdge)

	row := discordgo.ActionsRow{
		Components: []discordgo.MessageComponent{
			discordgo.Button{
				CustomID: fmt.Sprintf("poly_buy_yes_%s", market.ID),
				Label:    fmt.Sprintf("Comprar SIM (%d EC)", yesCost.TotalCost),
				Style:    discordgo.SuccessButton,
				Emoji:    &discordgo.ComponentEmoji{Name: "🟢"},
			},
			discordgo.Button{
				CustomID: fmt.Sprintf("poly_buy_no_%s", market.ID),
				Label:    fmt.Sprintf("Comprar NÃO (%d EC)", noCost.TotalCost),
				Style:    discordgo.DangerButton,
				Emoji:    &discordgo.ComponentEmoji{Name: "🔴"},
			},
			discordgo.Button{
				CustomID: fmt.Sprintf("poly_pos_%s", market.ID),
				Label:    "Minhas Ações",
				Style:    discordgo.SecondaryButton,
				Emoji:    &discordgo.ComponentEmoji{Name: "💼"},
			},
			discordgo.Button{
				CustomID: fmt.Sprintf("poly_refresh_%s", market.ID),
				Label:    "Atualizar",
				Style:    discordgo.SecondaryButton,
				Emoji:    &discordgo.ComponentEmoji{Name: "🔄"},
			},
		},
	}

	return []discordgo.MessageComponent{row}
}

// CreateBuyModal creates an interactive modal popup for buying shares
func CreateBuyModal(marketID, outcome string, pricePerShare float64, houseEdge float64) *discordgo.InteractionResponse {
	cost1 := CalculateShareCost(outcomePrice(pricePerShare), 1, houseEdge)
	cost10 := CalculateShareCost(outcomePrice(pricePerShare), 10, houseEdge)

	title := fmt.Sprintf("Comprar Ações: %s", strings.ToUpper(outcome))
	if len(title) > 45 {
		title = title[:45]
	}

	modalCustomID := fmt.Sprintf("poly_modal_buy_%s_%s", strings.ToLower(outcome), marketID)

	return &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{
			CustomID: modalCustomID,
			Title:    title,
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{
							CustomID:    "shares",
							Label:       "Quantidade de Ações (Cada ação paga 100 EC)",
							Style:       discordgo.TextInputShort,
							Placeholder: fmt.Sprintf("Ex: 10 (Custo estimado: %d EC)", cost10.TotalCost),
							Required:    true,
							MinLength:   1,
							MaxLength:   8,
						},
					},
				},
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{
							CustomID:    "info_hint",
							Label:       "Preço por ação atual (com taxa incluída):",
							Style:       discordgo.TextInputShort,
							Value:       fmt.Sprintf("1 ação = %d EC | Paga 100 EC se vencer!", cost1.TotalCost),
							Required:    false,
						},
					},
				},
			},
		},
	}
}

func outcomePrice(p float64) float64 {
	if p <= 0 {
		return 0.50
	}
	return p
}

// CreatePortfolioEmbed builds the portfolio summary of all active positions held by the user
func CreatePortfolioEmbed(userID string, positions []*database.DBPolymarketPosition, markets map[string]*database.DBPolymarketMarket) *discordgo.MessageEmbed {
	if len(positions) == 0 {
		return &discordgo.MessageEmbed{
			Title:       "💼 Seu Portfólio Polymarket",
			Description: "Você ainda não possui nenhuma ação ativa em mercados do Polymarket.\nUse `/poly trending` ou pesquise eventos no canal dedicado para começar a negociar!",
			Color:       0x5865f2,
		}
	}

	var totalInvested int64
	var totalPotentialPayout int64
	var positionLines []string

	for _, p := range positions {
		m := markets[p.MarketID]
		q := "Mercado #" + p.MarketID
		if m != nil && m.Question != "" {
			q = m.Question
		}
		if len(q) > 60 {
			q = q[:57] + "..."
		}

		payout := p.Shares * 100
		avgPrice := float64(p.TotalInvested) / float64(p.Shares)
		totalInvested += p.TotalInvested
		totalPotentialPayout += payout

		emoji := "🟢"
		if strings.EqualFold(p.Outcome, "No") {
			emoji = "🔴"
		}

		line := fmt.Sprintf("%s **%s** (%s)\n• **%d ações** | Preço médio: `%.1f EC`\n• Investido: `%d %s` | Retorno potencial: `+%d %s`\n",
			emoji, q, p.Outcome, p.Shares, avgPrice, p.TotalInvested, config.Bot.CurrencySymbol, payout-p.TotalInvested, config.Bot.CurrencySymbol)
		positionLines = append(positionLines, line)
	}

	embed := &discordgo.MessageEmbed{
		Title:       fmt.Sprintf("💼 Portfólio Polymarket - <@%s>", userID),
		Description: strings.Join(positionLines, "\n"),
		Color:       0x2ecc71,
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   "💰 Total Investido",
				Value:  fmt.Sprintf("**%d %s**", totalInvested, config.Bot.CurrencySymbol),
				Inline: true,
			},
			{
				Name:   "🏆 Retorno Máximo Potencial",
				Value:  fmt.Sprintf("**%d %s**", totalPotentialPayout, config.Bot.CurrencySymbol),
				Inline: true,
			},
			{
				Name:   "📊 Posições Abertas",
				Value:  fmt.Sprintf("**%d mercados**", len(positions)),
				Inline: true,
			},
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: "Cada ação vencedora é resgatada por 100 EC automaticamente no encerramento.",
		},
		Timestamp: time.Now().Format(time.RFC3339),
	}

	return embed
}

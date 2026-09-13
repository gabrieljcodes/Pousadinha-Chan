package commands

import (
	"bot/internal/database"
	"bot/internal/locale"
	"bot/internal/polymarket"
	"bot/pkg/config"
	"bot/pkg/utils"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

func buildMarketListEmbed(title string, markets []*polymarket.GammaMarket) *discordgo.MessageEmbed {
	var lines []string
	for i, m := range markets {
		yesPrice, noPrice, _ := m.GetPrices()
		yesPct := int(yesPrice * 100)
		noPct := int(noPrice * 100)

		lines = append(lines, locale.Text("commands.polymarket_cmd.probability_yes_no_volume_slug_import_poly.formatted", locale.Data{"I": i + 1, "Question": m.Question, "YesPct": yesPct, "NoPct": noPct, "M": m.FormatVolume(), "Slug": m.Slug, "Slug7": m.Slug}))
	}

	return &discordgo.MessageEmbed{
		Title:       title,
		Description: strings.Join(lines, "\n"),
		Color:       0x5865f2,
		Footer: &discordgo.MessageEmbedFooter{
			Text: locale.Text("commands.polymarket_cmd.use_poly_import_to_open_a_market"),
		},
	}
}

func buildCandidateSelectionEmbedAndComponents(event *polymarket.GammaEvent) (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
	var lines []string
	limit := 8
	if len(event.Markets) < limit {
		limit = len(event.Markets)
	}

	for idx := 0; idx < limit; idx++ {
		m := event.Markets[idx]
		title := m.GroupItemTitle
		if title == "" {
			title = m.Question
		}
		yesP, noP, _ := m.GetPrices()
		lines = append(lines, locale.Text("commands.polymarket_cmd.price_yes_no_volume.formatted", locale.Data{"Idx": idx + 1, "Title": title, "YesP": yesP * 100, "NoP": noP * 100, "M": m.FormatVolume()}))
	}

	embed := &discordgo.MessageEmbed{
		Title:       locale.Text("commands.polymarket_cmd.select_a_candidate.formatted", locale.Data{"Title": event.Title}),
		Description: locale.Text("commands.polymarket_cmd.this_event_has_markets_candidates_on_polymarket.formatted", locale.Data{"Value1": len(event.Markets), "Strings": strings.Join(lines, "\n"), "Slug": event.Slug}),
		Color:       0x5865f2,
		Thumbnail: &discordgo.MessageEmbedThumbnail{
			URL: event.Image,
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: locale.Text("commands.polymarket_cmd.automatic_settlement_using_official_polymarket_data"),
		},
	}

	var options []discordgo.SelectMenuOption
	maxOptions := 25
	if len(event.Markets) < maxOptions {
		maxOptions = len(event.Markets)
	}

	for idx := 0; idx < maxOptions; idx++ {
		m := event.Markets[idx]
		label := m.GroupItemTitle
		if label == "" {
			label = m.Question
		}
		if len(label) > 100 {
			label = label[:97] + "..."
		}
		yesP, noP, _ := m.GetPrices()
		desc := locale.Text("commands.polymarket_cmd.yes_no_vol.formatted", locale.Data{"YesP": yesP * 100, "NoP": noP * 100, "M": m.FormatVolume()})
		if len(desc) > 100 {
			desc = desc[:97] + "..."
		}

		options = append(options, discordgo.SelectMenuOption{
			Label:       label,
			Value:       m.ID,
			Description: desc,
			Emoji:       &discordgo.ComponentEmoji{Name: "🟢"},
		})
	}

	menu := discordgo.SelectMenu{
		CustomID:    "poly_candidate_select:" + event.Slug,
		Placeholder: locale.Text("commands.polymarket_cmd.choose_a_candidate"),
		Options:     options,
	}

	components := []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{menu},
		},
	}

	return embed, components
}

func publishMarketToChannel(s *discordgo.Session, guildID, targetChannelID, responseChannelID string, gammaMarket *polymarket.GammaMarket, settings *database.DBPolymarketSettings) {
	// Check if already imported
	existing, _ := database.GetPolymarketMarketByPolyID(gammaMarket.ID)
	if existing != nil && existing.Status == "open" {
		if responseChannelID != "" {
			s.ChannelMessageSendEmbed(responseChannelID, utils.InfoEmbed(
				locale.Text("commands.polymarket_cmd.market_already_active"),
				locale.Text("commands.polymarket_cmd.this_market_is_already_open_in_market.formatted", locale.Data{"ChannelID": existing.ChannelID, "ID": existing.ID}),
			))
		}
		return
	}

	yesPrice, noPrice, _ := gammaMarket.GetPrices()
	internalID := "poly_" + gammaMarket.ID

	dbMarket := &database.DBPolymarketMarket{
		ID:           internalID,
		PolymarketID: gammaMarket.ID,
		ConditionID:  gammaMarket.ConditionID,
		Slug:         gammaMarket.Slug,
		Question:     gammaMarket.Question,
		Description:  gammaMarket.Description,
		Category:     locale.Text("commands.polymarket_cmd.general"),
		ImageURL:     gammaMarket.Image,
		YesPrice:     yesPrice,
		NoPrice:      noPrice,
		Status:       "open",
		Winner:       "",
		EndDate:      gammaMarket.ParseEndDate(),
		GuildID:      guildID,
		ChannelID:    targetChannelID,
		MessageID:    "",
	}

	embed := polymarket.CreateMarketEmbed(dbMarket, settings)
	components := polymarket.CreateMarketComponents(dbMarket, settings)

	msg, err := s.ChannelMessageSendComplex(targetChannelID, &discordgo.MessageSend{
		Embeds:     []*discordgo.MessageEmbed{embed},
		Components: components,
	})

	if err != nil {
		if responseChannelID != "" {
			s.ChannelMessageSendEmbed(responseChannelID, utils.ErrorEmbed(locale.Text("commands.polymarket_cmd.unable_to_publish_the_market_in.formatted", locale.Data{"TargetChannelID": targetChannelID, "Err": err})))
		}
		return
	}

	dbMarket.MessageID = msg.ID
	if err := database.CreatePolymarketMarketDB(dbMarket); err != nil {
		log.Printf("[PolymarketCmd] Error saving market to DB: %v", err)
	}

	if responseChannelID != "" && responseChannelID != targetChannelID {
		s.ChannelMessageSendEmbed(responseChannelID, utils.SuccessEmbed(
			locale.Text("commands.polymarket_cmd.market_imported"),
			locale.Text("commands.polymarket_cmd.the_market_is_now_open_in_id.formatted", locale.Data{"Question": gammaMarket.Question, "TargetChannelID": targetChannelID, "ID": dbMarket.ID}),
		))
	}
}

func importMarketInternal(s *discordgo.Session, guildID, targetChannelID, responseChannelID, userID, input, candidate string, settings *database.DBPolymarketSettings) {
	client := polymarket.GetClient()
	gammaMarket, gammaEvent, err := client.ResolveImportQuery(input, candidate)
	if err != nil {
		s.ChannelMessageSendEmbed(responseChannelID, utils.ErrorEmbed(locale.Text("commands.polymarket_cmd.unable_to_query_polymarket.formatted", locale.Data{"Err": err})))
		return
	}

	if gammaMarket != nil {
		publishMarketToChannel(s, guildID, targetChannelID, responseChannelID, gammaMarket, settings)
		return
	}

	if gammaEvent != nil {
		// Multi-candidate event with no specific candidate provided: send interactive dropdown
		embed, components := buildCandidateSelectionEmbedAndComponents(gammaEvent)
		_, _ = s.ChannelMessageSendComplex(responseChannelID, &discordgo.MessageSend{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
		})
	}
}

func suggestMarket(s *discordgo.Session, guildID, channelID string, user *discordgo.User, input string, candidate string) {
	if guildID == "" {
		return
	}

	settings, _ := database.GetGuildPolymarketSettings(guildID)
	if settings == nil || settings.ChannelID == "" {
		s.ChannelMessageSendEmbed(channelID, utils.ErrorEmbed(locale.Text("commands.polymarket_cmd.the_polymarket_channel_is_not_configured_ask")))
		return
	}

	client := polymarket.GetClient()
	gammaMarket, gammaEvent, err := client.ResolveImportQuery(input, candidate)
	if err != nil {
		s.ChannelMessageSendEmbed(channelID, utils.ErrorEmbed(locale.Text("commands.polymarket_cmd.unable_to_find_this_event_on_polymarket.formatted", locale.Data{"Err": err})))
		return
	}

	if gammaEvent != nil && gammaMarket == nil {
		s.ChannelMessageSendEmbed(channelID, utils.InfoEmbed(
			locale.Text("commands.polymarket_cmd.choose_a_candidate_bf376c"),
			locale.Text("commands.polymarket_cmd.the_event_has_multiple_candidates_choose_one.formatted", locale.Data{"Title": gammaEvent.Title, "Slug": gammaEvent.Slug}),
		))
		return
	}

	yesPrice, noPrice, _ := gammaMarket.GetPrices()

	suggestionEmbed := &discordgo.MessageEmbed{
		Title:       locale.Text("commands.polymarket_cmd.polymarket_suggestion"),
		Description: locale.Text("commands.polymarket_cmd.suggested_opening_this_market_current_prices_yes.formatted", locale.Data{"ID": user.ID, "Question": gammaMarket.Question, "YesPrice": yesPrice * 100, "NoPrice": noPrice * 100, "GammaMarket": gammaMarket.FormatVolume(), "Slug": gammaMarket.Slug}),
		Color:       0xf1c40f,
		Thumbnail: &discordgo.MessageEmbedThumbnail{
			URL: gammaMarket.Image,
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: locale.Text("commands.polymarket_cmd.polymarket_id_suggested_by.formatted", locale.Data{"ID": gammaMarket.ID, "Username": user.Username}),
		},
		Timestamp: time.Now().Format(time.RFC3339),
	}

	actionRow := discordgo.ActionsRow{
		Components: []discordgo.MessageComponent{
			discordgo.Button{
				CustomID: fmt.Sprintf("poly_approve_%s", gammaMarket.ID),
				Label:    locale.Text("commands.polymarket_cmd.approve_and_import"),
				Style:    discordgo.SuccessButton,
				Emoji:    &discordgo.ComponentEmoji{Name: "✅"},
			},
			discordgo.Button{
				CustomID: fmt.Sprintf("poly_reject_%s", gammaMarket.ID),
				Label:    locale.Text("commands.polymarket_cmd.reject"),
				Style:    discordgo.DangerButton,
				Emoji:    &discordgo.ComponentEmoji{Name: "❌"},
			},
		},
	}

	_, _ = s.ChannelMessageSendComplex(settings.ChannelID, &discordgo.MessageSend{
		Embeds:     []*discordgo.MessageEmbed{suggestionEmbed},
		Components: []discordgo.MessageComponent{actionRow},
	})

	s.ChannelMessageSendEmbed(channelID, utils.SuccessEmbed(
		locale.Text("commands.polymarket_cmd.suggestion_submitted"),
		locale.Text("commands.polymarket_cmd.your_market_suggestion_was_sent_for_review.formatted", locale.Data{"ChannelID": settings.ChannelID}),
	))
}

func cancelMarket(s *discordgo.Session, guildID, channelID string, user *discordgo.User, query string) {
	if guildID == "" {
		return
	}
	if !isUserAdmin(s, guildID, user.ID) {
		s.ChannelMessageSendEmbed(channelID, utils.ErrorEmbed(locale.Text("commands.polymarket_cmd.only_administrators_can_cancel_markets")))
		return
	}

	cleanID := polymarket.ExtractSlug(query)
	internalID := cleanID
	if !strings.HasPrefix(internalID, "poly_") {
		internalID = "poly_" + cleanID
	}

	market, err := database.GetPolymarketMarketByID(internalID)
	if err != nil || market == nil {
		market, _ = database.GetPolymarketMarketByPolyID(cleanID)
	}
	if market == nil {
		s.ChannelMessageSendEmbed(channelID, utils.ErrorEmbed(locale.Text("commands.polymarket_cmd.market_was_not_found.formatted", locale.Data{"Query": query})))
		return
	}

	if market.Status != "open" && market.Status != "closed" {
		s.ChannelMessageSendEmbed(channelID, utils.ErrorEmbed(locale.Text("commands.polymarket_cmd.this_market_already_has_status.formatted", locale.Data{"Status": market.Status})))
		return
	}

	refunds, err := database.CancelAndRefundPolymarketMarketDB(market.ID)
	if err != nil {
		s.ChannelMessageSendEmbed(channelID, utils.ErrorEmbed(locale.Text("commands.polymarket_cmd.unable_to_cancel_the_market.formatted", locale.Data{"Err": err})))
		return
	}

	market.Status = "cancelled"
	market.Winner = "cancelled"

	settings, _ := database.GetGuildPolymarketSettings(guildID)
	if settings == nil {
		settings = &database.DBPolymarketSettings{HouseEdge: 0.03}
	}

	// Update original embed if exists
	if market.ChannelID != "" && market.MessageID != "" {
		embed := polymarket.CreateMarketEmbed(market, settings)
		components := polymarket.CreateMarketComponents(market, settings)
		embeds := []*discordgo.MessageEmbed{embed}
		_, _ = s.ChannelMessageEditComplex(&discordgo.MessageEdit{
			Channel:    market.ChannelID,
			ID:         market.MessageID,
			Embeds:     &embeds,
			Components: &components,
		})
	}

	totalRefunded := int64(0)
	for _, amt := range refunds {
		totalRefunded += amt
	}

	s.ChannelMessageSendEmbed(channelID, utils.SuccessEmbed(
		locale.Text("commands.polymarket_cmd.market_cancelled_and_refunded"),
		locale.Text("commands.polymarket_cmd.the_market_was_cancelled_refunded_to_bettors.formatted", locale.Data{"Question": market.Question, "ID": market.ID, "TotalRefunded": totalRefunded, "CurrencySymbol": config.Bot.CurrencySymbol, "Value5": len(refunds)}),
	))
}

// HandlePolymarketButton routes button clicks starting with poly_
func HandlePolymarketButton(s *discordgo.Session, i *discordgo.InteractionCreate) {
	customID := i.MessageComponentData().CustomID

	if strings.HasPrefix(customID, "poly_candidate_select:") {
		handleCandidateSelectInteraction(s, i)
		return
	}

	parts := strings.Split(customID, "_")
	if len(parts) < 3 {
		return
	}

	action := parts[1] // "buy", "pos", "refresh", "approve", "reject"

	switch action {
	case "buy":
		// poly_buy_yes_<marketID> or poly_buy_no_<marketID>
		if len(parts) < 4 {
			return
		}
		outcome := strings.Title(strings.ToLower(parts[2])) // "Yes" or "No"
		marketID := strings.Join(parts[3:], "_")

		market, err := database.GetPolymarketMarketByID(marketID)
		if err != nil || market == nil || market.Status != "open" {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: locale.Text("commands.polymarket_cmd.this_market_is_no_longer_open_for"),
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})
			return
		}

		settings, _ := database.GetGuildPolymarketSettings(market.GuildID)
		if settings == nil {
			settings = &database.DBPolymarketSettings{HouseEdge: 0.03}
		}

		price := market.YesPrice
		if strings.EqualFold(outcome, "no") {
			price = market.NoPrice
		}

		modalResp := polymarket.CreateBuyModal(marketID, outcome, price, settings.HouseEdge)
		_ = s.InteractionRespond(i.Interaction, modalResp)

	case "pos":
		marketID := strings.Join(parts[2:], "_")
		userID := i.Member.User.ID
		positions, _ := database.GetUserMarketPositions(userID, marketID)

		if len(positions) == 0 {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: locale.Text("commands.polymarket_cmd.you_do_not_own_any_shares_in"),
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})
			return
		}

		var lines []string
		for _, p := range positions {
			avg := float64(p.TotalInvested) / float64(p.Shares)
			lines = append(lines, locale.Text("commands.polymarket_cmd.shares_average_price_ec_invested_potential_payout.formatted", locale.Data{"Outcome": p.Outcome, "Shares": p.Shares, "Avg": avg, "TotalInvested": p.TotalInvested, "CurrencySymbol": config.Bot.CurrencySymbol, "Shares6": p.Shares*100 - p.TotalInvested, "CurrencySymbol7": config.Bot.CurrencySymbol}))
		}

		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: locale.Text("commands.polymarket_cmd.your_shares_in_this_market.formatted", locale.Data{"Strings": strings.Join(lines, "\n")}),
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})

	case "refresh":
		marketID := strings.Join(parts[2:], "_")
		market, err := database.GetPolymarketMarketByID(marketID)
		if err != nil || market == nil {
			return
		}

		client := polymarket.GetClient()
		gammaMarket, err := client.GetMarketByID(market.PolymarketID)
		if err == nil && gammaMarket != nil {
			yesPrice, noPrice, _ := gammaMarket.GetPrices()
			market.YesPrice = yesPrice
			market.NoPrice = noPrice
			_ = database.UpdatePolymarketPricesDB(market.ID, yesPrice, noPrice, market.Status)
		}

		settings, _ := database.GetGuildPolymarketSettings(market.GuildID)
		if settings == nil {
			settings = &database.DBPolymarketSettings{HouseEdge: 0.03}
		}

		embed := polymarket.CreateMarketEmbed(market, settings)
		components := polymarket.CreateMarketComponents(market, settings)

		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Embeds:     []*discordgo.MessageEmbed{embed},
				Components: components,
			},
		})

	case "approve":
		polyID := parts[2]
		if !isUserAdmin(s, i.GuildID, i.Member.User.ID) {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: locale.Text("commands.polymarket_cmd.only_administrators_can_approve_market_suggestions"),
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})
			return
		}

		settings, _ := database.GetGuildPolymarketSettings(i.GuildID)
		if settings == nil || settings.ChannelID == "" {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: locale.Text("commands.polymarket_cmd.the_polymarket_channel_is_not_configured"),
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})
			return
		}

		// Acknowledge click and disable buttons
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Content: locale.Text("commands.polymarket_cmd.suggestion_approved_by_publishing_the_market.formatted", locale.Data{"ID": i.Member.User.ID}),
			},
		})

		importMarketInternal(s, i.GuildID, settings.ChannelID, i.ChannelID, i.Member.User.ID, polyID, "", settings)

	case "reject":
		if !isUserAdmin(s, i.GuildID, i.Member.User.ID) {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: locale.Text("commands.polymarket_cmd.only_administrators_can_reject_suggestions"),
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})
			return
		}

		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Content: locale.Text("commands.polymarket_cmd.suggestion_rejected_by.formatted", locale.Data{"ID": i.Member.User.ID}),
			},
		})
	}
}

func handleCandidateSelectInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.MessageComponentData()
	if len(data.Values) == 0 {
		return
	}
	selectedPolyID := data.Values[0]

	settings, err := database.GetGuildPolymarketSettings(i.GuildID)
	if err != nil || settings == nil {
		settings = &database.DBPolymarketSettings{ImportMode: "admin_only", HouseEdge: 0.03}
	}

	if settings.ChannelID == "" {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: locale.Text("commands.polymarket_cmd.the_polymarket_channel_is_not_configured_use"),
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	isAdmin := isUserAdmin(s, i.GuildID, i.Member.User.ID)
	if settings.ImportMode == "admin_only" && !isAdmin {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: locale.Text("commands.polymarket_cmd.only_administrators_can_import_markets_into_the"),
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	client := polymarket.GetClient()
	gammaMarket, err := client.GetMarketByID(selectedPolyID)
	if err != nil {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: locale.Text("commands.polymarket_cmd.unable_to_query_the_selected_market.formatted", locale.Data{"Err": err}),
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	candidateName := gammaMarket.GroupItemTitle
	if candidateName == "" {
		candidateName = gammaMarket.Question
	}

	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content:    locale.Text("commands.polymarket_cmd.selected_by_publishing_the_market.formatted", locale.Data{"CandidateName": candidateName, "ID": i.Member.User.ID}),
			Components: []discordgo.MessageComponent{},
		},
	})

	publishMarketToChannel(s, i.GuildID, settings.ChannelID, i.ChannelID, gammaMarket, settings)
}

// HandlePolymarketModalSubmit processes the share purchase submitted via modal
func HandlePolymarketModalSubmit(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ModalSubmitData()
	customID := data.CustomID

	// poly_modal_buy_<outcome>_<marketID>
	parts := strings.Split(customID, "_")
	if len(parts) < 5 {
		return
	}

	outcome := strings.Title(strings.ToLower(parts[3])) // "Yes" or "No"
	marketID := strings.Join(parts[4:], "_")
	userID := i.Member.User.ID

	var sharesStr string
	for _, row := range data.Components {
		if actionRow, ok := row.(*discordgo.ActionsRow); ok {
			for _, comp := range actionRow.Components {
				if input, ok := comp.(*discordgo.TextInput); ok {
					if input.CustomID == "shares" {
						sharesStr = strings.TrimSpace(input.Value)
					}
				}
			}
		}
	}

	shares, err := strconv.ParseInt(sharesStr, 10, 64)
	if err != nil || shares <= 0 {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: locale.Text("commands.polymarket_cmd.invalid_share_quantity_enter_a_whole_number"),
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	market, err := database.GetPolymarketMarketByID(marketID)
	if err != nil || market == nil || market.Status != "open" {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: locale.Text("commands.polymarket_cmd.this_market_has_closed_or_no_longer"),
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	settings, _ := database.GetGuildPolymarketSettings(market.GuildID)
	if settings == nil {
		settings = &database.DBPolymarketSettings{HouseEdge: 0.03}
	}

	price := market.YesPrice
	if strings.EqualFold(outcome, "no") {
		price = market.NoPrice
	}

	cost := polymarket.CalculateShareCost(price, shares, settings.HouseEdge)

	// Execute purchase transaction in PostgreSQL
	err = database.ExecuteBuySharesTransaction(market.ID, userID, outcome, shares, cost.PricePerShare, cost.Fee, cost.TotalCost)
	if err != nil {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: locale.Text("commands.polymarket_cmd.unable_to_complete_the_purchase.formatted", locale.Data{"Err": err}),
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	emoji := "🟢"
	if strings.EqualFold(outcome, "no") {
		emoji = "🔴"
	}

	receipt := locale.Text("commands.polymarket_cmd.shares_purchased_market_outcome_quantity_shares_total.formatted", locale.Data{"Question": market.Question, "Emoji": emoji, "Outcome": outcome, "Shares": cost.Shares, "TotalCost": cost.TotalCost, "CurrencySymbol": config.Bot.CurrencySymbol, "RawCost": cost.RawCost, "Fee": cost.Fee, "PotentialPayout": cost.PotentialPayout, "CurrencySymbol10": config.Bot.CurrencySymbol, "PotentialProfit": cost.PotentialProfit, "CurrencySymbol12": config.Bot.CurrencySymbol})

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: receipt,
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
}

// HandleSlashPolymarket handles the /poly slash command and subcommands
func HandleSlashPolymarket(s *discordgo.Session, i *discordgo.InteractionCreate) {
	options := i.ApplicationCommandData().Options
	if len(options) == 0 {
		return
	}

	subCmd := options[0].Name

	switch subCmd {
	case "trending":
		client := polymarket.GetClient()
		markets, err := client.GetTrendingMarkets(5)
		if err != nil {
			respondEmbed(s, i, utils.ErrorEmbed(locale.Text("commands.polymarket_cmd.unable_to_query_polymarket.formatted", locale.Data{"Err": err})))
			return
		}
		respondEmbed(s, i, buildMarketListEmbed(locale.Text("commands.polymarket_cmd.trending_polymarket_markets"), markets))

	case "search":
		query := options[0].Options[0].StringValue()
		client := polymarket.GetClient()
		markets, err := client.SearchMarkets(query, 5)
		if err != nil {
			respondEmbed(s, i, utils.ErrorEmbed(locale.Text("commands.polymarket_cmd.search_failed.formatted", locale.Data{"Err": err})))
			return
		}
		if len(markets) == 0 {
			respondEmbed(s, i, utils.InfoEmbed(locale.Text("commands.polymarket_cmd.no_results"), locale.Text("commands.polymarket_cmd.no_active_markets_found_for.formatted", locale.Data{"Query": query})))
			return
		}
		respondEmbed(s, i, buildMarketListEmbed(locale.Text("commands.polymarket_cmd.results_for.formatted", locale.Data{"Query": query}), markets))

	case "import":
		input := options[0].Options[0].StringValue()
		candidate := ""
		for _, opt := range options[0].Options {
			if opt.Name == "candidate" {
				candidate = opt.StringValue()
			}
		}
		settings, _ := database.GetGuildPolymarketSettings(i.GuildID)
		if settings == nil || settings.ChannelID == "" {
			respondEmbed(s, i, utils.ErrorEmbed(locale.Text("commands.polymarket_cmd.the_polymarket_channel_is_not_configured_an")))
			return
		}
		isAdmin := isUserAdmin(s, i.GuildID, i.Member.User.ID)
		if settings.ImportMode == "admin_only" && !isAdmin {
			respondEmbed(s, i, utils.ErrorEmbed(locale.Text("commands.polymarket_cmd.only_administrators_can_import_directly_use_poly")))
			return
		}
		respondEmbed(s, i, utils.InfoEmbed(locale.Text("commands.polymarket_cmd.importing"), locale.Text("commands.polymarket_cmd.fetching_polymarket_data")))
		importMarketInternal(s, i.GuildID, settings.ChannelID, i.ChannelID, i.Member.User.ID, input, candidate, settings)

	case "suggest":
		input := options[0].Options[0].StringValue()
		candidate := ""
		for _, opt := range options[0].Options {
			if opt.Name == "candidate" {
				candidate = opt.StringValue()
			}
		}
		respondEmbed(s, i, utils.InfoEmbed(locale.Text("commands.polymarket_cmd.submitting_suggestion"), locale.Text("commands.polymarket_cmd.processing_your_request")))
		suggestMarket(s, i.GuildID, i.ChannelID, i.Member.User, input, candidate)

	case "cancel":
		marketID := options[0].Options[0].StringValue()
		respondEmbed(s, i, utils.InfoEmbed(locale.Text("commands.polymarket_cmd.cancelling_market"), locale.Text("commands.polymarket_cmd.checking_the_market_and_issuing_refunds")))
		cancelMarket(s, i.GuildID, i.ChannelID, i.Member.User, marketID)

	case "view":
		marketID := options[0].Options[0].StringValue()
		market, err := database.GetPolymarketMarketByID(marketID)
		if err != nil || market == nil {
			respondEmbed(s, i, utils.ErrorEmbed(locale.Text("commands.polymarket_cmd.market_not_found_in_the_local_catalog")))
			return
		}
		settings, _ := database.GetGuildPolymarketSettings(market.GuildID)
		if settings == nil {
			settings = &database.DBPolymarketSettings{HouseEdge: 0.03}
		}
		embed := polymarket.CreateMarketEmbed(market, settings)
		components := polymarket.CreateMarketComponents(market, settings)
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Embeds:     []*discordgo.MessageEmbed{embed},
				Components: components,
			},
		})

	case "portfolio":
		positions, err := database.GetUserAllPositions(i.Member.User.ID)
		if err != nil {
			respondEmbed(s, i, utils.ErrorEmbed(locale.Text("commands.polymarket_cmd.unable_to_load_positions")))
			return
		}
		markets := make(map[string]*database.DBPolymarketMarket)
		for _, p := range positions {
			if _, exists := markets[p.MarketID]; !exists {
				m, _ := database.GetPolymarketMarketByID(p.MarketID)
				if m != nil {
					markets[p.MarketID] = m
				}
			}
		}
		embed := polymarket.CreatePortfolioEmbed(i.Member.User.ID, positions, markets)
		respondEmbed(s, i, embed)

	case "config":
		if !isUserAdmin(s, i.GuildID, i.Member.User.ID) {
			respondEmbed(s, i, utils.ErrorEmbed(locale.Text("commands.polymarket_cmd.only_administrators_can_use_this_command")))
			return
		}
		settings, _ := database.GetGuildPolymarketSettings(i.GuildID)
		if settings == nil {
			settings = &database.DBPolymarketSettings{GuildID: i.GuildID, ImportMode: "admin_only", HouseEdge: 0.03, MinBet: 10}
		}
		for _, opt := range options[0].Options {
			switch opt.Name {
			case "channel":
				ch := opt.ChannelValue(s)
				if ch != nil {
					settings.ChannelID = ch.ID
				}
			case "import_mode":
				settings.ImportMode = opt.StringValue()
			case "house_edge":
				settings.HouseEdge = opt.FloatValue()
			}
		}
		_ = database.UpdateGuildPolymarketSettings(settings)
		respondEmbed(s, i, utils.SuccessEmbed(locale.Text("commands.polymarket_cmd.settings_updated"), locale.Text("commands.polymarket_cmd.polymarket_settings_saved")))
	}
}

func isUserAdmin(s *discordgo.Session, guildID, userID string) bool {
	member, err := s.GuildMember(guildID, userID)
	if err != nil {
		return false
	}
	guild, err := s.Guild(guildID)
	if err == nil && guild.OwnerID == userID {
		return true
	}
	for _, roleID := range member.Roles {
		role, err := s.State.Role(guildID, roleID)
		if err == nil && (role.Permissions&discordgo.PermissionAdministrator != 0 || role.Permissions&discordgo.PermissionManageServer != 0) {
			return true
		}
	}
	return false
}

package commands

import (
	"bot/internal/database"
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

// CmdPolymarket handles text commands starting with !poly or !polymarket
func CmdPolymarket(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	if len(args) == 0 {
		sendPolymarketHelp(s, m.ChannelID)
		return
	}

	subCmd := strings.ToLower(args[0])

	switch subCmd {
	case "help", "ajuda":
		sendPolymarketHelp(s, m.ChannelID)

	case "trending", "populares", "alta":
		handleTextTrending(s, m.ChannelID)

	case "search", "buscar", "pesquisar":
		if len(args) < 2 {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Uso: `!poly search <termo>`\nExemplo: `!poly search bitcoin`"))
			return
		}
		query := strings.Join(args[1:], " ")
		handleTextSearch(s, m.ChannelID, query)

	case "import", "importar":
		if len(args) < 2 {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Uso: `!poly import <slug_ou_url> [candidato]`\nExemplo: `!poly import brazil-presidential-election lula`\nOu digite apenas o slug/URL para escolher no menu interativo!"))
			return
		}
		candidate := ""
		if len(args) >= 3 {
			candidate = strings.Join(args[2:], " ")
		}
		handleTextImport(s, m, args[1], candidate)

	case "suggest", "sugerir":
		if len(args) < 2 {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Uso: `!poly suggest <slug_ou_url> [candidato]`\nExemplo: `!poly suggest brazil-presidential-election lula`"))
			return
		}
		candidate := ""
		if len(args) >= 3 {
			candidate = strings.Join(args[2:], " ")
		}
		handleTextSuggest(s, m, args[1], candidate)

	case "cancel", "cancelar", "fechar", "reembolsar":
		if len(args) < 2 {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Uso: `!poly cancel <market_id>`\nExemplo: `!poly cancel poly_601818`"))
			return
		}
		handleTextCancelMarket(s, m, args[1])

	case "view", "ver", "mercado":
		if len(args) < 2 {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Uso: `!poly view <market_id>`"))
			return
		}
		handleTextView(s, m.ChannelID, args[1])

	case "portfolio", "portfólio", "carteira", "posicoes", "posições":
		handleTextPortfolio(s, m.ChannelID, m.Author.ID)

	case "config", "configurar":
		handleTextConfig(s, m, args[1:])

	default:
		// Attempt to view or import if input looks like a slug/URL
		handleTextAutoResolve(s, m, args[0])
	}
}

func sendPolymarketHelp(s *discordgo.Session, channelID string) {
	embed := &discordgo.MessageEmbed{
		Title: "🔮 Polymarket Prediction Markets - Comandos",
		Description: "Aposte em eventos reais do mundo (política, esportes, cripto, IA) com EstudoCoins (EC) comprando ações de **SIM** e **NÃO**!\n\n" +
			"**Comandos Disponíveis:**\n" +
			"`!poly trending` - Ver mercados com maior volume global\n" +
			"`!poly search <termo>` - Pesquisar eventos reais no Polymarket\n" +
			"`!poly import <slug_ou_url> [candidato]` - Importar mercado (com seletor interativo para eleições)\n" +
			"`!poly suggest <slug_ou_url> [candidato]` - Sugerir mercado para aprovação de admins\n" +
			"`!poly cancel <id>` - Cancelar mercado e reembolsar apostadores (apenas admins)\n" +
			"`!poly view <id>` - Ver detalhes e cotações de um mercado importado\n" +
			"`!poly portfolio` - Ver suas ações e lucros potenciais\n" +
			"`!poly config` - Configurar permissões e canal (apenas administradores)\n\n" +
			"*Cada ação vencedora é liquidada por exatamente 100 EC no desfecho oficial do evento!*",
		Color: 0x5865f2,
		Footer: &discordgo.MessageEmbedFooter{
			Text: "Oráculo automatizado com dados ao vivo da Gamma API do Polymarket",
		},
	}
	_, _ = s.ChannelMessageSendEmbed(channelID, embed)
}

func handleTextTrending(s *discordgo.Session, channelID string) {
	client := polymarket.GetClient()
	markets, err := client.GetTrendingMarkets(5)
	if err != nil {
		s.ChannelMessageSendEmbed(channelID, utils.ErrorEmbed(fmt.Sprintf("Erro ao consultar Polymarket: %v", err)))
		return
	}

	embed := buildMarketListEmbed("🔥 Mercados em Alta no Polymarket", markets)
	_, _ = s.ChannelMessageSendEmbed(channelID, embed)
}

func handleTextSearch(s *discordgo.Session, channelID, query string) {
	client := polymarket.GetClient()
	markets, err := client.SearchMarkets(query, 5)
	if err != nil {
		s.ChannelMessageSendEmbed(channelID, utils.ErrorEmbed(fmt.Sprintf("Erro na pesquisa: %v", err)))
		return
	}

	if len(markets) == 0 {
		s.ChannelMessageSendEmbed(channelID, utils.InfoEmbed("Nenhum resultado", fmt.Sprintf("Nenhum mercado ativo encontrado para `%s`. Tente outros termos em inglês.", query)))
		return
	}

	embed := buildMarketListEmbed(fmt.Sprintf("🔍 Resultados para: \"%s\"", query), markets)
	_, _ = s.ChannelMessageSendEmbed(channelID, embed)
}

func buildMarketListEmbed(title string, markets []*polymarket.GammaMarket) *discordgo.MessageEmbed {
	var lines []string
	for i, m := range markets {
		yesPrice, noPrice, _ := m.GetPrices()
		yesPct := int(yesPrice * 100)
		noPct := int(noPrice * 100)

		lines = append(lines, fmt.Sprintf(
			"**%d. %s**\n"+
				"• Probabilidade: 🟢 `%d%% Sim` | 🔴 `%d%% Não`\n"+
				"• Volume: `%s` | Slug: `%s`\n"+
				"• *Importar:* `!poly import %s`\n",
			i+1, m.Question, yesPct, noPct, m.FormatVolume(), m.Slug, m.Slug,
		))
	}

	return &discordgo.MessageEmbed{
		Title:       title,
		Description: strings.Join(lines, "\n"),
		Color:       0x5865f2,
		Footer: &discordgo.MessageEmbedFooter{
			Text: "Use !poly import <slug> para abrir as apostas no servidor!",
		},
	}
}

func handleTextImport(s *discordgo.Session, m *discordgo.MessageCreate, input string, candidate string) {
	guildID := m.GuildID
	if guildID == "" {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Este comando só pode ser usado dentro de um servidor Discord."))
		return
	}

	settings, err := database.GetGuildPolymarketSettings(guildID)
	if err != nil {
		settings = &database.DBPolymarketSettings{ImportMode: "admin_only", HouseEdge: 0.03}
	}

	// Verify channel configuration
	if settings.ChannelID == "" {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed(
			"⚠️ **Canal do Polymarket não configurado!**\n"+
				"Um administrador precisa definir o canal exclusivo de apostas usando:\n"+
				"`!poly config channel #canal` ou `/poly config channel`",
		))
		return
	}

	// Verify permissions
	isAdmin := isUserAdmin(s, guildID, m.Author.ID)
	if settings.ImportMode == "admin_only" && !isAdmin {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed(
			"⚠️ **Apenas administradores podem importar mercados diretamente.**\n"+
				"Você pode sugerir este evento usando: `!poly suggest "+input+"`",
		))
		return
	}

	importMarketInternal(s, guildID, settings.ChannelID, m.ChannelID, m.Author.ID, input, candidate, settings)
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
		lines = append(lines, fmt.Sprintf(
			"**%d. %s**\n• Cotação: 🟢 `%.0f%% Sim` | 🔴 `%.0f%% Não` | Volume: `%s`",
			idx+1, title, yesP*100, noP*100, m.FormatVolume(),
		))
	}

	embed := &discordgo.MessageEmbed{
		Title: fmt.Sprintf("🗳️ Selecione o Candidato: %s", event.Title),
		Description: fmt.Sprintf(
			"Este evento possui **%d mercados/candidatos** no Polymarket.\n\n"+
				"%s\n\n"+
				"👇 **Selecione no menu abaixo o candidato que deseja abrir para apostas no servidor:**\n"+
				"*💡 Dica: Você também pode importar direto usando: `!poly import %s <nome>`*",
			len(event.Markets), strings.Join(lines, "\n"), event.Slug,
		),
		Color: 0x5865f2,
		Thumbnail: &discordgo.MessageEmbedThumbnail{
			URL: event.Image,
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: "Oráculo automatizado com dados oficiais do Polymarket",
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
		desc := fmt.Sprintf("%.0f%% Sim | %.0f%% Não | Vol: %s", yesP*100, noP*100, m.FormatVolume())
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
		Placeholder: "Clique aqui para escolher o candidato...",
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
				"Mercado Já Ativo",
				fmt.Sprintf("Este mercado já está aberto para apostas em <#%s>!\nID interno: `%s`", existing.ChannelID, existing.ID),
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
		Category:     "Geral",
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
			s.ChannelMessageSendEmbed(responseChannelID, utils.ErrorEmbed(fmt.Sprintf("Erro ao publicar mercado em <#%s>: %v", targetChannelID, err)))
		}
		return
	}

	dbMarket.MessageID = msg.ID
	if err := database.CreatePolymarketMarketDB(dbMarket); err != nil {
		log.Printf("[PolymarketCmd] Error saving market to DB: %v", err)
	}

	if responseChannelID != "" && responseChannelID != targetChannelID {
		s.ChannelMessageSendEmbed(responseChannelID, utils.SuccessEmbed(
			"Mercado Importado!",
			fmt.Sprintf("O mercado **%s** foi aberto para apostas com sucesso em <#%s>!\nID: `%s`", gammaMarket.Question, targetChannelID, dbMarket.ID),
		))
	}
}

func importMarketInternal(s *discordgo.Session, guildID, targetChannelID, responseChannelID, userID, input, candidate string, settings *database.DBPolymarketSettings) {
	client := polymarket.GetClient()
	gammaMarket, gammaEvent, err := client.ResolveImportQuery(input, candidate)
	if err != nil {
		s.ChannelMessageSendEmbed(responseChannelID, utils.ErrorEmbed(fmt.Sprintf("Erro ao consultar Polymarket: %v", err)))
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

func handleTextSuggest(s *discordgo.Session, m *discordgo.MessageCreate, input string, candidate string) {
	guildID := m.GuildID
	if guildID == "" {
		return
	}

	settings, _ := database.GetGuildPolymarketSettings(guildID)
	if settings == nil || settings.ChannelID == "" {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Canal do Polymarket não configurado no servidor. Avise um administrador!"))
		return
	}

	client := polymarket.GetClient()
	gammaMarket, gammaEvent, err := client.ResolveImportQuery(input, candidate)
	if err != nil {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed(fmt.Sprintf("Não foi possível encontrar este evento no Polymarket: %v", err)))
		return
	}

	if gammaEvent != nil && gammaMarket == nil {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.InfoEmbed(
			"Especifique o Candidato",
			fmt.Sprintf("O evento **%s** possui múltiplos candidatos.\nPor favor, especifique o candidato desejado:\n`!poly suggest %s <nome_do_candidato>`", gammaEvent.Title, gammaEvent.Slug),
		))
		return
	}

	yesPrice, noPrice, _ := gammaMarket.GetPrices()

	suggestionEmbed := &discordgo.MessageEmbed{
		Title: "💡 Sugestão de Mercado Polymarket",
		Description: fmt.Sprintf(
			"O usuário <@%s> sugeriu abrir apostas para o seguinte evento:\n\n"+
				"### %s\n"+
				"• Cotações Atuais: 🟢 **%.0f%% Sim** | 🔴 **%.0f%% Não**\n"+
				"• Volume Global: `%s`\n"+
				"• Slug: `%s`\n\n"+
				"Administradores podem aprovar para abrir as apostas no canal oficial!",
			m.Author.ID, gammaMarket.Question, yesPrice*100, noPrice*100, gammaMarket.FormatVolume(), gammaMarket.Slug,
		),
		Color: 0xf1c40f,
		Thumbnail: &discordgo.MessageEmbedThumbnail{
			URL: gammaMarket.Image,
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("Polymarket ID: %s | Sugerido por %s", gammaMarket.ID, m.Author.Username),
		},
		Timestamp: time.Now().Format(time.RFC3339),
	}

	actionRow := discordgo.ActionsRow{
		Components: []discordgo.MessageComponent{
			discordgo.Button{
				CustomID: fmt.Sprintf("poly_approve_%s", gammaMarket.ID),
				Label:    "Aprovar e Importar",
				Style:    discordgo.SuccessButton,
				Emoji:    &discordgo.ComponentEmoji{Name: "✅"},
			},
			discordgo.Button{
				CustomID: fmt.Sprintf("poly_reject_%s", gammaMarket.ID),
				Label:    "Recusar",
				Style:    discordgo.DangerButton,
				Emoji:    &discordgo.ComponentEmoji{Name: "❌"},
			},
		},
	}

	_, _ = s.ChannelMessageSendComplex(settings.ChannelID, &discordgo.MessageSend{
		Embeds:     []*discordgo.MessageEmbed{suggestionEmbed},
		Components: []discordgo.MessageComponent{actionRow},
	})

	s.ChannelMessageSendEmbed(m.ChannelID, utils.SuccessEmbed(
		"Sugestão Enviada!",
		fmt.Sprintf("Sua sugestão de mercado foi enviada para moderação em <#%s>.", settings.ChannelID),
	))
}

func handleTextCancelMarket(s *discordgo.Session, m *discordgo.MessageCreate, query string) {
	guildID := m.GuildID
	if guildID == "" {
		return
	}
	if !isUserAdmin(s, guildID, m.Author.ID) {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Apenas administradores podem cancelar mercados."))
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
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed(fmt.Sprintf("Mercado com ID `%s` não foi encontrado no banco de dados.", query)))
		return
	}

	if market.Status != "open" && market.Status != "closed" {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed(fmt.Sprintf("Este mercado já se encontra com status `%s`.", market.Status)))
		return
	}

	refunds, err := database.CancelAndRefundPolymarketMarketDB(market.ID)
	if err != nil {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed(fmt.Sprintf("Erro ao cancelar mercado: %v", err)))
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

	s.ChannelMessageSendEmbed(m.ChannelID, utils.SuccessEmbed(
		"Mercado Cancelado e Reembolsado",
		fmt.Sprintf("O mercado **%s** (`%s`) foi cancelado com sucesso!\nTotal reembolsado: **%d %s** para **%d** apostador(es).",
			market.Question, market.ID, totalRefunded, config.Bot.CurrencySymbol, len(refunds)),
	))
}

func handleTextView(s *discordgo.Session, channelID, marketID string) {
	market, err := database.GetPolymarketMarketByID(marketID)
	if err != nil || market == nil {
		s.ChannelMessageSendEmbed(channelID, utils.ErrorEmbed("Mercado não encontrado no banco de dados local. Use `!poly search` para encontrar mercados."))
		return
	}

	settings, _ := database.GetGuildPolymarketSettings(market.GuildID)
	if settings == nil {
		settings = &database.DBPolymarketSettings{HouseEdge: 0.03}
	}

	embed := polymarket.CreateMarketEmbed(market, settings)
	components := polymarket.CreateMarketComponents(market, settings)

	_, _ = s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Embeds:     []*discordgo.MessageEmbed{embed},
		Components: components,
	})
}

func handleTextPortfolio(s *discordgo.Session, channelID, userID string) {
	positions, err := database.GetUserAllPositions(userID)
	if err != nil {
		s.ChannelMessageSendEmbed(channelID, utils.ErrorEmbed("Erro ao consultar suas posições no banco de dados."))
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

	embed := polymarket.CreatePortfolioEmbed(userID, positions, markets)
	_, _ = s.ChannelMessageSendEmbed(channelID, embed)
}

func handleTextConfig(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	guildID := m.GuildID
	if guildID == "" {
		return
	}

	if !isUserAdmin(s, guildID, m.Author.ID) {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("❌ Apenas administradores do servidor podem alterar as configurações do Polymarket."))
		return
	}

	settings, err := database.GetGuildPolymarketSettings(guildID)
	if err != nil {
		settings = &database.DBPolymarketSettings{GuildID: guildID, ImportMode: "admin_only", HouseEdge: 0.03, MinBet: 10}
	}

	if len(args) == 0 {
		channelText := "Nenhum (não configurado)"
		if settings.ChannelID != "" {
			channelText = fmt.Sprintf("<#%s>", settings.ChannelID)
		}

		info := fmt.Sprintf(
			"**Configurações Atuais do Polymarket:**\n\n"+
				"• **Canal Dedicado:** %s\n"+
				"• **Permissão de Importação:** `%s` (`admin_only` ou `all_users`)\n"+
				"• **Taxa da Casa (House Edge):** `%.1f%%`\n"+
				"• **Aposta Mínima:** `%d %s`\n\n"+
				"**Como alterar:**\n"+
				"`!poly config channel #canal` - Definir canal exclusivo\n"+
				"`!poly config import_mode <admin_only|all_users>` - Quem pode importar\n"+
				"`!poly config house_edge <porcentagem>` - Ex: `3%%` ou `0.03`\n",
			channelText, settings.ImportMode, settings.HouseEdge*100, settings.MinBet, config.Bot.CurrencySymbol,
		)
		s.ChannelMessageSendEmbed(m.ChannelID, utils.InfoEmbed("Configuração do Polymarket", info))
		return
	}

	param := strings.ToLower(args[0])
	if len(args) < 2 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Especifique o novo valor. Exemplo: `!poly config channel #apostas`"))
		return
	}
	val := args[1]

	switch param {
	case "channel", "canal":
		cleanID := strings.Trim(val, "<#>")
		settings.ChannelID = cleanID
		_ = database.UpdateGuildPolymarketSettings(settings)
		s.ChannelMessageSendEmbed(m.ChannelID, utils.SuccessEmbed("Canal Configurado", fmt.Sprintf("Canal exclusivo do Polymarket definido para <#%s>.", cleanID)))

	case "import_mode", "permissao", "permissão", "mode":
		val = strings.ToLower(val)
		if val != "admin_only" && val != "all_users" {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Modo inválido! Escolha entre `admin_only` ou `all_users`."))
			return
		}
		settings.ImportMode = val
		_ = database.UpdateGuildPolymarketSettings(settings)
		s.ChannelMessageSendEmbed(m.ChannelID, utils.SuccessEmbed("Permissão Atualizada", fmt.Sprintf("Modo de importação configurado para `%s`.", val)))

	case "house_edge", "taxa", "edge":
		val = strings.TrimSuffix(val, "%")
		parsed, err := strconv.ParseFloat(val, 64)
		if err != nil || parsed < 0 || parsed > 20 {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Taxa inválida! Digite um valor entre 0% e 20% (ex: `3%` ou `5%`)."))
			return
		}
		if parsed > 1.0 {
			parsed = parsed / 100.0
		}
		settings.HouseEdge = parsed
		_ = database.UpdateGuildPolymarketSettings(settings)
		s.ChannelMessageSendEmbed(m.ChannelID, utils.SuccessEmbed("Taxa Atualizada", fmt.Sprintf("Taxa da casa configurada para `%.1f%%`.", parsed*100)))

	default:
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Parâmetro desconhecido. Opções: `channel`, `import_mode`, `house_edge`."))
	}
}

func handleTextAutoResolve(s *discordgo.Session, m *discordgo.MessageCreate, query string) {
	// If query matches a market ID in DB
	market, _ := database.GetPolymarketMarketByID(query)
	if market != nil {
		handleTextView(s, m.ChannelID, market.ID)
		return
	}
	sendPolymarketHelp(s, m.ChannelID)
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
					Content: "❌ Este mercado não está mais aberto para negociações.",
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
		if outcome == "No" {
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
					Content: "💼 Você ainda não possui ações neste mercado.",
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})
			return
		}

		var lines []string
		for _, p := range positions {
			avg := float64(p.TotalInvested) / float64(p.Shares)
			lines = append(lines, fmt.Sprintf("• **%s:** %d ações (Preço médio: `%.1f EC` | Investido: `%d %s` | Payout potencial: `+%d %s`)",
				p.Outcome, p.Shares, avg, p.TotalInvested, config.Bot.CurrencySymbol, p.Shares*100-p.TotalInvested, config.Bot.CurrencySymbol))
		}

		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: fmt.Sprintf("💼 **Suas ações neste mercado:**\n%s", strings.Join(lines, "\n")),
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
					Content: "❌ Apenas administradores podem aprovar sugestões de mercado.",
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
					Content: "⚠️ Canal do Polymarket não configurado no servidor!",
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})
			return
		}

		// Acknowledge click and disable buttons
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Content: fmt.Sprintf("✅ Sugestão aprovada por <@%s>! Mercado sendo publicado...", i.Member.User.ID),
			},
		})

		importMarketInternal(s, i.GuildID, settings.ChannelID, i.ChannelID, i.Member.User.ID, polyID, "", settings)

	case "reject":
		if !isUserAdmin(s, i.GuildID, i.Member.User.ID) {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: "❌ Apenas administradores podem recusar sugestões.",
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})
			return
		}

		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Content: fmt.Sprintf("❌ Sugestão recusada por <@%s>.", i.Member.User.ID),
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
				Content: "⚠️ Canal do Polymarket não configurado no servidor! Use `!poly config channel #canal`.",
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
				Content: "❌ Apenas administradores podem importar mercados para o canal oficial.",
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
				Content: fmt.Sprintf("❌ Erro ao consultar o mercado selecionado: %v", err),
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
			Content: fmt.Sprintf("✅ **%s** selecionado por <@%s>! Publicando mercado no canal oficial...", candidateName, i.Member.User.ID),
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
				Content: "❌ Quantidade inválida de ações! Digite um número inteiro maior que 0.",
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
				Content: "❌ Este mercado já encerrou ou não aceita mais apostas.",
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
	if outcome == "No" {
		price = market.NoPrice
	}

	cost := polymarket.CalculateShareCost(price, shares, settings.HouseEdge)

	// Execute purchase transaction in PostgreSQL
	err = database.ExecuteBuySharesTransaction(market.ID, userID, outcome, shares, cost.PricePerShare, cost.Fee, cost.TotalCost)
	if err != nil {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: fmt.Sprintf("❌ Erro ao concluir compra: %v", err),
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	emoji := "🟢"
	if outcome == "No" {
		emoji = "🔴"
	}

	receipt := fmt.Sprintf(
		"✅ **Ações Compradas com Sucesso!**\n\n"+
			"📌 **Mercado:** %s\n"+
			"🎯 **Opção:** %s **%s**\n"+
			"📦 **Quantidade:** **%d ações**\n"+
			"💵 **Total Pago:** **%d %s** *(Custo: %d EC + Taxa: %d EC)*\n"+
			"🏆 **Retorno se Vencer:** **%d %s** *(Lucro: +%d %s)*\n\n"+
			"Suas ações foram guardadas no seu portfólio (`/poly portfolio`).",
		market.Question, emoji, outcome, cost.Shares,
		cost.TotalCost, config.Bot.CurrencySymbol, cost.RawCost, cost.Fee,
		cost.PotentialPayout, config.Bot.CurrencySymbol, cost.PotentialProfit, config.Bot.CurrencySymbol,
	)

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
			respondEmbed(s, i, utils.ErrorEmbed(fmt.Sprintf("Erro ao consultar Polymarket: %v", err)))
			return
		}
		respondEmbed(s, i, buildMarketListEmbed("🔥 Mercados em Alta no Polymarket", markets))

	case "search":
		query := options[0].Options[0].StringValue()
		client := polymarket.GetClient()
		markets, err := client.SearchMarkets(query, 5)
		if err != nil {
			respondEmbed(s, i, utils.ErrorEmbed(fmt.Sprintf("Erro na pesquisa: %v", err)))
			return
		}
		if len(markets) == 0 {
			respondEmbed(s, i, utils.InfoEmbed("Nenhum resultado", fmt.Sprintf("Nenhum mercado ativo encontrado para `%s`.", query)))
			return
		}
		respondEmbed(s, i, buildMarketListEmbed(fmt.Sprintf("🔍 Resultados para: \"%s\"", query), markets))

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
			respondEmbed(s, i, utils.ErrorEmbed("Canal do Polymarket não configurado! Um admin precisa definir com `/poly config channel`."))
			return
		}
		isAdmin := isUserAdmin(s, i.GuildID, i.Member.User.ID)
		if settings.ImportMode == "admin_only" && !isAdmin {
			respondEmbed(s, i, utils.ErrorEmbed("Apenas administradores podem importar diretamente. Use `/poly suggest`."))
			return
		}
		respondEmbed(s, i, utils.InfoEmbed("Importando...", "Buscando dados no Polymarket..."))
		importMarketInternal(s, i.GuildID, settings.ChannelID, i.ChannelID, i.Member.User.ID, input, candidate, settings)

	case "suggest":
		input := options[0].Options[0].StringValue()
		candidate := ""
		for _, opt := range options[0].Options {
			if opt.Name == "candidate" {
				candidate = opt.StringValue()
			}
		}
		// Wrap text suggestion logic
		msgDummy := &discordgo.MessageCreate{
			Message: &discordgo.Message{
				ChannelID: i.ChannelID,
				GuildID:   i.GuildID,
				Author:    i.Member.User,
			},
		}
		respondEmbed(s, i, utils.InfoEmbed("Enviando sugestão...", "Processando seu pedido..."))
		handleTextSuggest(s, msgDummy, input, candidate)

	case "cancel":
		marketID := options[0].Options[0].StringValue()
		msgDummy := &discordgo.MessageCreate{
			Message: &discordgo.Message{
				ChannelID: i.ChannelID,
				GuildID:   i.GuildID,
				Author:    i.Member.User,
			},
		}
		respondEmbed(s, i, utils.InfoEmbed("Processando cancelamento...", "Verificando mercado e reembolsando..."))
		handleTextCancelMarket(s, msgDummy, marketID)

	case "view":
		marketID := options[0].Options[0].StringValue()
		market, err := database.GetPolymarketMarketByID(marketID)
		if err != nil || market == nil {
			respondEmbed(s, i, utils.ErrorEmbed("Mercado não encontrado no banco de dados local."))
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
			respondEmbed(s, i, utils.ErrorEmbed("Erro ao consultar posições."))
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
			respondEmbed(s, i, utils.ErrorEmbed("Apenas administradores podem usar este comando."))
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
		respondEmbed(s, i, utils.SuccessEmbed("Configurações Atualizadas", "As configurações do Polymarket foram salvas com sucesso!"))
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

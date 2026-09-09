package commands

import (
	"bot/internal/database"
	"bot/internal/games"
	"bot/pkg/config"
	"bot/pkg/utils"
	"fmt"
	"strconv"
	"strings"

	"github.com/bwmarrin/discordgo"
)

// CmdBicho handles text commands for Jogo do Bicho
func CmdBicho(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	if len(args) == 0 {
		showBichoPanel(s, m)
		return
	}

	subCmd := strings.ToLower(args[0])
	switch subCmd {
	case "painel", "status", "info", "rodada":
		showBichoPanel(s, m)

	case "apostar", "aposta", "bet":
		handleBichoTextBet(s, m, args[1:])

	case "tabela", "animais", "grupos", "bichos":
		s.ChannelMessageSendEmbed(m.ChannelID, games.CreateBichoTableEmbed())

	case "apostas", "minhas-apostas", "minhasapostas", "bilhetes":
		showUserBichoBets(s, m)

	case "sortear", "draw":
		handleBichoManualDraw(s, m)

	case "config", "configurar":
		handleBichoConfig(s, m, args[1:])

	case "ajuda", "help", "regras":
		sendBichoHelp(s, m.ChannelID)

	default:
		sendBichoHelp(s, m.ChannelID)
	}
}

// showBichoPanel displays the active round embed with buttons or directs the user to configure it
func showBichoPanel(s *discordgo.Session, m *discordgo.MessageCreate) {
	guildID := m.GuildID
	if guildID == "" {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Este comando só pode ser utilizado em um servidor."))
		return
	}

	settings, err := database.GetBichoSettings(guildID)
	if err != nil || settings == nil || settings.ChannelID == "" {
		if isUserAdmin(s, guildID, m.Author.ID) {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.InfoEmbed("Configuração Necessária",
				"🎲 **O canal oficial do Jogo do Bicho ainda não foi definido!**\n\n"+
					"Defina o canal exclusivo usando:\n"+
					"`!bicho config canal #canal` ou `/bicho config canal:#canal`"))
		} else {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("O Jogo do Bicho ainda não foi configurado neste servidor pelos administradores."))
		}
		return
	}

	round, err := database.GetActiveBichoRound(guildID)
	if err != nil || round == nil {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.InfoEmbed("Jogo do Bicho", "Não há nenhuma rodada aberta no momento. Aguarde o próximo sorteio."))
		return
	}

	totalBets, totalAmount, uniqueBettors, _ := database.GetBichoRoundStats(round.ID)
	embed := games.CreateBichoRoundEmbed(round, totalBets, totalAmount, uniqueBettors)
	components := games.CreateBichoRoundComponents(round.ID)

	s.ChannelMessageSendComplex(m.ChannelID, &discordgo.MessageSend{
		Embeds:     []*discordgo.MessageEmbed{embed},
		Components: components,
	})
}

// handleBichoTextBet processes bets made through text command: !bicho apostar <modalidade> <alvo> <valor> [posicao]
func handleBichoTextBet(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	if len(args) < 3 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.InfoEmbed(
			"Como Apostar no Jogo do Bicho",
			"**Sintaxe:**\n"+
				"`!bicho apostar <modalidade> <alvo> <valor> [posicao]`\n\n"+
				"**Modalidades:**\n"+
				"• `grupo` (Ex: `!bicho apostar grupo macaco 100` ou `!bicho apostar grupo 17 100 cercado`)\n"+
				"• `dezena` (Ex: `!bicho apostar dezena 28 50`)\n"+
				"• `centena` (Ex: `!bicho apostar centena 528 20 cercado`)\n"+
				"• `milhar` (Ex: `!bicho apostar milhar 4528 10`)\n"+
				"• `duque` (Ex: `!bicho apostar duque \"macaco, leao\" 50`)\n"+
				"• `terno` (Ex: `!bicho apostar terno \"macaco, leao, tigre\" 20`)\n\n"+
				"**Posições:**\n"+
				"• `cabeca` (1º prêmio - padrão)\n"+
				"• `cercado` (1º ao 5º prêmio)",
		))
		return
	}

	modality := args[0]
	var target, scope string
	var amount int64

	// Determine if last argument is scope (cabeca/cercado)
	lastArg := strings.ToLower(args[len(args)-1])
	isScopeLast := lastArg == "cabeca" || lastArg == "cabeça" || lastArg == "cercado" || lastArg == "cer" || lastArg == "cab"

	if isScopeLast {
		scope = lastArg
		if len(args) < 4 {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Formato de aposta incompleto. Use `!bicho apostar <modalidade> <alvo> <valor> <posicao>`"))
			return
		}
		val, err := strconv.ParseInt(args[len(args)-2], 10, 64)
		if err != nil || val <= 0 {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Valor da aposta inválido!"))
			return
		}
		amount = val
		target = strings.Join(args[1:len(args)-2], " ")
	} else {
		scope = "cabeca"
		val, err := strconv.ParseInt(args[len(args)-1], 10, 64)
		if err != nil || val <= 0 {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Valor da aposta inválido! O valor deve ser o último parâmetro."))
			return
		}
		amount = val
		target = strings.Join(args[1:len(args)-1], " ")
	}

	res, err := games.GetBichoManager().PlaceBet(m.GuildID, m.Author.ID, modality, target, scope, amount)
	if err != nil {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed(fmt.Sprintf("❌ Não foi possível registrar a aposta: %s", err.Error())))
		return
	}

	ticketEmbed := createBetTicketEmbed(m.Author, res)
	s.ChannelMessageSendEmbed(m.ChannelID, ticketEmbed)
}

// showUserBichoBets lists all bets placed by the user in the active round
func showUserBichoBets(s *discordgo.Session, m *discordgo.MessageCreate) {
	round, err := database.GetActiveBichoRound(m.GuildID)
	if err != nil || round == nil {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.InfoEmbed("Jogo do Bicho", "Não há nenhuma rodada aberta no momento."))
		return
	}

	bets, err := database.GetUserRoundBetsDB(round.ID, m.Author.ID)
	if err != nil {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Erro ao consultar suas apostas."))
		return
	}

	if len(bets) == 0 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.InfoEmbed(
			fmt.Sprintf("Seus Bilhetes - Rodada #%d", round.RoundNumber),
			"Você ainda não realizou apostas nesta rodada.\nClique no botão **Apostar** no canal oficial ou use `!bicho apostar`!",
		))
		return
	}

	var totalInvested int64
	var lines []string
	for i, b := range bets {
		totalInvested += b.Amount
		scopeStr := "Cabeça"
		if b.Scope == "cercado" {
			scopeStr = "Cercado (1º ao 5º)"
		}
		lines = append(lines, fmt.Sprintf("`%d.` **%s** (%s) no alvo **%s** | `%d %s`",
			i+1, strings.Title(b.BetType), scopeStr, b.Target, b.Amount, config.Bot.CurrencySymbol))
	}

	embed := &discordgo.MessageEmbed{
		Title: fmt.Sprintf("🎫 Seus Bilhetes na Rodada #%d", round.RoundNumber),
		Description: fmt.Sprintf(
			"**Sorteio em:** <t:%d:R> (<t:%d:t>)\n\n"+
				"**Apostas Registradas (%d):**\n%s\n\n"+
				"💰 **Total Investido na Rodada:** `%d %s`",
			round.DrawTime.Unix(), round.DrawTime.Unix(),
			len(bets), strings.Join(lines, "\n"), totalInvested, config.Bot.CurrencySymbol,
		),
		Color: 0x3498db,
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("Boa sorte, %s! Deu no Poste às %s", m.Author.Username, round.DrawTime.Format("15:04")),
		},
	}

	s.ChannelMessageSendEmbed(m.ChannelID, embed)
}

// handleBichoManualDraw executes an immediate draw (admin only)
func handleBichoManualDraw(s *discordgo.Session, m *discordgo.MessageCreate) {
	if !isUserAdmin(s, m.GuildID, m.Author.ID) {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Apenas administradores podem disparar o sorteio manual."))
		return
	}

	if err := games.GetBichoManager().TriggerManualDraw(m.GuildID); err != nil {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed(fmt.Sprintf("Erro ao acionar sorteio: %s", err.Error())))
		return
	}

	s.ChannelMessageSendEmbed(m.ChannelID, utils.SuccessEmbed(
		"🎲 Sorteio Iniciado",
		"O sorteio do Jogo do Bicho foi disparado! Os 5 prêmios e a apuração dos ganhadores serão publicados no canal oficial em instantes.",
	))
}

// handleBichoConfig manages guild bicho settings
func handleBichoConfig(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	if !isUserAdmin(s, m.GuildID, m.Author.ID) {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Apenas administradores podem configurar o Jogo do Bicho."))
		return
	}

	settings, err := database.GetBichoSettings(m.GuildID)
	if err != nil || settings == nil {
		settings = &database.DBBichoSettings{
			GuildID:    m.GuildID,
			DrawHour:   20,
			DrawMinute: 0,
			Enabled:    true,
			MinBet:     10,
		}
	}

	if len(args) == 0 {
		channelText := "Não configurado"
		if settings.ChannelID != "" {
			channelText = fmt.Sprintf("<#%s>", settings.ChannelID)
		}
		statusText := "🟢 Ativado"
		if !settings.Enabled {
			statusText = "🔴 Desativado"
		}

		info := fmt.Sprintf(
			"**Configurações Atuais do Jogo do Bicho:**\n\n"+
				"• **Canal Oficial:** %s\n"+
				"• **Horário Diário do Sorteio:** `%02d:%02d`\n"+
				"• **Aposta Mínima:** `%d %s`\n"+
				"• **Status:** %s\n\n"+
				"**Como alterar:**\n"+
				"`!bicho config canal #canal` - Define canal oficial\n"+
				"`!bicho config hora <0-23>` - Define hora do sorteio\n"+
				"`!bicho config minuto <0-59>` - Define minuto do sorteio\n"+
				"`!bicho config min <valor>` - Define valor mínimo de aposta\n"+
				"`!bicho config status <ligar|desligar>` - Ativa/desativa o jogo",
			channelText, settings.DrawHour, settings.DrawMinute, settings.MinBet, config.Bot.CurrencySymbol, statusText,
		)
		s.ChannelMessageSendEmbed(m.ChannelID, utils.InfoEmbed("Configuração do Jogo do Bicho", info))
		return
	}

	param := strings.ToLower(args[0])
	if len(args) < 2 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Especifique o novo valor. Exemplo: `!bicho config canal #jogo-do-bicho`"))
		return
	}
	val := args[1]

	switch param {
	case "canal", "channel":
		cleanID := strings.Trim(val, "<#>")
		settings.ChannelID = cleanID
		settings.Enabled = true
		if err := database.SaveBichoSettings(settings); err != nil {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Erro ao salvar configuração de canal."))
			return
		}
		games.GetBichoManager().ScheduleGuildRound(settings)
		s.ChannelMessageSendEmbed(m.ChannelID, utils.SuccessEmbed("Canal Configurado", fmt.Sprintf("Canal exclusivo do Jogo do Bicho definido para <#%s>. O painel da rodada já foi inicializado lá!", cleanID)))

	case "hora", "hour":
		h, err := strconv.Atoi(val)
		if err != nil || h < 0 || h > 23 {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Hora inválida! Digite um número de 0 a 23."))
			return
		}
		settings.DrawHour = h
		_ = database.SaveBichoSettings(settings)
		games.GetBichoManager().ScheduleGuildRound(settings)
		s.ChannelMessageSendEmbed(m.ChannelID, utils.SuccessEmbed("Horário Atualizado", fmt.Sprintf("Horário do sorteio diário definido para `%02d:%02d`.", settings.DrawHour, settings.DrawMinute)))

	case "minuto", "minute":
		min, err := strconv.Atoi(val)
		if err != nil || min < 0 || min > 59 {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Minuto inválido! Digite um número de 0 a 59."))
			return
		}
		settings.DrawMinute = min
		_ = database.SaveBichoSettings(settings)
		games.GetBichoManager().ScheduleGuildRound(settings)
		s.ChannelMessageSendEmbed(m.ChannelID, utils.SuccessEmbed("Horário Atualizado", fmt.Sprintf("Horário do sorteio diário definido para `%02d:%02d`.", settings.DrawHour, settings.DrawMinute)))

	case "min", "minimo", "min_bet":
		minVal, err := strconv.ParseInt(val, 10, 64)
		if err != nil || minVal <= 0 {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Valor mínimo inválido!"))
			return
		}
		settings.MinBet = minVal
		_ = database.SaveBichoSettings(settings)
		s.ChannelMessageSendEmbed(m.ChannelID, utils.SuccessEmbed("Aposta Mínima Atualizada", fmt.Sprintf("Aposta mínima definida para `%d %s`.", minVal, config.Bot.CurrencySymbol)))

	case "status", "enable", "disable":
		if val == "ligar" || val == "ativar" || val == "on" || val == "true" {
			settings.Enabled = true
		} else {
			settings.Enabled = false
		}
		_ = database.SaveBichoSettings(settings)
		if settings.Enabled {
			games.GetBichoManager().ScheduleGuildRound(settings)
			s.ChannelMessageSendEmbed(m.ChannelID, utils.SuccessEmbed("Jogo do Bicho Ativado", "O agendador diário do Jogo do Bicho está ativo."))
		} else {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.SuccessEmbed("Jogo do Bicho Desativado", "O Jogo do Bicho foi pausado."))
		}

	default:
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Parâmetro desconhecido. Opções: `canal`, `hora`, `minuto`, `min`, `status`."))
	}
}

// sendBichoHelp displays the game tutorial and commands
func sendBichoHelp(s *discordgo.Session, channelID string) {
	embed := &discordgo.MessageEmbed{
		Title: "🎲 Jogo do Bicho - Guia Completo e Regras",
		Description: "O tradicional jogo dos 25 bichos em uma versão 100% auditável e diária!\n" +
			"Todos os dias às 20:00 (ou horário definido pelos admins), ocorre o sorteio oficial **Deu no Poste** com 5 prêmios (1º ao 5º).",
		Color: 0x2ecc71,
		Fields: []*discordgo.MessageEmbedField{
			{
				Name: "🎯 Modalidades e Multiplicadores",
				Value: "• **Grupo:** `18x` na cabeça | `3.6x` cercado (1º ao 5º)\n" +
					"• **Dezena:** `60x` na cabeça | `12x` cercado\n" +
					"• **Centena:** `600x` na cabeça | `120x` cercado\n" +
					"• **Milhar:** `4.000x` na cabeça | `800x` cercado\n" +
					"• **Duque de Grupo:** `18.5x` (2 bichos entre os 5 prêmios)\n" +
					"• **Terno de Grupo:** `130x` (3 bichos entre os 5 prêmios)",
				Inline: false,
			},
			{
				Name: "⌨️ Comandos Principais",
				Value: "• `!bicho` - Ver painel da rodada e botões interativos\n" +
					"• `!bicho apostar <modalidade> <alvo> <valor> [posicao]`\n" +
					"• `!bicho tabela` - Ver todos os 25 bichos e suas 100 dezenas\n" +
					"• `!bicho apostas` - Ver seus bilhetes ativos na rodada",
				Inline: false,
			},
			{
				Name: "⚙️ Comandos de Administrador",
				Value: "• `!bicho config canal #canal` - Definir canal oficial\n" +
					"• `!bicho config hora <0-23>` - Mudar horário do sorteio\n" +
					"• `!bicho sortear` - Disparar sorteio manual imediatamente",
				Inline: false,
			},
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: "Pousadinha-Chan • Boa sorte a todos os apostadores!",
		},
	}
	s.ChannelMessageSendEmbed(channelID, embed)
}

// HandleBichoButton handles button clicks on the Bicho round embed
func HandleBichoButton(s *discordgo.Session, i *discordgo.InteractionCreate) {
	customID := i.MessageComponentData().CustomID
	userID := i.Member.User.ID

	if strings.HasPrefix(customID, "bicho_btn_bet_") {
		roundIDStr := strings.TrimPrefix(customID, "bicho_btn_bet_")
		roundID, _ := strconv.ParseInt(roundIDStr, 10, 64)

		modalResp := createBichoBetModal(roundID)
		_ = s.InteractionRespond(i.Interaction, modalResp)
		return
	}

	if strings.HasPrefix(customID, "bicho_btn_my_bets_") {
		roundIDStr := strings.TrimPrefix(customID, "bicho_btn_my_bets_")
		roundID, _ := strconv.ParseInt(roundIDStr, 10, 64)

		bets, _ := database.GetUserRoundBetsDB(roundID, userID)
		if len(bets) == 0 {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: "🎫 Você ainda não possui bilhetes nesta rodada. Use o botão **Apostar** para jogar!",
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})
			return
		}

		var totalInvested int64
		var lines []string
		for idx, b := range bets {
			totalInvested += b.Amount
			posStr := "Cabeça"
			if b.Scope == "cercado" {
				posStr = "Cercado"
			}
			lines = append(lines, fmt.Sprintf("`%d.` **%s** (%s) no alvo **%s** | `%d %s`",
				idx+1, strings.Title(b.BetType), posStr, b.Target, b.Amount, config.Bot.CurrencySymbol))
		}

		content := fmt.Sprintf("🎫 **Seus Bilhetes na Rodada Atual:**\n%s\n\n💰 **Total Investido:** `%d %s`",
			strings.Join(lines, "\n"), totalInvested, config.Bot.CurrencySymbol)

		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: content,
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	if customID == "bicho_btn_table" {
		tableEmbed := games.CreateBichoTableEmbed()
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Embeds: []*discordgo.MessageEmbed{tableEmbed},
				Flags:  discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	if customID == "bicho_btn_help" {
		helpEmbed := &discordgo.MessageEmbed{
			Title: "📜 Regras do Jogo do Bicho",
			Description: "• **25 Bichos e 100 Dezenas:** Cada bicho comanda 4 dezenas consecutivas.\n" +
				"• **Sorteio:** Todo dia às 20:00 são sorteados 5 milhares (1º ao 5º prêmio).\n" +
				"• **Cabeça:** Paga prêmio máximo se seu palpite acertar o **1º prêmio**.\n" +
				"• **Cercado:** Divide o prêmio entre os 5 prêmios (1º ao 5º).\n\n" +
				"**Exemplos:**\n" +
				"• Se sair o milhar `4528` no 1º prêmio, a dezena é `28`, que pertence ao **Carneiro (Grupo 07)**.\n" +
				"• Quem jogou no Carneiro na Cabeça ganha **18x**!\n" +
				"• Quem jogou na dezena 28 na Cabeça ganha **60x**!",
			Color: 0x9b59b6,
		}
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Embeds: []*discordgo.MessageEmbed{helpEmbed},
				Flags:  discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}
}

// createBichoBetModal builds the pop-up modal for betting
func createBichoBetModal(roundID int64) *discordgo.InteractionResponse {
	return &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{
			CustomID: fmt.Sprintf("bicho_modal_bet_%d", roundID),
			Title:    "Jogo do Bicho - Fazer Aposta",
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{
							CustomID:    "modality",
							Label:       "Modalidade (grupo, dezena, centena, milhar)",
							Style:       discordgo.TextInputShort,
							Placeholder: "grupo, dezena, centena, milhar, duque, terno",
							Required:    true,
							MinLength:   1,
							MaxLength:   15,
						},
					},
				},
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{
							CustomID:    "target",
							Label:       "Alvo (Bicho ou Número)",
							Style:       discordgo.TextInputShort,
							Placeholder: "Ex: Macaco, 28, 528, 4528, ou Macaco Leao",
							Required:    true,
							MinLength:   1,
							MaxLength:   40,
						},
					},
				},
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{
							CustomID:    "amount",
							Label:       "Valor da Aposta (EC)",
							Style:       discordgo.TextInputShort,
							Placeholder: "Ex: 100",
							Required:    true,
							MinLength:   1,
							MaxLength:   10,
						},
					},
				},
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{
							CustomID:    "scope",
							Label:       "Posição (cabeca ou cercado)",
							Style:       discordgo.TextInputShort,
							Value:       "cabeca",
							Placeholder: "cabeca (1º prêmio) ou cercado (1º ao 5º)",
							Required:    false,
							MinLength:   0,
							MaxLength:   15,
						},
					},
				},
			},
		},
	}
}

// HandleBichoModalSubmit processes the modal form submission
func HandleBichoModalSubmit(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ModalSubmitData()
	customID := data.CustomID

	// bicho_modal_bet_<roundID>
	if !strings.HasPrefix(customID, "bicho_modal_bet_") {
		return
	}

	var modality, target, amountStr, scope string
	for _, row := range data.Components {
		if actionRow, ok := row.(*discordgo.ActionsRow); ok {
			for _, comp := range actionRow.Components {
				if input, ok := comp.(*discordgo.TextInput); ok {
					switch input.CustomID {
					case "modality":
						modality = strings.TrimSpace(input.Value)
					case "target":
						target = strings.TrimSpace(input.Value)
					case "amount":
						amountStr = strings.TrimSpace(input.Value)
					case "scope":
						scope = strings.TrimSpace(input.Value)
					}
				}
			}
		}
	}

	amount, err := strconv.ParseInt(amountStr, 10, 64)
	if err != nil || amount <= 0 {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "❌ Valor de aposta inválido! Digite um número inteiro maior que 0.",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	res, err := games.GetBichoManager().PlaceBet(i.GuildID, i.Member.User.ID, modality, target, scope, amount)
	if err != nil {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: fmt.Sprintf("❌ Erro ao registrar bilhete: %s", err.Error()),
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	ticketEmbed := createBetTicketEmbed(i.Member.User, res)
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds: []*discordgo.MessageEmbed{ticketEmbed},
			Flags:  discordgo.MessageFlagsEphemeral,
		},
	})
}

// createBetTicketEmbed builds a rich confirmation ticket
func createBetTicketEmbed(user *discordgo.User, res *games.PlaceBetResult) *discordgo.MessageEmbed {
	posStr := "Cabeça (1º Prêmio)"
	if res.Scope == "cercado" {
		posStr = "Cercado (1º ao 5º Prêmio)"
	}

	return &discordgo.MessageEmbed{
		Title: fmt.Sprintf("🎫 Bilhete Registrado! - Rodada #%d", res.RoundNumber),
		Description: fmt.Sprintf(
			"Sua aposta foi computada com sucesso no sistema oficial!\n\n"+
				"📌 **Palpite:** %s\n"+
				"🎯 **Posição:** `%s`\n"+
				"💰 **Valor Apostado:** `%d %s`\n"+
				"📈 **Retorno Potencial:** `%s`\n\n"+
				"⏰ **Sorteio:** <t:%d:R> (<t:%d:t>)",
			res.Description, posStr, res.Amount, config.Bot.CurrencySymbol,
			res.Multiplier, res.DrawTime.Unix(), res.DrawTime.Unix(),
		),
		Color: 0x2ecc71,
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("Apostador: %s • Boa sorte!", user.Username),
		},
	}
}

// HandleSlashBicho handles slash commands for /bicho
func HandleSlashBicho(s *discordgo.Session, i *discordgo.InteractionCreate) {
	options := i.ApplicationCommandData().Options
	if len(options) == 0 {
		return
	}

	subCmd := options[0].Name
	switch subCmd {
	case "painel":
		round, err := database.GetActiveBichoRound(i.GuildID)
		if err != nil || round == nil {
			respondBichoEphemeral(s, i, "Não há nenhuma rodada aberta no momento.")
			return
		}
		totalBets, totalAmount, uniqueBettors, _ := database.GetBichoRoundStats(round.ID)
		embed := games.CreateBichoRoundEmbed(round, totalBets, totalAmount, uniqueBettors)
		components := games.CreateBichoRoundComponents(round.ID)
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Embeds:     []*discordgo.MessageEmbed{embed},
				Components: components,
			},
		})

	case "tabela":
		tableEmbed := games.CreateBichoTableEmbed()
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Embeds: []*discordgo.MessageEmbed{tableEmbed},
				Flags:  discordgo.MessageFlagsEphemeral,
			},
		})

	case "minhas-apostas":
		round, err := database.GetActiveBichoRound(i.GuildID)
		if err != nil || round == nil {
			respondBichoEphemeral(s, i, "Não há rodada aberta no momento.")
			return
		}
		bets, _ := database.GetUserRoundBetsDB(round.ID, i.Member.User.ID)
		if len(bets) == 0 {
			respondBichoEphemeral(s, i, "Você ainda não possui apostas nesta rodada.")
			return
		}
		var totalInvested int64
		var lines []string
		for idx, b := range bets {
			totalInvested += b.Amount
			pos := "Cabeça"
			if b.Scope == "cercado" {
				pos = "Cercado"
			}
			lines = append(lines, fmt.Sprintf("`%d.` **%s** (%s) no alvo **%s** | `%d %s`",
				idx+1, strings.Title(b.BetType), pos, b.Target, b.Amount, config.Bot.CurrencySymbol))
		}
		content := fmt.Sprintf("🎫 **Seus Bilhetes na Rodada #%d:**\n%s\n\n💰 **Total Investido:** `%d %s`",
			round.RoundNumber, strings.Join(lines, "\n"), totalInvested, config.Bot.CurrencySymbol)
		respondBichoEphemeral(s, i, content)

	case "apostar":
		subOptions := options[0].Options
		var modality, target, scope string
		var amount int64

		for _, opt := range subOptions {
			switch opt.Name {
			case "modalidade":
				modality = opt.StringValue()
			case "alvo":
				target = opt.StringValue()
			case "valor":
				amount = opt.IntValue()
			case "posicao":
				scope = opt.StringValue()
			}
		}

		res, err := games.GetBichoManager().PlaceBet(i.GuildID, i.Member.User.ID, modality, target, scope, amount)
		if err != nil {
			respondBichoEphemeral(s, i, fmt.Sprintf("❌ Erro ao registrar aposta: %s", err.Error()))
			return
		}

		ticketEmbed := createBetTicketEmbed(i.Member.User, res)
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Embeds: []*discordgo.MessageEmbed{ticketEmbed},
				Flags:  discordgo.MessageFlagsEphemeral,
			},
		})

	case "sortear":
		if !isUserAdmin(s, i.GuildID, i.Member.User.ID) {
			respondBichoEphemeral(s, i, "❌ Apenas administradores podem disparar o sorteio.")
			return
		}
		if err := games.GetBichoManager().TriggerManualDraw(i.GuildID); err != nil {
			respondBichoEphemeral(s, i, fmt.Sprintf("❌ Erro ao sortear: %s", err.Error()))
			return
		}
		respondBichoEphemeral(s, i, "🎲 Sorteio disparado com sucesso! Os resultados serão publicados no canal oficial.")

	case "config":
		if !isUserAdmin(s, i.GuildID, i.Member.User.ID) {
			respondBichoEphemeral(s, i, "❌ Apenas administradores podem configurar o Jogo do Bicho.")
			return
		}
		settings, _ := database.GetBichoSettings(i.GuildID)
		if settings == nil {
			settings = &database.DBBichoSettings{
				GuildID:    i.GuildID,
				DrawHour:   20,
				DrawMinute: 0,
				Enabled:    true,
				MinBet:     10,
			}
		}

		for _, opt := range options[0].Options {
			switch opt.Name {
			case "canal":
				settings.ChannelID = opt.ChannelValue(s).ID
				settings.Enabled = true
			case "hora":
				settings.DrawHour = int(opt.IntValue())
			case "minuto":
				settings.DrawMinute = int(opt.IntValue())
			case "min_bet":
				settings.MinBet = opt.IntValue()
			case "ativado":
				settings.Enabled = opt.BoolValue()
			}
		}

		_ = database.SaveBichoSettings(settings)
		if settings.Enabled && settings.ChannelID != "" {
			games.GetBichoManager().ScheduleGuildRound(settings)
		}
		respondBichoEphemeral(s, i, fmt.Sprintf("✅ Configurações salvas! Canal: <#%s>, Horário: `%02d:%02d`, Aposta Mínima: `%d %s`.",
			settings.ChannelID, settings.DrawHour, settings.DrawMinute, settings.MinBet, config.Bot.CurrencySymbol))
	}
}

func respondBichoEphemeral(s *discordgo.Session, i *discordgo.InteractionCreate, content string) {
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: content,
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
}

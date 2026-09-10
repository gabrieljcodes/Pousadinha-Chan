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
	case "panel", "painel", "status", "info", "round", "rodada":
		showBichoPanel(s, m)

	case "bet", "apostar", "aposta":
		handleBichoTextBet(s, m, args[1:])

	case "table", "tabela", "animals", "animais", "groups", "grupos", "bichos":
		s.ChannelMessageSendEmbed(m.ChannelID, games.CreateBichoTableEmbed())

	case "bets", "my-bets", "mybets", "apostas", "minhas-apostas", "minhasapostas", "tickets", "bilhetes":
		showUserBichoBets(s, m)

	case "draw", "sortear":
		handleBichoManualDraw(s, m)

	case "config", "configure", "configurar":
		handleBichoConfig(s, m, args[1:])

	case "help", "ajuda", "rules", "regras":
		sendBichoHelp(s, m.ChannelID)

	default:
		sendBichoHelp(s, m.ChannelID)
	}
}

// showBichoPanel displays the active round embed with buttons or directs the user to configure it
func showBichoPanel(s *discordgo.Session, m *discordgo.MessageCreate) {
	guildID := m.GuildID
	if guildID == "" {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("This command can only be used in a server."))
		return
	}

	settings, err := database.GetBichoSettings(guildID)
	if err != nil || settings == nil || settings.ChannelID == "" {
		if isUserAdmin(s, guildID, m.Author.ID) {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.InfoEmbed("Configuration Required",
				"🎲 **The official Jogo do Bicho channel has not been configured yet!**\n\n"+
					"Set the dedicated channel using:\n"+
					"`!bicho config channel #channel` or `/bicho config channel:#channel`"))
		} else {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Jogo do Bicho has not been configured on this server by administrators yet."))
		}
		return
	}

	round, err := database.GetActiveBichoRound(guildID)
	if err != nil || round == nil {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.InfoEmbed("Jogo do Bicho", "No active round open at this time. Please wait for the next draw."))
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

// handleBichoTextBet processes bets made through text command: !bicho bet <modality> <target> <amount> [position]
func handleBichoTextBet(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	if len(args) < 3 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.InfoEmbed(
			"How to Bet on Jogo do Bicho",
			"**Syntax:**\n"+
				"`!bicho bet <modality> <target> <amount> [position]`\n\n"+
				"**Modalities:**\n"+
				"• `group` (E.g. `!bicho bet monkey 100` or `!bicho bet group 17 100 board`)\n"+
				"• `tens` (E.g. `!bicho bet tens 28 50`)\n"+
				"• `hundreds` (E.g. `!bicho bet hundreds 528 20 board`)\n"+
				"• `thousands` (E.g. `!bicho bet thousands 4528 10`)\n"+
				"• `pair` (E.g. `!bicho bet pair \"monkey, lion\" 50`)\n"+
				"• `trio` (E.g. `!bicho bet trio \"monkey, lion, tiger\" 20`)\n\n"+
				"**Positions:**\n"+
				"• `head` (1st prize - default)\n"+
				"• `board` (1st to 5th prizes)",
		))
		return
	}

	modality := args[0]
	var target, scope string
	var amount int64

	// Determine if last argument is scope (head/board/cabeca/cercado)
	lastArg := strings.ToLower(args[len(args)-1])
	isScopeLast := lastArg == "head" || lastArg == "cabeca" || lastArg == "cabeça" ||
		lastArg == "board" || lastArg == "cercado" || lastArg == "cer" || lastArg == "cab"

	if isScopeLast {
		scope = lastArg
		if len(args) < 4 {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Incomplete bet format. Use `!bicho bet <modality> <target> <amount> <position>`"))
			return
		}
		val, err := strconv.ParseInt(args[len(args)-2], 10, 64)
		if err != nil || val <= 0 {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Invalid bet amount!"))
			return
		}
		amount = val
		target = strings.Join(args[1:len(args)-2], " ")
	} else {
		scope = "head"
		val, err := strconv.ParseInt(args[len(args)-1], 10, 64)
		if err != nil || val <= 0 {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Invalid bet amount! The amount must be the last parameter."))
			return
		}
		amount = val
		target = strings.Join(args[1:len(args)-1], " ")
	}

	res, err := games.GetBichoManager().PlaceBet(m.GuildID, m.Author.ID, modality, target, scope, amount)
	if err != nil {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed(fmt.Sprintf("❌ Could not register bet: %s", err.Error())))
		return
	}

	ticketEmbed := createBetTicketEmbed(m.Author, res)
	s.ChannelMessageSendEmbed(m.ChannelID, ticketEmbed)
}

// showUserBichoBets lists all bets placed by the user in the active round
func showUserBichoBets(s *discordgo.Session, m *discordgo.MessageCreate) {
	round, err := database.GetActiveBichoRound(m.GuildID)
	if err != nil || round == nil {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.InfoEmbed("Jogo do Bicho", "No active round open at this time."))
		return
	}

	bets, err := database.GetUserRoundBetsDB(round.ID, m.Author.ID)
	if err != nil {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Error fetching your tickets."))
		return
	}

	if len(bets) == 0 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.InfoEmbed(
			fmt.Sprintf("Your Tickets - Round #%d", round.RoundNumber),
			"You haven't placed any bets in this round yet.\nClick the **Place Bet** button in the official channel or use `!bicho bet`!",
		))
		return
	}

	var totalInvested int64
	var lines []string
	for i, b := range bets {
		totalInvested += b.Amount
		scopeStr := "Head"
		if b.Scope == "board" || b.Scope == "cercado" {
			scopeStr = "Board (1st to 5th)"
		}
		lines = append(lines, fmt.Sprintf("`%d.` **%s** (%s) on target **%s** | `%d %s`",
			i+1, strings.Title(b.BetType), scopeStr, b.Target, b.Amount, config.Bot.CurrencySymbol))
	}

	embed := &discordgo.MessageEmbed{
		Title: fmt.Sprintf("🎫 Your Tickets in Round #%d", round.RoundNumber),
		Description: fmt.Sprintf(
			"**Draw in:** <t:%d:R> (<t:%d:t>)\n\n"+
				"**Registered Bets (%d):**\n%s\n\n"+
				"💰 **Total Wagered in Round:** `%d %s`",
			round.DrawTime.Unix(), round.DrawTime.Unix(),
			len(bets), strings.Join(lines, "\n"), totalInvested, config.Bot.CurrencySymbol,
		),
		Color: 0x3498db,
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("Good luck, %s! Official draw at %s", m.Author.Username, round.DrawTime.Format("15:04")),
		},
	}

	s.ChannelMessageSendEmbed(m.ChannelID, embed)
}

// handleBichoManualDraw executes an immediate draw (admin only)
func handleBichoManualDraw(s *discordgo.Session, m *discordgo.MessageCreate) {
	if !isUserAdmin(s, m.GuildID, m.Author.ID) {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Only administrators can trigger a manual draw."))
		return
	}

	if err := games.GetBichoManager().TriggerManualDraw(m.GuildID); err != nil {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed(fmt.Sprintf("Error triggering draw: %s", err.Error())))
		return
	}

	s.ChannelMessageSendEmbed(m.ChannelID, utils.SuccessEmbed(
		"🎲 Draw Initiated",
		"The Jogo do Bicho draw has been triggered! The 5 prizes and winner evaluation will be published in the official channel shortly.",
	))
}

// handleBichoConfig manages guild bicho settings
func handleBichoConfig(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	if !isUserAdmin(s, m.GuildID, m.Author.ID) {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Only administrators can configure Jogo do Bicho."))
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
		channelText := "Not configured"
		if settings.ChannelID != "" {
			channelText = fmt.Sprintf("<#%s>", settings.ChannelID)
		}
		statusText := "🟢 Enabled"
		if !settings.Enabled {
			statusText = "🔴 Disabled"
		}

		info := fmt.Sprintf(
			"**Current Jogo do Bicho Settings:**\n\n"+
				"• **Official Channel:** %s\n"+
				"• **Daily Draw Schedule:** `%02d:%02d`\n"+
				"• **Minimum Bet:** `%d %s`\n"+
				"• **Status:** %s\n\n"+
				"**How to change:**\n"+
				"`!bicho config channel #channel` - Set dedicated channel\n"+
				"`!bicho config hour <0-23>` - Set draw hour\n"+
				"`!bicho config minute <0-59>` - Set draw minute\n"+
				"`!bicho config min <amount>` - Set minimum bet amount\n"+
				"`!bicho config status <on|off>` - Enable or disable the game",
			channelText, settings.DrawHour, settings.DrawMinute, settings.MinBet, config.Bot.CurrencySymbol, statusText,
		)
		s.ChannelMessageSendEmbed(m.ChannelID, utils.InfoEmbed("Jogo do Bicho Configuration", info))
		return
	}

	param := strings.ToLower(args[0])
	if len(args) < 2 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Specify the new value. Example: `!bicho config channel #lottery`"))
		return
	}
	val := args[1]

	switch param {
	case "channel", "canal":
		cleanID := strings.Trim(val, "<#>")
		settings.ChannelID = cleanID
		settings.Enabled = true
		if err := database.SaveBichoSettings(settings); err != nil {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Error saving channel configuration."))
			return
		}
		games.GetBichoManager().ScheduleGuildRound(settings)
		s.ChannelMessageSendEmbed(m.ChannelID, utils.SuccessEmbed("Channel Configured", fmt.Sprintf("Dedicated Jogo do Bicho channel set to <#%s>. The round panel has been initialized there!", cleanID)))

	case "hour", "hora":
		h, err := strconv.Atoi(val)
		if err != nil || h < 0 || h > 23 {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Invalid hour! Enter a number between 0 and 23."))
			return
		}
		settings.DrawHour = h
		_ = database.SaveBichoSettings(settings)
		games.GetBichoManager().ScheduleGuildRound(settings)
		s.ChannelMessageSendEmbed(m.ChannelID, utils.SuccessEmbed("Schedule Updated", fmt.Sprintf("Daily draw schedule set to `%02d:%02d`.", settings.DrawHour, settings.DrawMinute)))

	case "minute", "minuto":
		min, err := strconv.Atoi(val)
		if err != nil || min < 0 || min > 59 {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Invalid minute! Enter a number between 0 and 59."))
			return
		}
		settings.DrawMinute = min
		_ = database.SaveBichoSettings(settings)
		games.GetBichoManager().ScheduleGuildRound(settings)
		s.ChannelMessageSendEmbed(m.ChannelID, utils.SuccessEmbed("Schedule Updated", fmt.Sprintf("Daily draw schedule set to `%02d:%02d`.", settings.DrawHour, settings.DrawMinute)))

	case "min", "minimo", "minimum", "min_bet":
		minVal, err := strconv.ParseInt(val, 10, 64)
		if err != nil || minVal <= 0 {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Invalid minimum bet amount!"))
			return
		}
		settings.MinBet = minVal
		_ = database.SaveBichoSettings(settings)
		s.ChannelMessageSendEmbed(m.ChannelID, utils.SuccessEmbed("Minimum Bet Updated", fmt.Sprintf("Minimum bet amount set to `%d %s`.", minVal, config.Bot.CurrencySymbol)))

	case "status", "enable", "disable", "ligar", "desligar":
		if val == "on" || val == "enable" || val == "enabled" || val == "true" || val == "ligar" || val == "ativar" {
			settings.Enabled = true
		} else {
			settings.Enabled = false
		}
		_ = database.SaveBichoSettings(settings)
		if settings.Enabled {
			games.GetBichoManager().ScheduleGuildRound(settings)
			s.ChannelMessageSendEmbed(m.ChannelID, utils.SuccessEmbed("Jogo do Bicho Enabled", "The daily lottery scheduler is now active."))
		} else {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.SuccessEmbed("Jogo do Bicho Disabled", "Jogo do Bicho has been paused."))
		}

	default:
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Unknown parameter. Options: `channel`, `hour`, `minute`, `min`, `status`."))
	}
}

// sendBichoHelp displays the game tutorial and commands
func sendBichoHelp(s *discordgo.Session, channelID string) {
	embed := &discordgo.MessageEmbed{
		Title: "🎲 Jogo do Bicho - Complete Guide & Rules",
		Description: "The traditional 25-animal Brazilian lottery in an audited, daily edition!\n" +
			"Every day at 20:00 (or the time configured by admins), the official draw takes place with 5 prizes (1st to 5th).",
		Color: 0x2ecc71,
		Fields: []*discordgo.MessageEmbedField{
			{
				Name: "🎯 Modalities and Multipliers",
				Value: "• **Group:** `18x` on Head | `3.6x` on Board (1st to 5th)\n" +
					"• **Tens:** `60x` on Head | `12x` on Board\n" +
					"• **Hundreds:** `600x` on Head | `120x` on Board\n" +
					"• **Thousands:** `4,000x` on Head | `800x` on Board\n" +
					"• **Animal Pair (Duque):** `18.5x` (both animals in the 5 prizes)\n" +
					"• **Animal Trio (Terno):** `130x` (all 3 animals in the 5 prizes)",
				Inline: false,
			},
			{
				Name: "⌨️ Main Commands",
				Value: "• `!bicho` - View active round panel and interactive buttons\n" +
					"• `!bicho bet <modality> <target> <amount> [position]`\n" +
					"• `!bicho table` - View all 25 animals and their tens\n" +
					"• `!bicho bets` - View your active tickets in the current round",
				Inline: false,
			},
			{
				Name: "⚙️ Administrator Commands",
				Value: "• `!bicho config channel #channel` - Set official channel\n" +
					"• `!bicho config hour <0-23>` - Change daily draw time\n" +
					"• `!bicho draw` - Trigger immediate draw",
				Inline: false,
			},
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: "Pousadinha-Chan • Good luck to all players!",
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
					Content: "🎫 You don't have any tickets in this round yet. Use the **Place Bet** button to play!",
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})
			return
		}

		var totalInvested int64
		var lines []string
		for idx, b := range bets {
			totalInvested += b.Amount
			posStr := "Head"
			if b.Scope == "board" || b.Scope == "cercado" {
				posStr = "Board"
			}
			lines = append(lines, fmt.Sprintf("`%d.` **%s** (%s) on target **%s** | `%d %s`",
				idx+1, strings.Title(b.BetType), posStr, b.Target, b.Amount, config.Bot.CurrencySymbol))
		}

		content := fmt.Sprintf("🎫 **Your Tickets in the Current Round:**\n%s\n\n💰 **Total Wagered:** `%d %s`",
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
			Title: "📜 Jogo do Bicho Rules",
			Description: "• **25 Animals and 100 Tens:** Each animal rules 4 consecutive tens.\n" +
				"• **Daily Draw:** Every day 5 four-digit numbers (1st to 5th prizes) are drawn.\n" +
				"• **Head (Cabeça):** Top payout if your pick hits the **1st prize**.\n" +
				"• **Board (Cercado):** Splits payout across all 5 prizes (1st to 5th).\n\n" +
				"**Examples:**\n" +
				"• If `4528` is drawn as 1st prize, the tens are `28`, which belongs to **Ram (Group 07)**.\n" +
				"• Betting on Ram on Head pays **18x**!\n" +
				"• Betting on tens 28 on Head pays **60x**!",
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
			Title:    "Jogo do Bicho - Place Bet",
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{
							CustomID:    "modality",
							Label:       "Modality (group, tens, hundreds, thousands)",
							Style:       discordgo.TextInputShort,
							Placeholder: "group, tens, hundreds, thousands, pair, trio",
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
							Label:       "Target (Animal or Number)",
							Style:       discordgo.TextInputShort,
							Placeholder: "E.g. Monkey, 28, 528, 4528, or Monkey Lion",
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
							Label:       "Bet Amount (EC)",
							Style:       discordgo.TextInputShort,
							Placeholder: "E.g. 100",
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
							Label:       "Position (head or board)",
							Style:       discordgo.TextInputShort,
							Value:       "head",
							Placeholder: "head (1st prize) or board (1st to 5th)",
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
				Content: "❌ Invalid bet amount! Enter an integer greater than 0.",
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
				Content: fmt.Sprintf("❌ Error registering ticket: %s", err.Error()),
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
	posStr := "Head (1st Prize)"
	if res.Scope == "board" || res.Scope == "cercado" {
		posStr = "Board (1st to 5th Prizes)"
	}

	return &discordgo.MessageEmbed{
		Title: fmt.Sprintf("🎫 Ticket Registered! - Round #%d", res.RoundNumber),
		Description: fmt.Sprintf(
			"Your bet was successfully recorded in the official lottery system!\n\n"+
				"📌 **Pick:** %s\n"+
				"🎯 **Position:** `%s`\n"+
				"💰 **Amount Wagered:** `%d %s`\n"+
				"📈 **Potential Payout:** `%s`\n\n"+
				"⏰ **Draw:** <t:%d:R> (<t:%d:t>)",
			res.Description, posStr, res.Amount, config.Bot.CurrencySymbol,
			res.Multiplier, res.DrawTime.Unix(), res.DrawTime.Unix(),
		),
		Color: 0x2ecc71,
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("Bettor: %s • Good luck!", user.Username),
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
	case "panel", "painel":
		round, err := database.GetActiveBichoRound(i.GuildID)
		if err != nil || round == nil {
			respondBichoEphemeral(s, i, "No active round open at this time.")
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

	case "table", "tabela":
		tableEmbed := games.CreateBichoTableEmbed()
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Embeds: []*discordgo.MessageEmbed{tableEmbed},
				Flags:  discordgo.MessageFlagsEphemeral,
			},
		})

	case "my-bets", "minhas-apostas":
		round, err := database.GetActiveBichoRound(i.GuildID)
		if err != nil || round == nil {
			respondBichoEphemeral(s, i, "No active round open at this time.")
			return
		}
		bets, _ := database.GetUserRoundBetsDB(round.ID, i.Member.User.ID)
		if len(bets) == 0 {
			respondBichoEphemeral(s, i, "You don't have any tickets in this round yet.")
			return
		}
		var totalInvested int64
		var lines []string
		for idx, b := range bets {
			totalInvested += b.Amount
			pos := "Head"
			if b.Scope == "board" || b.Scope == "cercado" {
				pos = "Board"
			}
			lines = append(lines, fmt.Sprintf("`%d.` **%s** (%s) on target **%s** | `%d %s`",
				idx+1, strings.Title(b.BetType), pos, b.Target, b.Amount, config.Bot.CurrencySymbol))
		}
		content := fmt.Sprintf("🎫 **Your Tickets in Round #%d:**\n%s\n\n💰 **Total Wagered:** `%d %s`",
			round.RoundNumber, strings.Join(lines, "\n"), totalInvested, config.Bot.CurrencySymbol)
		respondBichoEphemeral(s, i, content)

	case "bet", "apostar":
		subOptions := options[0].Options
		var modality, target, scope string
		var amount int64

		for _, opt := range subOptions {
			switch opt.Name {
			case "modality", "modalidade":
				modality = opt.StringValue()
			case "target", "alvo":
				target = opt.StringValue()
			case "amount", "valor":
				amount = opt.IntValue()
			case "position", "posicao":
				scope = opt.StringValue()
			}
		}

		res, err := games.GetBichoManager().PlaceBet(i.GuildID, i.Member.User.ID, modality, target, scope, amount)
		if err != nil {
			respondBichoEphemeral(s, i, fmt.Sprintf("❌ Error registering bet: %s", err.Error()))
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

	case "draw", "sortear":
		if !isUserAdmin(s, i.GuildID, i.Member.User.ID) {
			respondBichoEphemeral(s, i, "❌ Only administrators can trigger the draw.")
			return
		}
		if err := games.GetBichoManager().TriggerManualDraw(i.GuildID); err != nil {
			respondBichoEphemeral(s, i, fmt.Sprintf("❌ Error triggering draw: %s", err.Error()))
			return
		}
		respondBichoEphemeral(s, i, "🎲 Draw triggered successfully! Results will be published in the official channel.")

	case "config":
		if !isUserAdmin(s, i.GuildID, i.Member.User.ID) {
			respondBichoEphemeral(s, i, "❌ Only administrators can configure Jogo do Bicho.")
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
			case "channel", "canal":
				settings.ChannelID = opt.ChannelValue(s).ID
				settings.Enabled = true
			case "hour", "hora":
				settings.DrawHour = int(opt.IntValue())
			case "minute", "minuto":
				settings.DrawMinute = int(opt.IntValue())
			case "min_bet":
				settings.MinBet = opt.IntValue()
			case "enabled", "ativado":
				settings.Enabled = opt.BoolValue()
			}
		}

		_ = database.SaveBichoSettings(settings)
		if settings.Enabled && settings.ChannelID != "" {
			games.GetBichoManager().ScheduleGuildRound(settings)
		}
		respondBichoEphemeral(s, i, fmt.Sprintf("✅ Settings saved! Channel: <#%s>, Schedule: `%02d:%02d`, Min Bet: `%d %s`.",
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

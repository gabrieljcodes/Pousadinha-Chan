package commands

import (
	"bot/internal/database"
	"bot/internal/games"
	"bot/internal/locale"
	"bot/pkg/config"

	"fmt"
	"strconv"
	"strings"

	"github.com/bwmarrin/discordgo"
)

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
					Content: locale.Text("commands.bicho_cmd.you_don_t_have_any_tickets_in"),
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})
			return
		}

		var totalInvested int64
		var lines []string
		for idx, b := range bets {
			totalInvested += b.Amount
			posStr := locale.Text("commands.bicho_cmd.head")
			if b.Scope == "board" || b.Scope == "cercado" {
				posStr = locale.Text("commands.bicho_cmd.board")
			}
			lines = append(lines, locale.Text("commands.bicho_cmd.on_target.formatted", locale.Data{"Idx": idx + 1, "Strings": strings.Title(b.BetType), "PosStr": posStr, "Target": b.Target, "Amount": b.Amount, "CurrencySymbol": config.Bot.CurrencySymbol}))
		}

		content := locale.Text("commands.bicho_cmd.your_tickets_in_the_current_round_total.formatted", locale.Data{"Strings": strings.Join(lines, "\n"), "TotalInvested": totalInvested, "CurrencySymbol": config.Bot.CurrencySymbol})

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
			Title:       locale.Text("commands.bicho_cmd.jogo_do_bicho_rules"),
			Description: locale.Text("commands.bicho_cmd.animals_and_tens_each_animal_rules_consecutive"),
			Color:       0x9b59b6,
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
			Title:    locale.Text("commands.bicho_cmd.jogo_do_bicho_place_bet"),
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{
							CustomID:    "modality",
							Label:       locale.Text("commands.bicho_cmd.modality_group_tens_hundreds_thousands"),
							Style:       discordgo.TextInputShort,
							Placeholder: locale.Text("commands.bicho_cmd.group_tens_hundreds_thousands_pair_trio"),
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
							Label:       locale.Text("commands.bicho_cmd.target_animal_or_number"),
							Style:       discordgo.TextInputShort,
							Placeholder: locale.Text("commands.bicho_cmd.e_g_monkey_or_monkey_lion"),
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
							Label:       locale.Text("commands.bicho_cmd.bet_amount_ec"),
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
							Label:       locale.Text("commands.bicho_cmd.position_head_or_board"),
							Style:       discordgo.TextInputShort,
							Value:       "head",
							Placeholder: locale.Text("commands.bicho_cmd.head_st_prize_or_board_st_to"),
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
				Content: locale.Text("commands.bicho_cmd.invalid_bet_amount_enter_an_integer_greater"),
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
				Content: locale.Text("commands.bicho_cmd.error_registering_ticket.formatted", locale.Data{"Err": err.Error()}),
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
	posStr := locale.Text("commands.bicho_cmd.head_st_prize")
	if res.Scope == "board" || res.Scope == "cercado" {
		posStr = locale.Text("commands.bicho_cmd.board_st_to_th_prizes")
	}

	return &discordgo.MessageEmbed{
		Title:       locale.Text("commands.bicho_cmd.ticket_registered_round.formatted", locale.Data{"RoundNumber": res.RoundNumber}),
		Description: locale.Text("commands.bicho_cmd.your_bet_was_successfully_recorded_in_the.formatted", locale.Data{"Description": res.Description, "PosStr": posStr, "Amount": res.Amount, "CurrencySymbol": config.Bot.CurrencySymbol, "Multiplier": res.Multiplier, "DrawTime": res.DrawTime.Unix(), "DrawTime7": res.DrawTime.Unix()}),
		Color:       0x2ecc71,
		Footer: &discordgo.MessageEmbedFooter{
			Text: locale.Text("commands.bicho_cmd.bettor_good_luck.formatted", locale.Data{"Username": user.Username}),
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
			respondBichoEphemeral(s, i, locale.Text("commands.bicho_cmd.no_active_round_open_at_this_time"))
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
			respondBichoEphemeral(s, i, locale.Text("commands.bicho_cmd.no_active_round_open_at_this_time"))
			return
		}
		bets, _ := database.GetUserRoundBetsDB(round.ID, i.Member.User.ID)
		if len(bets) == 0 {
			respondBichoEphemeral(s, i, locale.Text("commands.bicho_cmd.you_don_t_have_any_tickets_in_c41e41"))
			return
		}
		var totalInvested int64
		var lines []string
		for idx, b := range bets {
			totalInvested += b.Amount
			pos := locale.Text("commands.bicho_cmd.head")
			if b.Scope == "board" || b.Scope == "cercado" {
				pos = locale.Text("commands.bicho_cmd.board")
			}
			lines = append(lines, locale.Text("commands.bicho_cmd.on_target.formatted1", locale.Data{"Idx": idx + 1, "Strings": strings.Title(b.BetType), "Pos": pos, "Target": b.Target, "Amount": b.Amount, "CurrencySymbol": config.Bot.CurrencySymbol}))
		}
		content := locale.Text("commands.bicho_cmd.your_tickets_in_round_total_wagered.formatted", locale.Data{"RoundNumber": round.RoundNumber, "Strings": strings.Join(lines, "\n"), "TotalInvested": totalInvested, "CurrencySymbol": config.Bot.CurrencySymbol})
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
			respondBichoEphemeral(s, i, locale.Text("commands.bicho_cmd.error_registering_bet.formatted", locale.Data{"Err": err.Error()}))
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
			respondBichoEphemeral(s, i, locale.Text("commands.bicho_cmd.only_administrators_can_trigger_the_draw"))
			return
		}
		if err := games.GetBichoManager().TriggerManualDraw(i.GuildID); err != nil {
			respondBichoEphemeral(s, i, locale.Text("commands.bicho_cmd.error_triggering_draw.formatted", locale.Data{"Err": err.Error()}))
			return
		}
		respondBichoEphemeral(s, i, locale.Text("commands.bicho_cmd.draw_triggered_successfully_results_will_be_published"))

	case "config":
		if !isUserAdmin(s, i.GuildID, i.Member.User.ID) {
			respondBichoEphemeral(s, i, locale.Text("commands.bicho_cmd.only_administrators_can_configure_jogo_do_bicho"))
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
		respondBichoEphemeral(s, i, locale.Text("commands.bicho_cmd.settings_saved_channel_schedule_min_bet.formatted", locale.Data{"ChannelID": settings.ChannelID, "DrawHour": settings.DrawHour, "DrawMinute": settings.DrawMinute, "MinBet": settings.MinBet, "CurrencySymbol": config.Bot.CurrencySymbol}))
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

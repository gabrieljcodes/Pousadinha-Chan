package games

import (
	"bot/internal/database"
	"bot/internal/locale"
	"bot/pkg/config"
	"bot/pkg/utils"
	"crypto/rand"
	"encoding/binary"

	"log"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Active games map: UserID -> AviatorSession
var (
	activeAviatorGames = make(map[string]*AviatorSession)
	aviatorMutex       sync.Mutex
)

// Constants
const (
	MinBet          = 100
	MultiplierSpeed = 1500 * time.Millisecond // Safe update interval preventing Discord 429 rate limits
)

type AviatorSession struct {
	GuildID     string
	UserID      string
	Bet         int
	AutoCashout float64
	StartTime   time.Time
	CrashPoint  float64
	ControlChan chan bool
	DoneChan    chan struct{}
	CashedOut   bool
}

type MessageUpdater func(embed *discordgo.MessageEmbed, finished bool)

func StartAviatorInteraction(s *discordgo.Session, i *discordgo.InteractionCreate, bet int, autoCashout float64) {
	if i.GuildID == "" {
		respondPrivate(s, i, utils.ErrorEmbed(locale.Text("games.aviator.this_game_can_only_be_played_within")))
		return
	}
	guildID := i.GuildID
	userID := i.Member.User.ID

	if !validatePreGame(guildID, userID, bet) {
		respondPrivate(s, i, utils.ErrorEmbed(locale.Text("games.aviator.cannot_start_game_min_bet_check_your.formatted", locale.Data{"MinBet": MinBet, "CurrencySymbol": config.Bot.CurrencySymbol})))
		return
	}

	if autoCashout > 0 && autoCashout < 1.05 {
		respondPrivate(s, i, utils.ErrorEmbed(locale.Text("games.aviator.auto_cash_out_multiplier_must_be_at")))
		return
	}

	job := GameJob{
		UserID: userID,
		OnQueue: func(pos int) {
			if pos == -1 {
				respondPrivate(s, i, utils.ErrorEmbed(locale.Text("games.aviator.you_already_have_an_active_game_in")))
			}
		},
		Run: func(finishChan chan struct{}) {
			defer close(finishChan)

			// Validate balance
			if database.GetBalance(guildID, userID) < bet {
				respondPrivate(s, i, utils.ErrorEmbed(locale.Text("games.aviator.you_ran_out_of_coins_before_takeoff")))
				return
			}

			// Deduct bet atomically
			if err := database.CollectLostBet(guildID, userID, bet); err != nil {
				respondPrivate(s, i, utils.ErrorEmbed(locale.Text("games.aviator.failed_to_deduct_bet")))
				return
			}

			session := setupGame(guildID, userID, bet, autoCashout)
			embed, btn := getInitialState(bet, autoCashout, userID)

			// Send game board message
			msg, err := s.ChannelMessageSendComplex(i.ChannelID, &discordgo.MessageSend{
				Content: locale.Text("games.aviator.your_aviator_flight_is_taking_off.formatted", locale.Data{"UserID": userID}),
				Embeds:  []*discordgo.MessageEmbed{embed},
				Components: []discordgo.MessageComponent{
					discordgo.ActionsRow{Components: []discordgo.MessageComponent{btn}},
				},
			})

			if err != nil {
				// Refund immediately if message delivery failed
				_ = database.AddCoins(guildID, userID, bet)
				cleanup(userID)
				return
			}

			// Acknowledge the slash interaction so it doesn't time out
			_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: locale.Text("games.aviator.flight_started_in.formatted", locale.Data{"ChannelID": i.ChannelID}),
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})

			updater := func(embed *discordgo.MessageEmbed, finished bool) {
				comps := []discordgo.MessageComponent{}
				if !finished {
					comps = []discordgo.MessageComponent{
						discordgo.ActionsRow{Components: []discordgo.MessageComponent{btn}},
					}
				} else {
					btn.Label = locale.Text("games.aviator.flight_ended")
					btn.Style = discordgo.SecondaryButton
					btn.Disabled = true
					comps = []discordgo.MessageComponent{
						discordgo.ActionsRow{Components: []discordgo.MessageComponent{btn}},
					}
				}

				embeds := []*discordgo.MessageEmbed{embed}
				_, _ = s.ChannelMessageEditComplex(&discordgo.MessageEdit{
					ID:         msg.ID,
					Channel:    i.ChannelID,
					Embeds:     &embeds,
					Components: &comps,
				})
			}

			runGameLoop(session, updater)
		},
	}

	Enqueue(job)
}

func validatePreGame(guildID, userID string, bet int) bool {
	if bet < MinBet {
		return false
	}
	if database.GetBalance(guildID, userID) < bet {
		return false
	}
	if IsUserInGame(userID) {
		return false
	}
	return true
}

func setupGame(guildID, userID string, bet int, autoCashout float64) *AviatorSession {
	session := &AviatorSession{
		GuildID:     guildID,
		UserID:      userID,
		Bet:         bet,
		AutoCashout: autoCashout,
		CrashPoint:  GenerateCrashPoint(),
		ControlChan: make(chan bool, 1),
		DoneChan:    make(chan struct{}),
	}

	aviatorMutex.Lock()
	activeAviatorGames[userID] = session
	aviatorMutex.Unlock()

	return session
}

// GenerateCrashPoint produces a fair crash point with standard 96% RTP / 4% house edge
func GenerateCrashPoint() float64 {
	var b [8]byte
	_, _ = rand.Read(b[:])
	r := float64(binary.LittleEndian.Uint64(b[:])%1000000) / 1000000.0

	// 4% instant crash house edge
	if r < 0.04 {
		return 1.00
	}

	// Classic crash game probability curve
	crash := 0.96 / (1.0 - r)
	if crash < 1.00 {
		crash = 1.00
	}
	if crash > 100.00 {
		crash = 100.00
	}
	return math.Floor(crash*100) / 100
}

// CalculateMultiplier uses an exponential curve so multipliers climb smoothly without taking minutes
func CalculateMultiplier(elapsedSeconds float64) float64 {
	mult := math.Exp(0.06 * elapsedSeconds)
	return math.Floor(mult*100) / 100
}

func getInitialState(bet int, autoCashout float64, userID string) (*discordgo.MessageEmbed, discordgo.Button) {
	embed := utils.NewEmbed()
	embed.Title = locale.Text("games.aviator.aviator_starting")

	desc := locale.Text("games.aviator.bet_preparing_for_takeoff.formatted", locale.Data{"Bet": bet, "CurrencySymbol": config.Bot.CurrencySymbol})
	if autoCashout > 1.0 {
		desc += locale.Text("games.aviator.target_auto_cash_out_x.formatted", locale.Data{"AutoCashout": autoCashout})
	}
	embed.Description = desc
	embed.Color = utils.ColorBlue

	btn := discordgo.Button{
		Label:    locale.Text("games.aviator.cash_out"),
		Style:    discordgo.SuccessButton,
		CustomID: "aviator_stop_" + userID,
	}
	return embed, btn
}

func buildAltitudeGraph(multiplier float64) string {
	dots := int((multiplier - 1.0) * 3)
	if dots > 15 {
		dots = 15
	}
	if dots < 1 {
		dots = 1
	}
	return "🛫" + strings.Repeat("·", dots) + "✈️"
}

func runGameLoop(session *AviatorSession, update MessageUpdater) {
	defer cleanup(session.UserID)
	defer close(session.DoneChan)

	time.Sleep(800 * time.Millisecond)
	session.StartTime = time.Now()

	ticker := time.NewTicker(MultiplierSpeed)
	defer ticker.Stop()

	for {
		select {
		case <-session.ControlChan:
			// Manual Cash Out
			session.CashedOut = true
			elapsed := time.Since(session.StartTime).Seconds()
			multiplier := CalculateMultiplier(elapsed)

			if multiplier >= session.CrashPoint {
				update(utils.ErrorEmbed(locale.Text("games.aviator.crashed_at_x_you_didn_t_jump.formatted", locale.Data{"CrashPoint": session.CrashPoint, "Bet": session.Bet, "CurrencySymbol": config.Bot.CurrencySymbol})), true)
				return
			}

			totalPayout := int(float64(session.Bet) * multiplier)
			netProfit := totalPayout - session.Bet
			_ = database.AddCoins(session.GuildID, session.UserID, totalPayout)

			log.Printf("[AVIATOR WIN] User %s cashed out at x%.2f (bet: %d, payout: %d, profit: %d)",
				session.UserID, multiplier, session.Bet, totalPayout, netProfit)

			successDesc := locale.Text("games.aviator.you_jumped_at_x_total_payout_net.formatted", locale.Data{"Multiplier": multiplier, "TotalPayout": totalPayout, "CurrencySymbol": config.Bot.CurrencySymbol, "NetProfit": netProfit, "CurrencySymbol5": config.Bot.CurrencySymbol})
			update(utils.SuccessEmbed(locale.Text("games.aviator.cashed_out"), successDesc), true)
			return

		case <-ticker.C:
			elapsed := time.Since(session.StartTime).Seconds()
			multiplier := CalculateMultiplier(elapsed)

			// Check Auto Cash-Out
			if session.AutoCashout > 1.0 && multiplier >= session.AutoCashout {
				if session.AutoCashout <= session.CrashPoint {
					session.CashedOut = true
					multiplier = session.AutoCashout
					totalPayout := int(float64(session.Bet) * multiplier)
					netProfit := totalPayout - session.Bet
					_ = database.AddCoins(session.GuildID, session.UserID, totalPayout)

					log.Printf("[AVIATOR AUTO-WIN] User %s auto-cashed out at x%.2f (bet: %d, payout: %d)",
						session.UserID, multiplier, session.Bet, totalPayout)

					successDesc := locale.Text("games.aviator.auto_cash_out_triggered_at_x_total.formatted", locale.Data{"Multiplier": multiplier, "TotalPayout": totalPayout, "CurrencySymbol": config.Bot.CurrencySymbol, "NetProfit": netProfit, "CurrencySymbol5": config.Bot.CurrencySymbol})
					update(utils.SuccessEmbed(locale.Text("games.aviator.auto_cash_out_success"), successDesc), true)
					return
				}
			}

			// Check Crash
			if multiplier >= session.CrashPoint {
				update(utils.ErrorEmbed(locale.Text("games.aviator.crashed_at_x_you_lost.formatted", locale.Data{"CrashPoint": session.CrashPoint, "Bet": session.Bet, "CurrencySymbol": config.Bot.CurrencySymbol})), true)
				return
			}

			embed := utils.NewEmbed()
			embed.Title = locale.Text("games.aviator.aviator_flying")
			potentialWin := int(float64(session.Bet) * multiplier)
			potentialProfit := potentialWin - session.Bet

			desc := locale.Text("games.aviator.multiplier_x_potential_win_profit.formatted", locale.Data{"Multiplier": multiplier, "PotentialWin": potentialWin, "CurrencySymbol": config.Bot.CurrencySymbol, "PotentialProfit": potentialProfit})
			if session.AutoCashout > 1.0 {
				desc += locale.Text("games.aviator.auto_cash_out_x.formatted", locale.Data{"AutoCashout": session.AutoCashout})
			}
			embed.Description = desc
			embed.Color = utils.ColorBlue

			embed.Fields = []*discordgo.MessageEmbedField{
				{Name: locale.Text("games.aviator.altitude"), Value: buildAltitudeGraph(multiplier)},
			}

			update(embed, false)
		}
	}
}

// HandleButton handles clicking the Cash Out button
func HandleButton(s *discordgo.Session, i *discordgo.InteractionCreate) {
	customID := i.MessageComponentData().CustomID
	expectedUserID := strings.TrimPrefix(customID, "aviator_stop_")

	if i.Member.User.ID != expectedUserID {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: locale.Text("games.aviator.this_is_not_your_flight_only_can.formatted", locale.Data{"ExpectedUserID": expectedUserID}),
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	aviatorMutex.Lock()
	session, exists := activeAviatorGames[expectedUserID]
	aviatorMutex.Unlock()

	if !exists || session.CashedOut {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: locale.Text("games.aviator.flight_already_ended_or_not_active"),
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredMessageUpdate,
	})

	select {
	case session.ControlChan <- true:
	default:
	}
}

func cleanup(userID string) {
	aviatorMutex.Lock()
	delete(activeAviatorGames, userID)
	aviatorMutex.Unlock()
}

func respondPrivate(s *discordgo.Session, i *discordgo.InteractionCreate, embed *discordgo.MessageEmbed) {
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds: []*discordgo.MessageEmbed{embed},
			Flags:  discordgo.MessageFlagsEphemeral,
		},
	})
}

package games

import (
	"bot/internal/database"
	"bot/internal/locale"
	"bot/pkg/config"
	"bot/pkg/utils"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

const (
	MinSlotsBet            = 10
	SlotsInactivityTimeout = 30 * time.Second
)

// SlotSymbol defines each fruit/icon, its weight, and payouts
type SlotSymbol struct {
	Name          string
	Emoji         string
	Value         int     // Multiplier for 3-of-a-kind (Jackpot)
	TwoMatchValue float64 // Multiplier for 2-of-a-kind
	Weight        int     // Weight out of 100
}

var (
	// Calibrated paytable yielding ~96.7% RTP (3.3% house edge)
	slotSymbols = []SlotSymbol{
		{Name: "cherry", Emoji: "🍒", Value: 3, TwoMatchValue: 1.0, Weight: 36},
		{Name: "lemon", Emoji: "🍋", Value: 5, TwoMatchValue: 1.2, Weight: 26},
		{Name: "orange", Emoji: "🍊", Value: 7, TwoMatchValue: 1.5, Weight: 18},
		{Name: "bell", Emoji: "🔔", Value: 12, TwoMatchValue: 2.0, Weight: 12},
		{Name: "diamond", Emoji: "💎", Value: 25, TwoMatchValue: 3.5, Weight: 6},
		{Name: "seven", Emoji: "7️⃣", Value: 100, TwoMatchValue: 10.0, Weight: 2},
	}

	activeSlotsSessions = make(map[string]*SlotsSession)
	slotsMu             sync.Mutex
)

type SlotsSession struct {
	GuildID    string
	UserID     string
	Username   string
	Bet        int
	ChannelID  string
	MessageID  string
	IsSpinning bool
	Timer      *time.Timer
	mu         sync.Mutex
}

type SlotsResult struct {
	Reel1       SlotSymbol
	Reel2       SlotSymbol
	Reel3       SlotSymbol
	WinAmount   int
	NetProfit   int
	Multiplier  float64
	IsJackpot   bool
	IsTwoMatch  bool
	IsPush      bool
	MatchSymbol SlotSymbol
}

// getWeightedSymbol selects a reel symbol using uniform cryptographic randomness
func getWeightedSymbol() SlotSymbol {
	totalWeight := 0
	for _, s := range slotSymbols {
		totalWeight += s.Weight
	}

	var b [4]byte
	_, _ = rand.Read(b[:])
	r := int(binary.LittleEndian.Uint32(b[:]) % uint32(totalWeight))
	cumulative := 0

	for _, s := range slotSymbols {
		cumulative += s.Weight
		if r < cumulative {
			return s
		}
	}

	return slotSymbols[0]
}

// spinSlots executes a spin and computes winnings and net profit
func spinSlots(bet int) SlotsResult {
	r1 := getWeightedSymbol()
	r2 := getWeightedSymbol()
	r3 := getWeightedSymbol()

	result := SlotsResult{
		Reel1: r1,
		Reel2: r2,
		Reel3: r3,
	}

	if r1.Name == r2.Name && r2.Name == r3.Name {
		result.IsJackpot = true
		result.MatchSymbol = r1
		result.Multiplier = float64(r1.Value)
		result.WinAmount = int(float64(bet) * result.Multiplier)
		result.NetProfit = result.WinAmount - bet
	} else if r1.Name == r2.Name || r2.Name == r3.Name || r1.Name == r3.Name {
		result.IsTwoMatch = true
		var matchSymbol SlotSymbol
		if r1.Name == r2.Name || r1.Name == r3.Name {
			matchSymbol = r1
		} else {
			matchSymbol = r2
		}
		result.MatchSymbol = matchSymbol
		result.Multiplier = matchSymbol.TwoMatchValue
		result.WinAmount = int(float64(bet) * result.Multiplier)
		result.NetProfit = result.WinAmount - bet
		if result.Multiplier == 1.0 {
			result.IsPush = true
		}
	} else {
		result.Multiplier = 0
		result.WinAmount = 0
		result.NetProfit = -bet
	}

	return result
}

// createSpinningEmbed creates the initial suspense embed while the reels roll
func createSpinningEmbed(username string, bet int) *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{
		Title:       locale.Text("games.slots.slot_machine"),
		Description: locale.Text("games.slots.pulled_the_lever_bet_reels_are_spinning.formatted", locale.Data{"Username": username, "Bet": bet, "CurrencySymbol": config.Bot.CurrencySymbol}),
		Color:       0xFFD700, // Gold
	}
}

// createResultEmbed formats the final outcome with reels, payout, and profit
func createResultEmbed(username string, bet int, result SlotsResult) *discordgo.MessageEmbed {
	slotsDisplay := fmt.Sprintf("# %s | %s | %s", result.Reel1.Emoji, result.Reel2.Emoji, result.Reel3.Emoji)

	var color int
	var title, outcomeText string
	currency := config.Bot.CurrencySymbol

	if result.IsJackpot {
		color = 0xFFD700 // Gold
		title = locale.Text("games.slots.jackpot")
		outcomeText = locale.Text("games.slots.x_jackpot_hit.formatted", locale.Data{"Emoji": result.MatchSymbol.Emoji})
	} else if result.IsTwoMatch {
		if result.IsPush {
			color = 0x3498DB // Blue
			title = locale.Text("games.slots.push_bet_returned")
			outcomeText = locale.Text("games.slots.pair_of_cherries_your_bet_of_was.formatted", locale.Data{"Bet": bet, "Currency": currency})
		} else {
			color = 0x2ECC71 // Green
			title = locale.Text("games.slots.match_win")
			outcomeText = locale.Text("games.slots.pair_of.formatted", locale.Data{"Emoji": result.MatchSymbol.Emoji})
		}
	} else {
		color = 0xE74C3C // Red
		title = locale.Text("games.slots.no_luck")
		outcomeText = locale.Text("games.slots.no_matching_symbols_this_time")
	}

	profitStr := ""
	if result.NetProfit > 0 {
		profitStr = fmt.Sprintf("+%d %s", result.NetProfit, currency)
	} else if result.NetProfit == 0 {
		profitStr = fmt.Sprintf("0 %s", currency)
	} else {
		profitStr = fmt.Sprintf("%d %s", result.NetProfit, currency)
	}

	description := locale.Text("games.slots.spun_the_reels_bet_multiplier_x_total.formatted", locale.Data{"Username": username, "SlotsDisplay": slotsDisplay, "OutcomeText": outcomeText, "Bet": bet, "Currency": currency, "Multiplier": result.Multiplier, "WinAmount": result.WinAmount, "Currency8": currency, "ProfitStr": profitStr})

	return &discordgo.MessageEmbed{
		Title:       title,
		Description: description,
		Color:       color,
		Footer: &discordgo.MessageEmbedFooter{
			Text: "🍒 1x/3x | 🍋 1.2x/5x | 🍊 1.5x/7x | 🔔 2x/12x | 💎 3.5x/25x | 7️⃣ 10x/100x",
		},
	}
}

// buildSlotsActionRow creates the interactive "Spin Again" and "Leave" buttons
func buildSlotsActionRow(userID string, bet int, disabled bool) []discordgo.MessageComponent {
	return []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Label:    locale.Text("games.slots.spin_again.formatted", locale.Data{"Bet": bet, "CurrencySymbol": config.Bot.CurrencySymbol}),
					Style:    discordgo.SuccessButton,
					CustomID: fmt.Sprintf("slots_again_%s_%d", userID, bet),
					Disabled: disabled,
				},
				discordgo.Button{
					Label:    locale.Text("games.slots.leave"),
					Style:    discordgo.SecondaryButton,
					CustomID: fmt.Sprintf("slots_leave_%s", userID),
					Disabled: disabled,
				},
			},
		},
	}
}

// cleanupSlotsSession removes the session and resets the user's active game state
func cleanupSlotsSession(userID string, disableUI bool, s *discordgo.Session) {
	slotsMu.Lock()
	session, exists := activeSlotsSessions[userID]
	if !exists {
		slotsMu.Unlock()
		UnregisterActivePlayer(userID)
		return
	}
	delete(activeSlotsSessions, userID)
	slotsMu.Unlock()

	UnregisterActivePlayer(userID)

	session.mu.Lock()
	if session.Timer != nil {
		session.Timer.Stop()
		session.Timer = nil
	}
	channelID := session.ChannelID
	messageID := session.MessageID
	bet := session.Bet
	session.mu.Unlock()

	if disableUI && s != nil && channelID != "" && messageID != "" {
		rows := buildSlotsActionRow(userID, bet, true)
		_, _ = s.ChannelMessageEditComplex(&discordgo.MessageEdit{
			Channel:    channelID,
			ID:         messageID,
			Components: &rows,
		})
	}
}

// StartSlotsInteraction starts a slots game from a slash command (/bet slots <bet> or /slots <bet>)
func StartSlotsInteraction(s *discordgo.Session, i *discordgo.InteractionCreate, bet int) {
	if i.GuildID == "" {
		respondPrivate(s, i, utils.ErrorEmbed(locale.Text("games.aviator.this_game_can_only_be_played_within")))
		return
	}
	guildID := i.GuildID
	userID := i.Member.User.ID
	username := i.Member.User.Username
	channelID := i.ChannelID

	if bet < MinSlotsBet {
		respondPrivate(s, i, utils.ErrorEmbed(locale.Text("games.cups.minimum_bet_is.formatted4", locale.Data{"MinSlotsBet": MinSlotsBet, "CurrencySymbol": config.Bot.CurrencySymbol})))
		return
	}

	if !RegisterActivePlayer(userID) {
		respondPrivate(s, i, utils.ErrorEmbed(locale.Text("games.aviator.you_already_have_an_active_game_in")))
		return
	}

	if database.GetBalance(guildID, userID) < bet {
		UnregisterActivePlayer(userID)
		respondPrivate(s, i, utils.ErrorEmbed(locale.Text("games.slots.insufficient_balance_you_need.formatted", locale.Data{"Bet": bet, "CurrencySymbol": config.Bot.CurrencySymbol})))
		return
	}

	if err := database.CollectLostBet(guildID, userID, bet); err != nil {
		UnregisterActivePlayer(userID)
		respondPrivate(s, i, utils.ErrorEmbed(locale.Text("games.roulette.error_deducting_bet_coins")))
		return
	}

	embed := createSpinningEmbed(username, bet)
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: locale.Text("games.slots.the_reels_are_rolling.formatted", locale.Data{"UserID": userID}),
			Embeds:  []*discordgo.MessageEmbed{embed},
		},
	})
	if err != nil {
		_ = database.AddCoins(guildID, userID, bet)
		UnregisterActivePlayer(userID)
		return
	}

	msg, err := s.InteractionResponse(i.Interaction)
	if err != nil || msg == nil {
		_ = database.AddCoins(guildID, userID, bet)
		UnregisterActivePlayer(userID)
		return
	}

	session := &SlotsSession{
		GuildID:    guildID,
		UserID:     userID,
		Username:   username,
		Bet:        bet,
		ChannelID:  channelID,
		MessageID:  msg.ID,
		IsSpinning: true,
	}

	slotsMu.Lock()
	activeSlotsSessions[userID] = session
	slotsMu.Unlock()

	go executeSpinCycle(s, session)
}

// executeSpinCycle pauses 1.5s for suspense, computes results, and cleanly updates the message (1 edit)
func executeSpinCycle(s *discordgo.Session, session *SlotsSession) {
	// Suspense sleep (1500ms) - completely safe for Discord rate limits (1 edit only)
	time.Sleep(1500 * time.Millisecond)

	result := spinSlots(session.Bet)

	if result.WinAmount > 0 {
		_ = database.AddCoins(session.GuildID, session.UserID, result.WinAmount)
	}

	finalEmbed := createResultEmbed(session.Username, session.Bet, result)
	components := buildSlotsActionRow(session.UserID, session.Bet, false)

	_, err := s.ChannelMessageEditComplex(&discordgo.MessageEdit{
		Channel:    session.ChannelID,
		ID:         session.MessageID,
		Embeds:     &[]*discordgo.MessageEmbed{finalEmbed},
		Components: &components,
	})

	if err != nil {
		cleanupSlotsSession(session.UserID, false, s)
		return
	}

	session.mu.Lock()
	session.IsSpinning = false
	if session.Timer != nil {
		session.Timer.Stop()
	}
	session.Timer = time.AfterFunc(SlotsInactivityTimeout, func() {
		cleanupSlotsSession(session.UserID, true, s)
	})
	session.mu.Unlock()
}

// HandleSlotsInteraction handles button clicks for slots (Spin Again and Leave)
func HandleSlotsInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	customID := i.MessageComponentData().CustomID

	if strings.HasPrefix(customID, "slots_leave_") {
		parts := strings.Split(customID, "_")
		if len(parts) < 3 {
			return
		}
		expectedUser := parts[2]
		if i.Member.User.ID != expectedUser {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: locale.Text("games.blackjack.this_is_not_your_game"),
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})
			return
		}

		cleanupSlotsSession(expectedUser, false, s)

		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Content:    locale.Text("games.slots.stepped_away_from_the_slot_machine_thanks.formatted", locale.Data{"ExpectedUser": expectedUser}),
				Components: []discordgo.MessageComponent{},
			},
		})
		return
	}

	if strings.HasPrefix(customID, "slots_again_") {
		parts := strings.Split(customID, "_")
		if len(parts) < 4 {
			return
		}
		expectedUser := parts[2]
		bet, err := strconv.Atoi(parts[3])
		if err != nil {
			return
		}

		if i.Member.User.ID != expectedUser {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: locale.Text("games.blackjack.this_is_not_your_game"),
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})
			return
		}

		slotsMu.Lock()
		session, exists := activeSlotsSessions[expectedUser]
		slotsMu.Unlock()

		if !exists || session == nil {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: locale.Text("games.slots.this_session_has_expired_start_a_new"),
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})
			return
		}

		session.mu.Lock()
		if session.IsSpinning {
			session.mu.Unlock()
			return
		}
		if session.Timer != nil {
			session.Timer.Stop()
			session.Timer = nil
		}
		session.IsSpinning = true
		session.mu.Unlock()

		// Verify balance
		if database.GetBalance(session.GuildID, expectedUser) < bet {
			cleanupSlotsSession(expectedUser, true, s)
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: locale.Text("games.slots.insufficient_balance_you_need_to_spin_again.formatted", locale.Data{"Bet": bet, "CurrencySymbol": config.Bot.CurrencySymbol}),
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})
			return
		}

		// Deduct bet atomically
		if err := database.CollectLostBet(session.GuildID, expectedUser, bet); err != nil {
			cleanupSlotsSession(expectedUser, true, s)
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: locale.Text("games.slots.error_processing_your_bet"),
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})
			return
		}

		// Update message to spinning state and remove buttons
		spinningEmbed := createSpinningEmbed(session.Username, bet)
		err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Content:    locale.Text("games.slots.the_reels_are_rolling_again.formatted", locale.Data{"ExpectedUser": expectedUser}),
				Embeds:     []*discordgo.MessageEmbed{spinningEmbed},
				Components: []discordgo.MessageComponent{},
			},
		})
		if err != nil {
			_ = database.AddCoins(session.GuildID, expectedUser, bet)
			cleanupSlotsSession(expectedUser, false, s)
			return
		}

		go executeSpinCycle(s, session)
		return
	}
}

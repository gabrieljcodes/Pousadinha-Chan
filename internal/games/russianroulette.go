package games

import (
	"bot/internal/database"
	"bot/internal/locale"
	"bot/pkg/config"
	"bot/pkg/utils"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

const (
	MinRussianRouletteBet = 50
	ChallengeTimeout      = 30 * time.Second
	TurnTimeout           = 45 * time.Second
)

type RussianRouletteChallenge struct {
	GuildID      string
	ChallengerID string
	ChallengedID string
	Bet          int
	ChannelID    string
	MessageID    string
	TimeoutTimer *time.Timer
}

type RussianRouletteGame struct {
	GuildID     string
	Player1ID   string
	Player2ID   string
	Player1Name string
	Player2Name string
	CurrentTurn string
	Bet         int
	ChannelID   string
	MessageID   string
	Round       int
	Chamber     int // Bullet position (1-6)
	CurrentShot int // Current trigger position (1-6)
	GameOver    bool
	TurnTimer   *time.Timer
	mu          sync.Mutex
}

var (
	pendingChallenges = make(map[string]*RussianRouletteChallenge) // Key: ChallengedID
	challengerPending = make(map[string]string)                    // Key: ChallengerID -> ChallengedID
	pendingMu         sync.Mutex

	activeRouletteGames = make(map[string]*RussianRouletteGame)
	rrMu                sync.Mutex
)

// randomChamberCrypto selects a uniform random chamber between 1 and 6 using crypto/rand
func randomChamberCrypto() int {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return int(binary.LittleEndian.Uint32(b[:])%6) + 1
}

// randomCoinTossCrypto determines fairly who starts the duel
func randomCoinTossCrypto() bool {
	var b [1]byte
	_, _ = rand.Read(b[:])
	return b[0]%2 == 1
}

// isPlayerInGame checks if player is in an active Russian Roulette duel
func isPlayerInGame(playerID string) bool {
	rrMu.Lock()
	defer rrMu.Unlock()

	for _, game := range activeRouletteGames {
		if game.Player1ID == playerID || game.Player2ID == playerID {
			return true
		}
	}
	return false
}

// StartRussianRouletteInteraction handles slash command /roulette challenge @user amount
func StartRussianRouletteInteraction(s *discordgo.Session, i *discordgo.InteractionCreate, challenged *discordgo.User, amount int) {
	if i.GuildID == "" {
		respondPrivate(s, i, utils.ErrorEmbed(locale.Text("games.roulette.this_command_can_only_be_used_within")))
		return
	}
	guildID := i.GuildID
	challengerID := i.Member.User.ID
	challengedID := challenged.ID

	if amount < MinRussianRouletteBet {
		respondPrivate(s, i, utils.ErrorEmbed(locale.Text("games.cups.minimum_bet_is.formatted3", locale.Data{"MinRussianRouletteBet": MinRussianRouletteBet, "CurrencySymbol": config.Bot.CurrencySymbol})))
		return
	}

	if challengedID == challengerID {
		respondPrivate(s, i, utils.ErrorEmbed(locale.Text("games.russianroulette.you_cannot_challenge_yourself")))
		return
	}

	if challenged.Bot {
		respondPrivate(s, i, utils.ErrorEmbed(locale.Text("games.russianroulette.you_cannot_challenge_bots")))
		return
	}

	pendingMu.Lock()
	if _, exists := challengerPending[challengerID]; exists {
		pendingMu.Unlock()
		respondPrivate(s, i, utils.ErrorEmbed(locale.Text("games.russianroulette.you_already_have_an_outgoing_challenge_waiting")))
		return
	}
	if _, exists := pendingChallenges[challengedID]; exists {
		pendingMu.Unlock()
		respondPrivate(s, i, utils.ErrorEmbed(locale.Text("games.russianroulette.this_user_already_has_a_pending_challenge")))
		return
	}
	pendingMu.Unlock()

	if isPlayerInGame(challengerID) || IsUserInGame(challengerID) {
		respondPrivate(s, i, utils.ErrorEmbed(locale.Text("games.russianroulette.you_are_already_in_an_active_game")))
		return
	}
	if isPlayerInGame(challengedID) || IsUserInGame(challengedID) {
		respondPrivate(s, i, utils.ErrorEmbed(locale.Text("games.russianroulette.this_user_is_already_in_an_active")))
		return
	}

	challengerBalance := database.GetBalance(guildID, challengerID)
	if challengerBalance < amount {
		respondPrivate(s, i, utils.ErrorEmbed(locale.Text("games.blackjack.insufficient_balance_you_have.formatted2", locale.Data{"ChallengerBalance": challengerBalance, "CurrencySymbol": config.Bot.CurrencySymbol})))
		return
	}

	// Reserve coins in escrow
	if err := database.CollectLostBet(guildID, challengerID, amount); err != nil {
		respondPrivate(s, i, utils.ErrorEmbed(locale.Text("games.russianroulette.error_reserving_bet_coins")))
		return
	}
	RegisterActivePlayer(challengerID)

	embed := &discordgo.MessageEmbed{
		Title:       locale.Text("games.russianroulette.russian_roulette_challenge"),
		Description: locale.Text("games.russianroulette.challenged_to_a_game_of_russian_roulette.formatted", locale.Data{"ChallengerID": challengerID, "ChallengedID": challengedID}),
		Color:       0x8B0000,
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   locale.Text("games.blackjack.bet"),
				Value:  fmt.Sprintf("%d %s", amount, config.Bot.CurrencySymbol),
				Inline: true,
			},
			{
				Name:   locale.Text("games.russianroulette.time"),
				Value:  locale.Text("games.russianroulette.seconds_to_accept"),
				Inline: true,
			},
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: locale.Text("games.russianroulette.challenger_s_bet_has_been_reserved_in"),
		},
	}

	buttons := []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Label:    locale.Text("games.russianroulette.accept"),
					Style:    discordgo.SuccessButton,
					CustomID: fmt.Sprintf("rr_accept_%s_%s", challengerID, challengedID),
				},
				discordgo.Button{
					Label:    locale.Text("games.russianroulette.decline"),
					Style:    discordgo.DangerButton,
					CustomID: fmt.Sprintf("rr_decline_%s_%s", challengerID, challengedID),
				},
			},
		},
	}

	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content:    locale.Text("games.russianroulette.you_have_been_challenged.formatted", locale.Data{"ChallengedID": challengedID}),
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: buttons,
		},
	})

	if err != nil {
		_ = database.AddCoins(guildID, challengerID, amount)
		UnregisterActivePlayer(challengerID)
		return
	}

	msg, _ := s.InteractionResponse(i.Interaction)
	msgID := ""
	if msg != nil {
		msgID = msg.ID
	}

	challenge := &RussianRouletteChallenge{
		GuildID:      guildID,
		ChallengerID: challengerID,
		ChallengedID: challengedID,
		Bet:          amount,
		ChannelID:    i.ChannelID,
		MessageID:    msgID,
	}

	challenge.TimeoutTimer = time.AfterFunc(ChallengeTimeout, func() {
		expireChallenge(s, challengedID)
	})

	pendingMu.Lock()
	pendingChallenges[challengedID] = challenge
	challengerPending[challengerID] = challengedID
	pendingMu.Unlock()
}

func HandleRussianRouletteInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	customID := i.MessageComponentData().CustomID
	userID := i.Member.User.ID

	if strings.HasPrefix(customID, "rr_accept_") {
		handleAccept(s, i, userID)
	} else if strings.HasPrefix(customID, "rr_decline_") {
		handleDecline(s, i, userID)
	} else if strings.HasPrefix(customID, "rr_shoot_") {
		handleShoot(s, i)
	}
}

func handleAccept(s *discordgo.Session, i *discordgo.InteractionCreate, userID string) {
	pendingMu.Lock()
	challenge, exists := pendingChallenges[userID]
	if !exists {
		pendingMu.Unlock()
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: locale.Text("games.russianroulette.no_pending_challenge_found_for_you"),
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	// Stop timer and remove atomically to prevent expiration race
	if challenge.TimeoutTimer != nil {
		challenge.TimeoutTimer.Stop()
	}
	delete(pendingChallenges, challenge.ChallengedID)
	delete(challengerPending, challenge.ChallengerID)
	pendingMu.Unlock()

	// Verify challenged player's balance
	challengedBalance := database.GetBalance(challenge.GuildID, challenge.ChallengedID)
	if challengedBalance < challenge.Bet {
		// Refund challenger
		_ = database.AddCoins(challenge.GuildID, challenge.ChallengerID, challenge.Bet)
		UnregisterActivePlayer(challenge.ChallengerID)

		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Content:    locale.Text("games.russianroulette.does_not_have_enough_balance_to_accept.formatted", locale.Data{"ChallengedID": challenge.ChallengedID}),
				Embeds:     []*discordgo.MessageEmbed{},
				Components: []discordgo.MessageComponent{},
			},
		})
		return
	}

	// Atomically deduct challenged player's bet
	if err := database.CollectLostBet(challenge.GuildID, challenge.ChallengedID, challenge.Bet); err != nil {
		_ = database.AddCoins(challenge.GuildID, challenge.ChallengerID, challenge.Bet)
		UnregisterActivePlayer(challenge.ChallengerID)

		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Content:    locale.Text("games.russianroulette.error_processing_bet_challenge_cancelled_and_challenger"),
				Embeds:     []*discordgo.MessageEmbed{},
				Components: []discordgo.MessageComponent{},
			},
		})
		return
	}

	RegisterActivePlayer(challenge.ChallengedID)

	totalPot := challenge.Bet * 2

	challengerMember, _ := s.GuildMember(i.GuildID, challenge.ChallengerID)
	challengedMember, _ := s.GuildMember(i.GuildID, challenge.ChallengedID)

	challengerName := challenge.ChallengerID
	if challengerMember != nil && challengerMember.User != nil {
		challengerName = challengerMember.User.Username
	}

	challengedName := challenge.ChallengedID
	if challengedMember != nil && challengedMember.User != nil {
		challengedName = challengedMember.User.Username
	}

	game := &RussianRouletteGame{
		GuildID:     challenge.GuildID,
		Player1ID:   challenge.ChallengerID,
		Player2ID:   challenge.ChallengedID,
		Player1Name: challengerName,
		Player2Name: challengedName,
		CurrentTurn: challenge.ChallengerID,
		Bet:         challenge.Bet,
		ChannelID:   i.ChannelID,
		MessageID:   i.Message.ID,
		Round:       1,
		Chamber:     randomChamberCrypto(),
		CurrentShot: 1,
		GameOver:    false,
	}

	if randomCoinTossCrypto() {
		game.CurrentTurn = challenge.ChallengedID
	}

	gameID := fmt.Sprintf("%s_%s", challenge.ChallengerID, challenge.ChallengedID)

	rrMu.Lock()
	activeRouletteGames[gameID] = game
	rrMu.Unlock()

	// Start 45s turn timer to prevent stalls
	game.TurnTimer = time.AfterFunc(TurnTimeout, func() {
		handleTurnTimeout(s, game)
	})

	embed := game.createGameEmbed(totalPot)
	components := game.createShootButton()

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
		},
	})
}

func handleDecline(s *discordgo.Session, i *discordgo.InteractionCreate, userID string) {
	pendingMu.Lock()
	challenge, exists := pendingChallenges[userID]
	if !exists {
		pendingMu.Unlock()
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: locale.Text("games.russianroulette.no_pending_challenge_found"),
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	if challenge.TimeoutTimer != nil {
		challenge.TimeoutTimer.Stop()
	}
	delete(pendingChallenges, challenge.ChallengedID)
	delete(challengerPending, challenge.ChallengerID)
	pendingMu.Unlock()

	// Refund challenger
	_ = database.AddCoins(challenge.GuildID, challenge.ChallengerID, challenge.Bet)
	UnregisterActivePlayer(challenge.ChallengerID)

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content:    locale.Text("games.russianroulette.declined_the_challenge_s_bet_of_has.formatted", locale.Data{"UserID": userID, "ChallengerID": challenge.ChallengerID, "Bet": challenge.Bet, "CurrencySymbol": config.Bot.CurrencySymbol}),
			Embeds:     []*discordgo.MessageEmbed{},
			Components: []discordgo.MessageComponent{},
		},
	})
}

func handleShoot(s *discordgo.Session, i *discordgo.InteractionCreate) {
	customID := i.MessageComponentData().CustomID
	parts := strings.Split(customID, "_")
	if len(parts) < 4 {
		return
	}

	gameID := parts[2] + "_" + parts[3]

	rrMu.Lock()
	game, exists := activeRouletteGames[gameID]
	if !exists {
		gameID = parts[3] + "_" + parts[2]
		game, exists = activeRouletteGames[gameID]
	}
	rrMu.Unlock()

	if !exists || game == nil {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: locale.Text("games.russianroulette.game_not_found_or_already_finished"),
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	game.mu.Lock()
	if i.Member.User.ID != game.CurrentTurn {
		game.mu.Unlock()
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: locale.Text("games.russianroulette.it_s_not_your_turn"),
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	if game.GameOver {
		game.mu.Unlock()
		return
	}

	// Stop turn timer since player acted
	if game.TurnTimer != nil {
		game.TurnTimer.Stop()
		game.TurnTimer = nil
	}

	died := game.CurrentShot == game.Chamber
	totalPot := game.Bet * 2

	if died {
		game.GameOver = true
		survivorID := game.getOtherPlayer(game.CurrentTurn)
		loserID := game.CurrentTurn
		roundNum := game.Round
		shotPos := game.CurrentShot
		game.mu.Unlock()

		cleanupGame(game)

		if database.DB != nil {
			_ = database.AddCoins(game.GuildID, survivorID, totalPot)
		}

		embed := &discordgo.MessageEmbed{
			Title:       locale.Text("games.russianroulette.russian_roulette_game_over"),
			Description: locale.Text("games.russianroulette.pow_pulled_the_trigger_and_the_chamber.formatted", locale.Data{"LoserID": loserID}),
			Color:       0x8B0000,
			Fields: []*discordgo.MessageEmbedField{
				{
					Name:   locale.Text("games.russianroulette.survivor_winner"),
					Value:  fmt.Sprintf("<@%s>", survivorID),
					Inline: true,
				},
				{
					Name:   locale.Text("games.russianroulette.prize"),
					Value:  fmt.Sprintf("%d %s", totalPot, config.Bot.CurrencySymbol),
					Inline: true,
				},
				{
					Name:   locale.Text("games.russianroulette.shot_details"),
					Value:  locale.Text("games.russianroulette.round_fatal_shot_position.formatted", locale.Data{"RoundNum": roundNum, "ShotPos": shotPos}),
					Inline: false,
				},
			},
			Footer: &discordgo.MessageEmbedFooter{
				Text: locale.Text("games.russianroulette.game_over_the_survivor_takes_all"),
			},
		}

		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Embeds:     []*discordgo.MessageEmbed{embed},
				Components: []discordgo.MessageComponent{},
			},
		})

	} else {
		survivor := game.CurrentTurn
		nextPlayer := game.getOtherPlayer(survivor)
		shotPos := game.CurrentShot

		embed := &discordgo.MessageEmbed{
			Title:       locale.Text("games.russianroulette.russian_roulette"),
			Description: locale.Text("games.russianroulette.click_pulled_the_trigger_and_survived.formatted", locale.Data{"Survivor": survivor}),
			Color:       0x00FF00,
			Fields: []*discordgo.MessageEmbedField{
				{
					Name:   locale.Text("games.russianroulette.chamber_result"),
					Value:  locale.Text("games.russianroulette.chamber_was_empty.formatted", locale.Data{"ShotPos": shotPos}),
					Inline: false,
				},
				{
					Name:   locale.Text("games.russianroulette.total_pot"),
					Value:  fmt.Sprintf("%d %s", totalPot, config.Bot.CurrencySymbol),
					Inline: true,
				},
				{
					Name:   locale.Text("games.russianroulette.next_turn"),
					Value:  locale.Text("games.russianroulette.s_turn.formatted", locale.Data{"NextPlayer": nextPlayer}),
					Inline: true,
				},
			},
		}

		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Embeds:     []*discordgo.MessageEmbed{embed},
				Components: []discordgo.MessageComponent{},
			},
		})

		// Release mutex during sleep to avoid blocking concurrent threads
		game.mu.Unlock()

		time.Sleep(1500 * time.Millisecond)

		game.mu.Lock()
		if game.GameOver {
			game.mu.Unlock()
			return
		}

		game.CurrentShot++
		game.CurrentTurn = nextPlayer
		game.Round++

		newEmbed := game.createGameEmbed(totalPot)
		components := game.createShootButton()

		// Start new turn timer for next player
		game.TurnTimer = time.AfterFunc(TurnTimeout, func() {
			handleTurnTimeout(s, game)
		})
		game.mu.Unlock()

		_, _ = s.ChannelMessageEditComplex(&discordgo.MessageEdit{
			Channel:    i.ChannelID,
			ID:         i.Message.ID,
			Embeds:     &[]*discordgo.MessageEmbed{newEmbed},
			Components: &components,
		})
	}
}

// handleTurnTimeout handles a player stalling on their turn and forfeits the duel
func handleTurnTimeout(s *discordgo.Session, game *RussianRouletteGame) {
	game.mu.Lock()
	if game.GameOver {
		game.mu.Unlock()
		return
	}
	game.GameOver = true
	loserID := game.CurrentTurn
	winnerID := game.getOtherPlayer(loserID)
	channelID := game.ChannelID
	messageID := game.MessageID
	totalPot := game.Bet * 2
	game.mu.Unlock()

	cleanupGame(game)

	if database.DB != nil {
		_ = database.AddCoins(game.GuildID, winnerID, totalPot)
	}

	embed := &discordgo.MessageEmbed{
		Title:       locale.Text("games.russianroulette.russian_roulette_forfeit_timed_out"),
		Description: locale.Text("games.russianroulette.took_longer_than_seconds_to_pull_the.formatted", locale.Data{"LoserID": loserID, "WinnerID": winnerID, "TotalPot": totalPot, "CurrencySymbol": config.Bot.CurrencySymbol}),
		Color:       utils.ColorGold,
		Footer: &discordgo.MessageEmbedFooter{
			Text: locale.Text("games.russianroulette.don_t_accept_duels_if_you_re"),
		},
	}

	_, _ = s.ChannelMessageEditComplex(&discordgo.MessageEdit{
		Channel:    channelID,
		ID:         messageID,
		Embeds:     &[]*discordgo.MessageEmbed{embed},
		Components: &[]discordgo.MessageComponent{},
	})
}

func (g *RussianRouletteGame) createGameEmbed(totalPot int) *discordgo.MessageEmbed {
	currentPlayerName := g.Player1Name
	if g.CurrentTurn == g.Player2ID {
		currentPlayerName = g.Player2Name
	}

	return &discordgo.MessageEmbed{
		Title:       locale.Text("games.russianroulette.russian_roulette_duel"),
		Description: locale.Text("games.russianroulette.it_s_s_turn_you_have_seconds.formatted", locale.Data{"CurrentPlayerName": currentPlayerName}),
		Color:       0x8B0000,
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   locale.Text("games.russianroulette.player"),
				Value:  fmt.Sprintf("%s%s", g.Player1Name, getTurnIndicator(g.Player1ID, g.CurrentTurn)),
				Inline: true,
			},
			{
				Name:   locale.Text("games.russianroulette.player_07e8d4"),
				Value:  fmt.Sprintf("%s%s", g.Player2Name, getTurnIndicator(g.Player2ID, g.CurrentTurn)),
				Inline: true,
			},
			{
				Name:   locale.Text("games.russianroulette.pot"),
				Value:  fmt.Sprintf("%d %s", totalPot, config.Bot.CurrencySymbol),
				Inline: true,
			},
			{
				Name:   locale.Text("games.russianroulette.round"),
				Value:  fmt.Sprintf("%d", g.Round),
				Inline: true,
			},
			{
				Name:   locale.Text("games.russianroulette.cylinder"),
				Value:  locale.Text("games.russianroulette.position.formatted", locale.Data{"CurrentShot": g.CurrentShot}),
				Inline: true,
			},
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: locale.Text("games.russianroulette.survivor_takes_pull_the_trigger_with_the.formatted", locale.Data{"TotalPot": totalPot, "CurrencySymbol": config.Bot.CurrencySymbol}),
		},
	}
}

func (g *RussianRouletteGame) createShootButton() []discordgo.MessageComponent {
	gameID := fmt.Sprintf("%s_%s", g.Player1ID, g.Player2ID)
	return []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Label:    locale.Text("games.russianroulette.shoot"),
					Style:    discordgo.DangerButton,
					CustomID: fmt.Sprintf("rr_shoot_%s", gameID),
					Emoji:    &discordgo.ComponentEmoji{Name: "💀"},
				},
			},
		},
	}
}

func (g *RussianRouletteGame) getOtherPlayer(playerID string) string {
	if playerID == g.Player1ID {
		return g.Player2ID
	}
	return g.Player1ID
}

func getTurnIndicator(playerID string, currentTurn string) string {
	if playerID == currentTurn {
		return locale.Text("games.russianroulette.your_turn")
	}
	return ""
}

func expireChallenge(s *discordgo.Session, challengedID string) {
	pendingMu.Lock()
	challenge, exists := pendingChallenges[challengedID]
	if !exists {
		pendingMu.Unlock()
		return
	}
	delete(pendingChallenges, challengedID)
	delete(challengerPending, challenge.ChallengerID)
	pendingMu.Unlock()

	// Refund challenger
	_ = database.AddCoins(challenge.GuildID, challenge.ChallengerID, challenge.Bet)
	UnregisterActivePlayer(challenge.ChallengerID)

	s.ChannelMessageSend(challenge.ChannelID,
		locale.Text("games.russianroulette.did_not_respond_to_s_challenge_in.formatted", locale.Data{"ChallengedID": challengedID, "ChallengerID": challenge.ChallengerID, "Bet": challenge.Bet, "CurrencySymbol": config.Bot.CurrencySymbol}))
}

func cleanupGame(g *RussianRouletteGame) {
	gameID1 := fmt.Sprintf("%s_%s", g.Player1ID, g.Player2ID)
	gameID2 := fmt.Sprintf("%s_%s", g.Player2ID, g.Player1ID)

	rrMu.Lock()
	delete(activeRouletteGames, gameID1)
	delete(activeRouletteGames, gameID2)
	rrMu.Unlock()

	UnregisterActivePlayer(g.Player1ID)
	UnregisterActivePlayer(g.Player2ID)

	g.mu.Lock()
	if g.TurnTimer != nil {
		g.TurnTimer.Stop()
		g.TurnTimer = nil
	}
	g.mu.Unlock()
}

package games

import (
	"crypto/rand"
	"encoding/binary"
	"estudocoin/internal/database"
	"estudocoin/pkg/config"
	"estudocoin/pkg/utils"
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
	ChallengerID string
	ChallengedID string
	Bet          int
	ChannelID    string
	MessageID    string
	TimeoutTimer *time.Timer
}

type RussianRouletteGame struct {
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

// CmdRussianRoulette handles text challenge: !roulette @user <amount>
func CmdRussianRoulette(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	if len(args) < 2 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.InfoEmbed("🔫 Russian Roulette",
			"Usage: `!roulette @user <amount>`\n\nChallenge another user to a game of Russian Roulette. Winner takes all!"))
		return
	}

	amount, err := parseAmount(args[len(args)-1])
	if err != nil || amount <= 0 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Invalid amount. Use a positive number."))
		return
	}

	if amount < MinRussianRouletteBet {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed(fmt.Sprintf("Minimum bet is %d %s", MinRussianRouletteBet, config.Bot.CurrencySymbol)))
		return
	}

	userArg := args[0]
	challengedID := ""

	if strings.HasPrefix(userArg, "<@") && strings.HasSuffix(userArg, ">") {
		challengedID = strings.TrimPrefix(userArg, "<@")
		challengedID = strings.TrimPrefix(challengedID, "!")
		challengedID = strings.TrimSuffix(challengedID, ">")
	} else {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Mention a valid user. Example: `!roulette @user 100`"))
		return
	}

	challengerID := m.Author.ID

	if challengedID == challengerID {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("You cannot challenge yourself!"))
		return
	}

	challengedMember, err := s.GuildMember(m.GuildID, challengedID)
	if err != nil || challengedMember.User.Bot {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Invalid user or bot."))
		return
	}

	// Concurrency checks
	pendingMu.Lock()
	if _, exists := challengerPending[challengerID]; exists {
		pendingMu.Unlock()
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("You already have an outgoing challenge waiting for a response!"))
		return
	}
	if _, exists := pendingChallenges[challengedID]; exists {
		pendingMu.Unlock()
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("This user already has a pending challenge!"))
		return
	}
	pendingMu.Unlock()

	if isPlayerInGame(challengerID) || IsUserInGame(challengerID) {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("You are already in an active game!"))
		return
	}
	if isPlayerInGame(challengedID) || IsUserInGame(challengedID) {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("This user is already in an active game!"))
		return
	}

	challengerBalance := database.GetBalance(challengerID)
	if challengerBalance < amount {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed(fmt.Sprintf("Insufficient balance! You have %d %s", challengerBalance, config.Bot.CurrencySymbol)))
		return
	}

	// Atomically reserve challenger's bet
	if err := database.CollectLostBet(challengerID, amount); err != nil {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Error reserving bet coins."))
		return
	}
	RegisterActivePlayer(challengerID)

	embed := &discordgo.MessageEmbed{
		Title:       "🔫 Russian Roulette Challenge",
		Description: fmt.Sprintf("<@%s> challenged <@%s> to a game of Russian Roulette!", challengerID, challengedID),
		Color:       0x8B0000,
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   "💰 Bet",
				Value:  fmt.Sprintf("%d %s", amount, config.Bot.CurrencySymbol),
				Inline: true,
			},
			{
				Name:   "⏱️ Time",
				Value:  "30 seconds to accept",
				Inline: true,
			},
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: "Challenger's bet has been reserved in escrow.",
		},
	}

	buttons := []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Label:    "✅ Accept",
					Style:    discordgo.SuccessButton,
					CustomID: fmt.Sprintf("rr_accept_%s_%s", challengerID, challengedID),
				},
				discordgo.Button{
					Label:    "❌ Decline",
					Style:    discordgo.DangerButton,
					CustomID: fmt.Sprintf("rr_decline_%s_%s", challengerID, challengedID),
				},
			},
		},
	}

	msg, err := s.ChannelMessageSendComplex(m.ChannelID, &discordgo.MessageSend{
		Content:    fmt.Sprintf("<@%s>, you have been challenged!", challengedID),
		Embeds:     []*discordgo.MessageEmbed{embed},
		Components: buttons,
	})

	if err != nil || msg == nil {
		_ = database.AddCoins(challengerID, amount)
		UnregisterActivePlayer(challengerID)
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Failed to send challenge message."))
		return
	}

	challenge := &RussianRouletteChallenge{
		ChallengerID: challengerID,
		ChallengedID: challengedID,
		Bet:          amount,
		ChannelID:    m.ChannelID,
		MessageID:    msg.ID,
	}

	challenge.TimeoutTimer = time.AfterFunc(ChallengeTimeout, func() {
		expireChallenge(s, challengedID)
	})

	pendingMu.Lock()
	pendingChallenges[challengedID] = challenge
	challengerPending[challengerID] = challengedID
	pendingMu.Unlock()
}

// StartRussianRouletteInteraction handles slash command /roulette challenge @user amount
func StartRussianRouletteInteraction(s *discordgo.Session, i *discordgo.InteractionCreate, challenged *discordgo.User, amount int) {
	challengerID := i.Member.User.ID
	challengedID := challenged.ID

	if amount < MinRussianRouletteBet {
		respondPrivate(s, i, utils.ErrorEmbed(fmt.Sprintf("Minimum bet is %d %s", MinRussianRouletteBet, config.Bot.CurrencySymbol)))
		return
	}

	if challengedID == challengerID {
		respondPrivate(s, i, utils.ErrorEmbed("You cannot challenge yourself!"))
		return
	}

	if challenged.Bot {
		respondPrivate(s, i, utils.ErrorEmbed("You cannot challenge bots!"))
		return
	}

	pendingMu.Lock()
	if _, exists := challengerPending[challengerID]; exists {
		pendingMu.Unlock()
		respondPrivate(s, i, utils.ErrorEmbed("You already have an outgoing challenge waiting for a response!"))
		return
	}
	if _, exists := pendingChallenges[challengedID]; exists {
		pendingMu.Unlock()
		respondPrivate(s, i, utils.ErrorEmbed("This user already has a pending challenge!"))
		return
	}
	pendingMu.Unlock()

	if isPlayerInGame(challengerID) || IsUserInGame(challengerID) {
		respondPrivate(s, i, utils.ErrorEmbed("You are already in an active game!"))
		return
	}
	if isPlayerInGame(challengedID) || IsUserInGame(challengedID) {
		respondPrivate(s, i, utils.ErrorEmbed("This user is already in an active game!"))
		return
	}

	challengerBalance := database.GetBalance(challengerID)
	if challengerBalance < amount {
		respondPrivate(s, i, utils.ErrorEmbed(fmt.Sprintf("Insufficient balance! You have %d %s", challengerBalance, config.Bot.CurrencySymbol)))
		return
	}

	// Reserve coins in escrow
	if err := database.CollectLostBet(challengerID, amount); err != nil {
		respondPrivate(s, i, utils.ErrorEmbed("Error reserving bet coins."))
		return
	}
	RegisterActivePlayer(challengerID)

	embed := &discordgo.MessageEmbed{
		Title:       "🔫 Russian Roulette Challenge",
		Description: fmt.Sprintf("<@%s> challenged <@%s> to a game of Russian Roulette!", challengerID, challengedID),
		Color:       0x8B0000,
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   "💰 Bet",
				Value:  fmt.Sprintf("%d %s", amount, config.Bot.CurrencySymbol),
				Inline: true,
			},
			{
				Name:   "⏱️ Time",
				Value:  "30 seconds to accept",
				Inline: true,
			},
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: "Challenger's bet has been reserved in escrow.",
		},
	}

	buttons := []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Label:    "✅ Accept",
					Style:    discordgo.SuccessButton,
					CustomID: fmt.Sprintf("rr_accept_%s_%s", challengerID, challengedID),
				},
				discordgo.Button{
					Label:    "❌ Decline",
					Style:    discordgo.DangerButton,
					CustomID: fmt.Sprintf("rr_decline_%s_%s", challengerID, challengedID),
				},
			},
		},
	}

	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content:    fmt.Sprintf("<@%s>, you have been challenged!", challengedID),
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: buttons,
		},
	})

	if err != nil {
		_ = database.AddCoins(challengerID, amount)
		UnregisterActivePlayer(challengerID)
		return
	}

	msg, _ := s.InteractionResponse(i.Interaction)
	msgID := ""
	if msg != nil {
		msgID = msg.ID
	}

	challenge := &RussianRouletteChallenge{
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
				Content: "❌ No pending challenge found for you!",
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
	challengedBalance := database.GetBalance(challenge.ChallengedID)
	if challengedBalance < challenge.Bet {
		// Refund challenger
		_ = database.AddCoins(challenge.ChallengerID, challenge.Bet)
		UnregisterActivePlayer(challenge.ChallengerID)

		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Content:    fmt.Sprintf("❌ <@%s> does not have enough balance to accept! Bet refunded.", challenge.ChallengedID),
				Embeds:     []*discordgo.MessageEmbed{},
				Components: []discordgo.MessageComponent{},
			},
		})
		return
	}

	// Atomically deduct challenged player's bet
	if err := database.CollectLostBet(challenge.ChallengedID, challenge.Bet); err != nil {
		_ = database.AddCoins(challenge.ChallengerID, challenge.Bet)
		UnregisterActivePlayer(challenge.ChallengerID)

		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Content:    "❌ Error processing bet. Challenge cancelled and challenger refunded.",
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
				Content: "❌ No pending challenge found!",
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
	_ = database.AddCoins(challenge.ChallengerID, challenge.Bet)
	UnregisterActivePlayer(challenge.ChallengerID)

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content:    fmt.Sprintf("❌ <@%s> declined the challenge! <@%s>'s bet of %d %s has been refunded.", userID, challenge.ChallengerID, challenge.Bet, config.Bot.CurrencySymbol),
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
				Content: "❌ Game not found or already finished!",
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
				Content: "❌ It's not your turn!",
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
			_ = database.AddCoins(survivorID, totalPot)
		}

		embed := &discordgo.MessageEmbed{
			Title:       "🔫 Russian Roulette - GAME OVER",
			Description: fmt.Sprintf("💥 **POW!** <@%s> pulled the trigger and the chamber was **LOADED!**", loserID),
			Color:       0x8B0000,
			Fields: []*discordgo.MessageEmbedField{
				{
					Name:   "🏆 Survivor / Winner",
					Value:  fmt.Sprintf("<@%s>", survivorID),
					Inline: true,
				},
				{
					Name:   "💰 Prize",
					Value:  fmt.Sprintf("%d %s", totalPot, config.Bot.CurrencySymbol),
					Inline: true,
				},
				{
					Name:   "🎲 Shot Details",
					Value:  fmt.Sprintf("Round: %d | Fatal shot position: %d/6", roundNum, shotPos),
					Inline: false,
				},
			},
			Footer: &discordgo.MessageEmbedFooter{
				Text: "Game Over - The survivor takes all!",
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
			Title:       "🔫 Russian Roulette",
			Description: fmt.Sprintf("😅 **CLICK!** <@%s> pulled the trigger and... **SURVIVED!**", survivor),
			Color:       0x00FF00,
			Fields: []*discordgo.MessageEmbedField{
				{
					Name:   "🎲 Chamber Result",
					Value:  fmt.Sprintf("Chamber %d was empty!", shotPos),
					Inline: false,
				},
				{
					Name:   "💰 Total Pot",
					Value:  fmt.Sprintf("%d %s", totalPot, config.Bot.CurrencySymbol),
					Inline: true,
				},
				{
					Name:   "🔄 Next Turn",
					Value:  fmt.Sprintf("<@%s>'s turn...", nextPlayer),
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
		_ = database.AddCoins(winnerID, totalPot)
	}

	embed := &discordgo.MessageEmbed{
		Title: "🔫 Russian Roulette - FORFEIT (TIMED OUT)",
		Description: fmt.Sprintf("⏰ <@%s> took longer than 45 seconds to pull the trigger and fled!\n\n"+
			"🏆 **Winner by default:** <@%s>\n"+
			"💰 **Prize Awarded:** %d %s",
			loserID, winnerID, totalPot, config.Bot.CurrencySymbol),
		Color: utils.ColorGold,
		Footer: &discordgo.MessageEmbedFooter{
			Text: "Don't accept duels if you're too afraid to shoot!",
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
		Title:       "🔫 Russian Roulette Duel",
		Description: fmt.Sprintf("It's **%s**'s turn!\nYou have **45 seconds** to pull the trigger...", currentPlayerName),
		Color:       0x8B0000,
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   "👤 Player 1",
				Value:  fmt.Sprintf("%s%s", g.Player1Name, getTurnIndicator(g.Player1ID, g.CurrentTurn)),
				Inline: true,
			},
			{
				Name:   "👤 Player 2",
				Value:  fmt.Sprintf("%s%s", g.Player2Name, getTurnIndicator(g.Player2ID, g.CurrentTurn)),
				Inline: true,
			},
			{
				Name:   "💰 Pot",
				Value:  fmt.Sprintf("%d %s", totalPot, config.Bot.CurrencySymbol),
				Inline: true,
			},
			{
				Name:   "🎲 Round",
				Value:  fmt.Sprintf("%d", g.Round),
				Inline: true,
			},
			{
				Name:   "🔫 Cylinder",
				Value:  fmt.Sprintf("Position %d/6", g.CurrentShot),
				Inline: true,
			},
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("Survivor takes %d %s! Pull the trigger with the button below.", totalPot, config.Bot.CurrencySymbol),
		},
	}
}

func (g *RussianRouletteGame) createShootButton() []discordgo.MessageComponent {
	gameID := fmt.Sprintf("%s_%s", g.Player1ID, g.Player2ID)
	return []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Label:    "🔫 SHOOT",
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
		return " ⬅️ **(Your turn)**"
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
	_ = database.AddCoins(challenge.ChallengerID, challenge.Bet)
	UnregisterActivePlayer(challenge.ChallengerID)

	s.ChannelMessageSend(challenge.ChannelID,
		fmt.Sprintf("⏰ <@%s> did not respond to <@%s>'s challenge in time! Challenge expired and %d %s refunded to challenger.",
			challengedID, challenge.ChallengerID, challenge.Bet, config.Bot.CurrencySymbol))
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

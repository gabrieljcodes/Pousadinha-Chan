package games

import (
	"estudocoin/internal/database"
	"estudocoin/pkg/config"
	"estudocoin/pkg/utils"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

type RussianRouletteChallenge struct {
	ChallengerID string
	ChallengedID string
	Bet          int
	ChannelID    string
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
	mu          sync.Mutex
}

var (
	pendingChallenges = make(map[string]*RussianRouletteChallenge)
	pendingMu         sync.Mutex

	activeRouletteGames = make(map[string]*RussianRouletteGame)
	rouletteMu          sync.Mutex
)

const ChallengeTimeout = 30 * time.Second

func CmdRussianRoulette(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	if len(args) < 2 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.InfoEmbed("🔫 Russian Roulette", "Usage: `!roulette @user <amount>`\n\nChallenge another user to a game of Russian Roulette. Winner takes all!"))
		return
	}

	amount, err := strconv.Atoi(args[len(args)-1])
	if err != nil || amount <= 0 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Invalid amount. Use a positive number."))
		return
	}

	if amount < 50 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed(fmt.Sprintf("Minimum bet is 50 %s", config.Bot.CurrencySymbol)))
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

	challengerBalance := database.GetBalance(challengerID)
	if challengerBalance < amount {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed(fmt.Sprintf("Insufficient balance! You have %d %s", challengerBalance, config.Bot.CurrencySymbol)))
		return
	}

	pendingMu.Lock()
	if _, exists := pendingChallenges[challengedID]; exists {
		pendingMu.Unlock()
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("This user already has a pending challenge!"))
		return
	}

	challengerInGame := isPlayerInGame(challengerID)
	challengedInGame := isPlayerInGame(challengedID)
	pendingMu.Unlock()

	if challengerInGame {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("You are already in a Russian Roulette game!"))
		return
	}
	if challengedInGame {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("This user is already in a Russian Roulette game!"))
		return
	}

	challenge := &RussianRouletteChallenge{
		ChallengerID: challengerID,
		ChallengedID: challengedID,
		Bet:          amount,
		ChannelID:    m.ChannelID,
	}

	challenge.TimeoutTimer = time.AfterFunc(ChallengeTimeout, func() {
		expireChallenge(s, challengedID)
	})

	pendingMu.Lock()
	pendingChallenges[challengedID] = challenge
	pendingMu.Unlock()

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

	s.ChannelMessageSendComplex(m.ChannelID, &discordgo.MessageSend{
		Embeds:     []*discordgo.MessageEmbed{embed},
		Components: buttons,
	})
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
	pendingMu.Unlock()

	if !exists {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "❌ No pending challenge found!",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	challengedBalance := database.GetBalance(challenge.ChallengedID)
	if challengedBalance < challenge.Bet {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Content:    fmt.Sprintf("❌ <@%s> does not have enough balance!", challenge.ChallengedID),
				Embeds:     []*discordgo.MessageEmbed{},
				Components: []discordgo.MessageComponent{},
			},
		})
		cleanupChallenge(challenge.ChallengedID)
		return
	}

	challengerBalance := database.GetBalance(challenge.ChallengerID)
	if challengerBalance < challenge.Bet {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Content:    fmt.Sprintf("❌ <@%s> no longer has enough balance!", challenge.ChallengerID),
				Embeds:     []*discordgo.MessageEmbed{},
				Components: []discordgo.MessageComponent{},
			},
		})
		cleanupChallenge(challenge.ChallengedID)
		return
	}

	challenge.TimeoutTimer.Stop()
	cleanupChallenge(challenge.ChallengedID)

	database.AddCoins(challenge.ChallengerID, -challenge.Bet)
	database.AddCoins(challenge.ChallengedID, -challenge.Bet)

	totalPot := challenge.Bet * 2

	challengerMember, _ := s.GuildMember(i.GuildID, challenge.ChallengerID)
	challengedMember, _ := s.GuildMember(i.GuildID, challenge.ChallengedID)

	challengerName := challengerMember.User.Username
	challengedName := challengedMember.User.Username

	game := &RussianRouletteGame{
		Player1ID:   challenge.ChallengerID,
		Player2ID:   challenge.ChallengedID,
		Player1Name: challengerName,
		Player2Name: challengedName,
		CurrentTurn: challenge.ChallengerID,
		Bet:         challenge.Bet,
		ChannelID:   i.ChannelID,
		Round:       1,
		Chamber:     rand.Intn(6) + 1,
		CurrentShot: 1,
		GameOver:    false,
	}

	if rand.Intn(2) == 1 {
		game.CurrentTurn = challenge.ChallengedID
	}

	gameID := fmt.Sprintf("%s_%s", challenge.ChallengerID, challenge.ChallengedID)

	rouletteMu.Lock()
	activeRouletteGames[gameID] = game
	rouletteMu.Unlock()

	embed := game.createGameEmbed(totalPot)
	components := game.createShootButton()

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
		},
	})

	msg, _ := s.InteractionResponse(i.Interaction)
	if msg != nil {
		game.mu.Lock()
		game.MessageID = msg.ID
		game.mu.Unlock()
	}
}

func handleDecline(s *discordgo.Session, i *discordgo.InteractionCreate, userID string) {
	pendingMu.Lock()
	challenge, exists := pendingChallenges[userID]
	pendingMu.Unlock()

	if !exists {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "❌ No pending challenge found!",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	challenge.TimeoutTimer.Stop()
	cleanupChallenge(challenge.ChallengedID)

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content:    fmt.Sprintf("❌ <@%s> declined the challenge!", userID),
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

	rouletteMu.Lock()
	game, exists := activeRouletteGames[gameID]
	if !exists {
		gameID = parts[3] + "_" + parts[2]
		game, exists = activeRouletteGames[gameID]
	}
	rouletteMu.Unlock()

	if !exists {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "❌ Game not found!",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	game.mu.Lock()
	defer game.mu.Unlock()

	if i.Member.User.ID != game.CurrentTurn {
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
		return
	}

	died := game.CurrentShot == game.Chamber
	totalPot := game.Bet * 2

	if died {
		game.GameOver = true
		winnerID := game.getOtherPlayer(game.CurrentTurn)

		database.AddCoins(winnerID, totalPot)

		embed := &discordgo.MessageEmbed{
			Title:       "🔫 Russian Roulette - GAME OVER",
			Description: fmt.Sprintf("💥 **POW!** <@%s> pulled the trigger and... **DIED!**", game.CurrentTurn),
			Color:       0x8B0000,
			Fields: []*discordgo.MessageEmbedField{
				{
					Name:   "🏆 Winner",
					Value:  fmt.Sprintf("<@%s>", winnerID),
					Inline: true,
				},
				{
					Name:   "💰 Prize",
					Value:  fmt.Sprintf("%d %s", totalPot, config.Bot.CurrencySymbol),
					Inline: true,
				},
				{
					Name:   "🎲 Details",
					Value:  fmt.Sprintf("Round: %d | Shot position: %d/6", game.Round, game.CurrentShot),
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

		rouletteMu.Lock()
		delete(activeRouletteGames, gameID)
		rouletteMu.Unlock()

	} else {
		survivor := game.CurrentTurn
		nextPlayer := game.getOtherPlayer(survivor)

		embed := &discordgo.MessageEmbed{
			Title:       "🔫 Russian Roulette",
			Description: fmt.Sprintf("😅 **CLICK!** <@%s> pulled the trigger and... survived!", survivor),
			Color:       0x00FF00,
			Fields: []*discordgo.MessageEmbedField{
				{
					Name:   "🎲 Result",
					Value:  fmt.Sprintf("Chamber %d was empty!", game.CurrentShot),
					Inline: false,
				},
				{
					Name:   "💰 Total Pot",
					Value:  fmt.Sprintf("%d %s", totalPot, config.Bot.CurrencySymbol),
					Inline: true,
				},
				{
					Name:   "🔄 Next",
					Value:  fmt.Sprintf("<@%s>'s turn", nextPlayer),
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

		time.Sleep(2 * time.Second)

		game.CurrentShot++
		game.CurrentTurn = nextPlayer
		game.Round++

		if game.CurrentShot > 6 {
			game.CurrentShot = 1
			game.Chamber = rand.Intn(6) + 1
		}

		newEmbed := game.createGameEmbed(totalPot)
		components := game.createShootButton()

		s.ChannelMessageEditComplex(&discordgo.MessageEdit{
			Channel:    i.ChannelID,
			ID:         i.Message.ID,
			Embeds:     &[]*discordgo.MessageEmbed{newEmbed},
			Components: &components,
		})
	}
}

func (g *RussianRouletteGame) createGameEmbed(totalPot int) *discordgo.MessageEmbed {
	currentPlayerName := g.Player1Name
	if g.CurrentTurn == g.Player2ID {
		currentPlayerName = g.Player2Name
	}

	return &discordgo.MessageEmbed{
		Title:       "🔫 Russian Roulette",
		Description: fmt.Sprintf("It's **%s**'s turn!\nClick the button to pull the trigger...", currentPlayerName),
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
				Name:   "💰 Prize",
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
			Text: fmt.Sprintf("Survivor takes %d %s!", totalPot, config.Bot.CurrencySymbol),
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
		return " ⬅️ (Your turn)"
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
	pendingMu.Unlock()

	s.ChannelMessageSend(challenge.ChannelID, fmt.Sprintf("⏰ <@%s> did not respond to <@%s>'s challenge in time! Challenge expired.", challengedID, challenge.ChallengerID))
}

func cleanupChallenge(challengedID string) {
	pendingMu.Lock()
	delete(pendingChallenges, challengedID)
	pendingMu.Unlock()
}

func isPlayerInGame(playerID string) bool {
	rouletteMu.Lock()
	defer rouletteMu.Unlock()

	for gameID := range activeRouletteGames {
		if strings.Contains(gameID, playerID) {
			return true
		}
	}
	return false
}

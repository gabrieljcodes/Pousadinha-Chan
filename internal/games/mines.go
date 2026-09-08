package games

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"estudocoin/internal/database"
	"estudocoin/pkg/config"
	"estudocoin/pkg/utils"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

const (
	MinesTotalTiles   = 20
	MinesDefaultCount = 3
	MinesMinCount     = 1
	MinesMaxCount     = 19
	MinesMinBet       = 10
	MinesRTP          = 0.99
	MinesInactivity   = 2 * time.Minute
)

type TileType int

const (
	TileDiamond TileType = iota
	TileMine
)

type MinesGame struct {
	UserID            string
	Bet               int
	MinesCount        int
	TotalTiles        int
	Board             []TileType
	Revealed          []bool
	MinePositions     []int
	PicksCount        int
	CurrentMultiplier float64
	NextMultiplier    float64
	Status            string // "playing", "cashout", "exploded", "cleared", "timeout_cashout", "timeout_refund"
	ExplodedTile      int
	ServerSeed        string
	ServerHash        string
	MessageID         string
	ChannelID         string
	Timer             *time.Timer
	mu                sync.Mutex
}

var (
	activeMinesGames = make(map[string]*MinesGame)
	minesMu          sync.RWMutex
)

// CalculateMinesMultiplier calculates the fair casino multiplier with RTP applied
// Formula: Multiplier(k) = RTP * Product_{i=0}^{k-1} (Total - i) / (Total - Mines - i)
func CalculateMinesMultiplier(totalTiles, minesCount, picksCount int) float64 {
	if picksCount <= 0 || totalTiles <= minesCount || picksCount > totalTiles-minesCount {
		return 1.0
	}
	mult := MinesRTP
	for i := 0; i < picksCount; i++ {
		mult *= float64(totalTiles-i) / float64(totalTiles-minesCount-i)
	}
	// Round to 2 decimal places
	return math.Round(mult*100) / 100.0
}

// generateMinesBoard creates a randomized board using Fisher-Yates with crypto/rand
func generateMinesBoard(totalTiles, minesCount int) ([]TileType, []int) {
	perm := make([]int, totalTiles)
	for i := 0; i < totalTiles; i++ {
		perm[i] = i
	}

	for i := totalTiles - 1; i > 0; i-- {
		var b [4]byte
		_, _ = rand.Read(b[:])
		j := int(binary.LittleEndian.Uint32(b[:]) % uint32(i+1))
		perm[i], perm[j] = perm[j], perm[i]
	}

	board := make([]TileType, totalTiles)
	minePositions := make([]int, minesCount)
	copy(minePositions, perm[:minesCount])
	sort.Ints(minePositions)

	for _, pos := range minePositions {
		board[pos] = TileMine
	}

	return board, minePositions
}

// generateProvablyFairSeed generates a random cryptographic seed and a SHA-256 commitment hash
func generateProvablyFairSeed(minePositions []int) (string, string) {
	var b [16]byte
	_, _ = rand.Read(b[:])
	seed := hex.EncodeToString(b[:])

	hashInput := fmt.Sprintf("%s:%v", seed, minePositions)
	hashBytes := sha256.Sum256([]byte(hashInput))
	hash := hex.EncodeToString(hashBytes[:])
	return seed, hash
}

// StartMinesInteraction starts a Mines game from a Discord slash command
func StartMinesInteraction(s *discordgo.Session, i *discordgo.InteractionCreate, bet int, minesCount int) {
	userID := i.Member.User.ID
	if minesCount < MinesMinCount || minesCount > MinesMaxCount {
		minesCount = MinesDefaultCount
	}

	if bet < MinesMinBet {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Embeds: []*discordgo.MessageEmbed{utils.ErrorEmbed(fmt.Sprintf("A aposta mínima é de %d %s", MinesMinBet, config.Bot.CurrencySymbol))},
				Flags:  discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	balance := database.GetBalance(userID)
	if balance < bet {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Embeds: []*discordgo.MessageEmbed{utils.ErrorEmbed(fmt.Sprintf("Saldo insuficiente! Você possui %d %s", balance, config.Bot.CurrencySymbol))},
				Flags:  discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	minesMu.Lock()
	if _, exists := activeMinesGames[userID]; exists {
		minesMu.Unlock()
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Embeds: []*discordgo.MessageEmbed{utils.ErrorEmbed("Você já tem uma partida de Mines ativa! Finalize-a antes de iniciar outra.")},
				Flags:  discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	if !RegisterActivePlayer(userID) {
		minesMu.Unlock()
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Embeds: []*discordgo.MessageEmbed{utils.ErrorEmbed("Você já está participando de outro jogo no momento!")},
				Flags:  discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	if err := database.CollectLostBet(userID, bet); err != nil {
		minesMu.Unlock()
		UnregisterActivePlayer(userID)
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Embeds: []*discordgo.MessageEmbed{utils.ErrorEmbed("Erro ao debitar a aposta.")},
				Flags:  discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	board, minePositions := generateMinesBoard(MinesTotalTiles, minesCount)
	seed, hash := generateProvablyFairSeed(minePositions)

	game := &MinesGame{
		UserID:            userID,
		Bet:               bet,
		MinesCount:        minesCount,
		TotalTiles:        MinesTotalTiles,
		Board:             board,
		Revealed:          make([]bool, MinesTotalTiles),
		MinePositions:     minePositions,
		PicksCount:        0,
		CurrentMultiplier: 1.0,
		NextMultiplier:    CalculateMinesMultiplier(MinesTotalTiles, minesCount, 1),
		Status:            "playing",
		ExplodedTile:      -1,
		ServerSeed:        seed,
		ServerHash:        hash,
		ChannelID:         i.ChannelID,
	}

	activeMinesGames[userID] = game
	minesMu.Unlock()

	game.Timer = time.AfterFunc(MinesInactivity, func() {
		game.handleInactivity(s)
	})

	embed := game.createMinesEmbed(false)
	components := game.createMinesComponents(false)

	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
		},
	})

	if err != nil {
		game.stopTimer()
		minesMu.Lock()
		delete(activeMinesGames, userID)
		minesMu.Unlock()
		UnregisterActivePlayer(userID)
		_ = database.AddCoins(userID, bet)
	}
}

// StartMinesText starts a Mines game from a Discord text command
func StartMinesText(s *discordgo.Session, m *discordgo.MessageCreate, bet int, minesCount int) {
	userID := m.Author.ID
	if minesCount < MinesMinCount || minesCount > MinesMaxCount {
		minesCount = MinesDefaultCount
	}

	if bet < MinesMinBet {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed(fmt.Sprintf("A aposta mínima é de %d %s", MinesMinBet, config.Bot.CurrencySymbol)))
		return
	}

	balance := database.GetBalance(userID)
	if balance < bet {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed(fmt.Sprintf("Saldo insuficiente! Você possui %d %s", balance, config.Bot.CurrencySymbol)))
		return
	}

	minesMu.Lock()
	if _, exists := activeMinesGames[userID]; exists {
		minesMu.Unlock()
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Você já tem uma partida de Mines ativa! Finalize-a antes de iniciar outra."))
		return
	}

	if !RegisterActivePlayer(userID) {
		minesMu.Unlock()
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Você já está participando de outro jogo no momento!"))
		return
	}

	if err := database.CollectLostBet(userID, bet); err != nil {
		minesMu.Unlock()
		UnregisterActivePlayer(userID)
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Erro ao debitar a aposta."))
		return
	}

	board, minePositions := generateMinesBoard(MinesTotalTiles, minesCount)
	seed, hash := generateProvablyFairSeed(minePositions)

	game := &MinesGame{
		UserID:            userID,
		Bet:               bet,
		MinesCount:        minesCount,
		TotalTiles:        MinesTotalTiles,
		Board:             board,
		Revealed:          make([]bool, MinesTotalTiles),
		MinePositions:     minePositions,
		PicksCount:        0,
		CurrentMultiplier: 1.0,
		NextMultiplier:    CalculateMinesMultiplier(MinesTotalTiles, minesCount, 1),
		Status:            "playing",
		ExplodedTile:      -1,
		ServerSeed:        seed,
		ServerHash:        hash,
		ChannelID:         m.ChannelID,
	}

	activeMinesGames[userID] = game
	minesMu.Unlock()

	game.Timer = time.AfterFunc(MinesInactivity, func() {
		game.handleInactivity(s)
	})

	embed := game.createMinesEmbed(false)
	components := game.createMinesComponents(false)

	msg, err := s.ChannelMessageSendComplex(m.ChannelID, &discordgo.MessageSend{
		Embeds:     []*discordgo.MessageEmbed{embed},
		Components: components,
	})

	if err != nil {
		game.stopTimer()
		minesMu.Lock()
		delete(activeMinesGames, userID)
		minesMu.Unlock()
		UnregisterActivePlayer(userID)
		_ = database.AddCoins(userID, bet)
		return
	}

	game.mu.Lock()
	game.MessageID = msg.ID
	game.mu.Unlock()
}

// HandleMinesInteraction processes button clicks on the Mines board
func HandleMinesInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	customID := i.MessageComponentData().CustomID
	parts := strings.Split(customID, "_")
	if len(parts) < 2 {
		return
	}

	action := parts[1] // "pick", "cashout", "restart"

	// Restart action: mines_restart_<bet>_<mines>
	if action == "restart" {
		if len(parts) < 4 {
			return
		}
		bet, err1 := strconv.Atoi(parts[2])
		minesCount, err2 := strconv.Atoi(parts[3])
		if err1 != nil || err2 != nil {
			return
		}
		StartMinesInteraction(s, i, bet, minesCount)
		return
	}

	clickerID := i.Member.User.ID

	// Determine game owner
	var targetUserID string
	if action == "cashout" && len(parts) >= 3 {
		targetUserID = parts[2]
	} else if action == "pick" && len(parts) >= 4 {
		targetUserID = parts[3]
	}

	if targetUserID != "" && clickerID != targetUserID {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "❌ Esta partida de Mines pertence a outro jogador!",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	minesMu.RLock()
	game, exists := activeMinesGames[clickerID]
	minesMu.RUnlock()

	if !exists {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "❌ Nenhuma partida ativa encontrada ou a partida já foi encerrada.",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	game.mu.Lock()
	defer game.mu.Unlock()

	if game.Status != "playing" {
		return
	}

	switch action {
	case "cashout":
		if game.PicksCount == 0 {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: "⚠️ Você precisa revelar pelo menos 1 diamante para realizar o cashout!",
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})
			return
		}

		game.stopTimer()
		game.Status = "cashout"
		winnings := int(math.Round(float64(game.Bet) * game.CurrentMultiplier))
		_ = database.AddCoins(game.UserID, winnings)

		minesMu.Lock()
		delete(activeMinesGames, game.UserID)
		minesMu.Unlock()
		UnregisterActivePlayer(game.UserID)

		embed := game.createMinesEmbed(true)
		components := game.createMinesComponents(true)

		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Embeds:     []*discordgo.MessageEmbed{embed},
				Components: components,
			},
		})

	case "pick":
		if len(parts) < 3 {
			return
		}
		tilePos, err := strconv.Atoi(parts[2])
		if err != nil || tilePos < 0 || tilePos >= game.TotalTiles || game.Revealed[tilePos] {
			return
		}

		game.Revealed[tilePos] = true
		game.resetTimer(s)

		if game.Board[tilePos] == TileMine {
			// BOOM! Hit a mine
			game.stopTimer()
			game.Status = "exploded"
			game.ExplodedTile = tilePos

			minesMu.Lock()
			delete(activeMinesGames, game.UserID)
			minesMu.Unlock()
			UnregisterActivePlayer(game.UserID)

			embed := game.createMinesEmbed(true)
			components := game.createMinesComponents(true)

			_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseUpdateMessage,
				Data: &discordgo.InteractionResponseData{
					Embeds:     []*discordgo.MessageEmbed{embed},
					Components: components,
				},
			})
			return
		}

		// Hit a Diamond!
		game.PicksCount++
		game.CurrentMultiplier = CalculateMinesMultiplier(game.TotalTiles, game.MinesCount, game.PicksCount)
		maxPicks := game.TotalTiles - game.MinesCount

		if game.PicksCount >= maxPicks {
			// Cleared the whole board!
			game.stopTimer()
			game.Status = "cleared"
			winnings := int(math.Round(float64(game.Bet) * game.CurrentMultiplier))
			_ = database.AddCoins(game.UserID, winnings)

			minesMu.Lock()
			delete(activeMinesGames, game.UserID)
			minesMu.Unlock()
			UnregisterActivePlayer(game.UserID)

			embed := game.createMinesEmbed(true)
			components := game.createMinesComponents(true)

			_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseUpdateMessage,
				Data: &discordgo.InteractionResponseData{
					Embeds:     []*discordgo.MessageEmbed{embed},
					Components: components,
				},
			})
			return
		}

		// Continue game
		game.NextMultiplier = CalculateMinesMultiplier(game.TotalTiles, game.MinesCount, game.PicksCount+1)
		embed := game.createMinesEmbed(false)
		components := game.createMinesComponents(false)

		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Embeds:     []*discordgo.MessageEmbed{embed},
				Components: components,
			},
		})
	}
}

func (g *MinesGame) stopTimer() {
	if g.Timer != nil {
		g.Timer.Stop()
	}
}

func (g *MinesGame) resetTimer(s *discordgo.Session) {
	g.stopTimer()
	g.Timer = time.AfterFunc(MinesInactivity, func() {
		g.handleInactivity(s)
	})
}

func (g *MinesGame) handleInactivity(s *discordgo.Session) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.Status != "playing" {
		return
	}

	minesMu.Lock()
	delete(activeMinesGames, g.UserID)
	minesMu.Unlock()
	UnregisterActivePlayer(g.UserID)

	if g.PicksCount > 0 {
		// Auto-cashout protection
		g.Status = "timeout_cashout"
		winnings := int(math.Round(float64(g.Bet) * g.CurrentMultiplier))
		_ = database.AddCoins(g.UserID, winnings)
	} else {
		// Refund
		g.Status = "timeout_refund"
		_ = database.AddCoins(g.UserID, g.Bet)
	}

	embed := g.createMinesEmbed(true)
	components := g.createMinesComponents(true)

	if g.MessageID != "" && g.ChannelID != "" {
		_, _ = s.ChannelMessageEditComplex(&discordgo.MessageEdit{
			Channel:    g.ChannelID,
			ID:         g.MessageID,
			Embeds:     &[]*discordgo.MessageEmbed{embed},
			Components: &components,
		})
	}
}

// createMinesEmbed generates the rich embed for Mines
func (g *MinesGame) createMinesEmbed(showAll bool) *discordgo.MessageEmbed {
	embedColor := 0x2b2d31 // Dark theme
	statusTitle := "💣 Mines - Campo Minado"
	statusDesc := "Encontre as gemas e evite as bombas! Faça cashout a qualquer momento."

	currentPayout := int(math.Round(float64(g.Bet) * g.CurrentMultiplier))
	currentProfit := currentPayout - g.Bet

	switch g.Status {
	case "cashout":
		embedColor = 0x2ecc71 // Emerald Green
		statusTitle = "💰 Mines - CASHOUT COM SUCESSO!"
		statusDesc = fmt.Sprintf("Parabéns! Você retirou com multiplicador de **%.2fx**, lucrando **+%d %s**!", g.CurrentMultiplier, currentProfit, config.Bot.CurrencySymbol)
	case "timeout_cashout":
		embedColor = 0x2ecc71
		statusTitle = "⏰ Mines - Auto-Cashout por Inatividade"
		statusDesc = fmt.Sprintf("Tempo limite atingido! Seu lucro de **+%d %s** (%.2fx) foi salvo automaticamente.", currentProfit, config.Bot.CurrencySymbol, g.CurrentMultiplier)
	case "timeout_refund":
		embedColor = 0x95a5a6
		statusTitle = "⏰ Mines - Cancelado por Inatividade"
		statusDesc = fmt.Sprintf("Tempo limite atingido sem jogadas. Sua aposta de **%d %s** foi reembolsada.", g.Bet, config.Bot.CurrencySymbol)
	case "exploded":
		embedColor = 0xe74c3c // Crimson Red
		statusTitle = "💥 Mines - BUST! VOCÊ EXPLODIU!"
		statusDesc = fmt.Sprintf("Você encontrou uma bomba na casa **#%d** e perdeu **%d %s**.", g.ExplodedTile+1, g.Bet, config.Bot.CurrencySymbol)
	case "cleared":
		embedColor = 0xf1c40f // Gold
		statusTitle = "🏆 Mines - TABULEIRO LIMPO COM PERFEIÇÃO!"
		statusDesc = fmt.Sprintf("INCRÍVEL! Você encontrou todos os **%d diamantes** com multiplicador de **%.2fx**, faturando **%d %s**!",
			g.TotalTiles-g.MinesCount, g.CurrentMultiplier, currentPayout, config.Bot.CurrencySymbol)
	}

	remainingDiamonds := (g.TotalTiles - g.MinesCount) - g.PicksCount
	if remainingDiamonds < 0 {
		remainingDiamonds = 0
	}

	fields := []*discordgo.MessageEmbedField{
		{
			Name:   "👤 Jogador",
			Value:  fmt.Sprintf("<@%s>", g.UserID),
			Inline: true,
		},
		{
			Name:   "💵 Aposta Inicial",
			Value:  fmt.Sprintf("**%d %s**", g.Bet, config.Bot.CurrencySymbol),
			Inline: true,
		},
		{
			Name:   "💣 Minas / Diamantes",
			Value:  fmt.Sprintf("**%d 💣** / **%d 💎**", g.MinesCount, g.TotalTiles-g.MinesCount),
			Inline: true,
		},
	}

	if g.Status == "playing" {
		fields = append(fields,
			&discordgo.MessageEmbedField{
				Name:   "📈 Multiplicador Atual",
				Value:  fmt.Sprintf("**%.2fx** (+%d %s)", g.CurrentMultiplier, currentProfit, config.Bot.CurrencySymbol),
				Inline: true,
			},
			&discordgo.MessageEmbedField{
				Name:   "🚀 Próximo Passo",
				Value:  fmt.Sprintf("**%.2fx** (+%d %s)", g.NextMultiplier, int(math.Round(float64(g.Bet)*g.NextMultiplier))-g.Bet, config.Bot.CurrencySymbol),
				Inline: true,
			},
			&discordgo.MessageEmbedField{
				Name:   "💎 Restantes",
				Value:  fmt.Sprintf("**%d** diamantes", remainingDiamonds),
				Inline: true,
			},
		)
	} else if g.Status == "cashout" || g.Status == "cleared" || g.Status == "timeout_cashout" {
		fields = append(fields,
			&discordgo.MessageEmbedField{
				Name:   "💸 Retorno Total",
				Value:  fmt.Sprintf("**%d %s** (Lucro: +%d %s)", currentPayout, config.Bot.CurrencySymbol, currentProfit, config.Bot.CurrencySymbol),
				Inline: true,
			},
			&discordgo.MessageEmbedField{
				Name:   "🎯 Gemas Coletadas",
				Value:  fmt.Sprintf("**%d** / %d", g.PicksCount, g.TotalTiles-g.MinesCount),
				Inline: true,
			},
		)
	}

	footerText := fmt.Sprintf("🔒 Provably Fair Hash: %s", g.ServerHash)
	if showAll && g.ServerSeed != "" {
		footerText = fmt.Sprintf("🔑 Seed: %s | Minas: %v", g.ServerSeed, g.MinePositions)
	}

	return &discordgo.MessageEmbed{
		Title:       statusTitle,
		Description: statusDesc,
		Color:       embedColor,
		Fields:      fields,
		Footer: &discordgo.MessageEmbedFooter{
			Text: footerText,
		},
		Timestamp: time.Now().Format(time.RFC3339),
	}
}

// createMinesComponents builds the 5x4 board grid (Rows 0-3) and the control row (Row 4)
func (g *MinesGame) createMinesComponents(showAll bool) []discordgo.MessageComponent {
	rows := make([]discordgo.MessageComponent, 0, 5)

	// Rows 0 to 3: 4 rows of 5 buttons each (Total 20 tiles)
	for r := 0; r < 4; r++ {
		actionRow := discordgo.ActionsRow{
			Components: make([]discordgo.MessageComponent, 0, 5),
		}

		for c := 0; c < 5; c++ {
			tilePos := r*5 + c
			button := discordgo.Button{
				CustomID: fmt.Sprintf("mines_pick_%d_%s", tilePos, g.UserID),
				Style:    discordgo.SecondaryButton,
				Label:    "",
			}

			if !showAll {
				// Game is active
				if g.Revealed[tilePos] {
					button.Emoji = &discordgo.ComponentEmoji{Name: "💎"}
					button.Style = discordgo.SuccessButton
					button.Disabled = true
					button.CustomID = fmt.Sprintf("mines_revealed_%d", tilePos)
				} else {
					button.Emoji = &discordgo.ComponentEmoji{Name: "⬛"}
					button.Disabled = false
				}
			} else {
				// Game ended - reveal all tiles
				button.Disabled = true
				if tilePos == g.ExplodedTile {
					button.Emoji = &discordgo.ComponentEmoji{Name: "💥"}
					button.Style = discordgo.DangerButton
				} else if g.Board[tilePos] == TileMine {
					button.Emoji = &discordgo.ComponentEmoji{Name: "💣"}
					button.Style = discordgo.DangerButton
				} else {
					button.Emoji = &discordgo.ComponentEmoji{Name: "💎"}
					if g.Revealed[tilePos] {
						button.Style = discordgo.SuccessButton
					} else {
						button.Style = discordgo.SecondaryButton
					}
				}
			}

			actionRow.Components = append(actionRow.Components, button)
		}

		rows = append(rows, actionRow)
	}

	// Row 4: Controls (Cashout button + Stats / Play Again)
	controlRow := discordgo.ActionsRow{
		Components: make([]discordgo.MessageComponent, 0, 3),
	}

	if !showAll {
		// Active game controls
		currentPayout := int(math.Round(float64(g.Bet) * g.CurrentMultiplier))
		cashoutLabel := "💰 Sacar"
		if g.PicksCount > 0 {
			cashoutLabel = fmt.Sprintf("💰 Sacar (%.2fx • %d %s)", g.CurrentMultiplier, currentPayout, config.Bot.CurrencySymbol)
		}

		controlRow.Components = append(controlRow.Components,
			discordgo.Button{
				CustomID: fmt.Sprintf("mines_cashout_%s", g.UserID),
				Label:    cashoutLabel,
				Style:    discordgo.SuccessButton,
				Disabled: g.PicksCount == 0,
			},
			discordgo.Button{
				CustomID: "mines_info_count",
				Label:    fmt.Sprintf("💣 %d Minas", g.MinesCount),
				Style:    discordgo.SecondaryButton,
				Disabled: true,
			},
			discordgo.Button{
				CustomID: "mines_info_diamonds",
				Label:    fmt.Sprintf("💎 %d/%d", g.PicksCount, g.TotalTiles-g.MinesCount),
				Style:    discordgo.SecondaryButton,
				Disabled: true,
			},
		)
	} else {
		// Game Over controls
		controlRow.Components = append(controlRow.Components,
			discordgo.Button{
				CustomID: fmt.Sprintf("mines_restart_%d_%d", g.Bet, g.MinesCount),
				Label:    "🔄 Jogar Novamente",
				Style:    discordgo.PrimaryButton,
			},
			discordgo.Button{
				CustomID: "mines_end_info",
				Label:    fmt.Sprintf("💣 %d Minas", g.MinesCount),
				Style:    discordgo.SecondaryButton,
				Disabled: true,
			},
		)
	}

	rows = append(rows, controlRow)
	return rows
}

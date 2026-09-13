package games

import (
	"bot/internal/database"
	"bot/internal/locale"
	"bot/pkg/config"
	"bot/pkg/utils"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
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
	GuildID           string
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
	if i.GuildID == "" {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Embeds: []*discordgo.MessageEmbed{utils.ErrorEmbed(locale.Text("games.mines.this_game_can_only_be_played_in"))},
				Flags:  discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}
	guildID := i.GuildID
	userID := i.Member.User.ID
	if minesCount < MinesMinCount || minesCount > MinesMaxCount {
		minesCount = MinesDefaultCount
	}

	if bet < MinesMinBet {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Embeds: []*discordgo.MessageEmbed{utils.ErrorEmbed(locale.Text("games.mines.the_minimum_bet_is.formatted", locale.Data{"MinesMinBet": MinesMinBet, "CurrencySymbol": config.Bot.CurrencySymbol}))},
				Flags:  discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	balance := database.GetBalance(guildID, userID)
	if balance < bet {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Embeds: []*discordgo.MessageEmbed{utils.ErrorEmbed(locale.Text("games.mines.insufficient_funds_you_have.formatted", locale.Data{"Balance": balance, "CurrencySymbol": config.Bot.CurrencySymbol}))},
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
				Embeds: []*discordgo.MessageEmbed{utils.ErrorEmbed(locale.Text("games.mines.you_already_have_an_active_mines_game"))},
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
				Embeds: []*discordgo.MessageEmbed{utils.ErrorEmbed(locale.Text("games.mines.you_are_already_playing_another_game"))},
				Flags:  discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	if err := database.CollectLostBet(guildID, userID, bet); err != nil {
		minesMu.Unlock()
		UnregisterActivePlayer(userID)
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Embeds: []*discordgo.MessageEmbed{utils.ErrorEmbed(locale.Text("games.mines.unable_to_debit_your_bet"))},
				Flags:  discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	board, minePositions := generateMinesBoard(MinesTotalTiles, minesCount)
	seed, hash := generateProvablyFairSeed(minePositions)

	game := &MinesGame{
		GuildID:           guildID,
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
		_ = database.AddCoins(guildID, userID, bet)
	}
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
				Content: locale.Text("games.mines.this_mines_game_belongs_to_another_player"),
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
				Content: locale.Text("games.mines.no_active_game_found_it_may_have"),
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
					Content: locale.Text("games.mines.reveal_at_least_one_gem_before_cashing"),
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})
			return
		}

		game.stopTimer()
		game.Status = "cashout"
		winnings := int(math.Round(float64(game.Bet) * game.CurrentMultiplier))
		_ = database.AddCoins(game.GuildID, game.UserID, winnings)

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
			_ = database.AddCoins(game.GuildID, game.UserID, winnings)

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
		_ = database.AddCoins(g.GuildID, g.UserID, winnings)
	} else {
		// Refund
		g.Status = "timeout_refund"
		_ = database.AddCoins(g.GuildID, g.UserID, g.Bet)
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
	statusTitle := locale.Text("games.mines.mines")
	statusDesc := locale.Text("games.mines.find_gems_and_avoid_mines_cash_out")

	currentPayout := int(math.Round(float64(g.Bet) * g.CurrentMultiplier))
	currentProfit := currentPayout - g.Bet

	switch g.Status {
	case "cashout":
		embedColor = 0x2ecc71 // Emerald Green
		statusTitle = locale.Text("games.mines.mines_cashed_out")
		statusDesc = locale.Text("games.mines.you_cashed_out_at_x_with_a.formatted", locale.Data{"CurrentMultiplier": g.CurrentMultiplier, "CurrentProfit": currentProfit, "CurrencySymbol": config.Bot.CurrencySymbol})
	case "timeout_cashout":
		embedColor = 0x2ecc71
		statusTitle = locale.Text("games.mines.mines_automatic_cash_out")
		statusDesc = locale.Text("games.mines.time_expired_your_profit_of_x_was.formatted", locale.Data{"CurrentProfit": currentProfit, "CurrencySymbol": config.Bot.CurrencySymbol, "CurrentMultiplier": g.CurrentMultiplier})
	case "timeout_refund":
		embedColor = 0x95a5a6
		statusTitle = locale.Text("games.mines.mines_cancelled_due_to_inactivity")
		statusDesc = locale.Text("games.mines.time_expired_without_a_move_your_bet.formatted", locale.Data{"Bet": g.Bet, "CurrencySymbol": config.Bot.CurrencySymbol})
	case "exploded":
		embedColor = 0xe74c3c // Crimson Red
		statusTitle = locale.Text("games.mines.mines_you_hit_a_mine")
		statusDesc = locale.Text("games.mines.you_hit_a_mine_on_tile_and.formatted", locale.Data{"ExplodedTile": g.ExplodedTile + 1, "Bet": g.Bet, "CurrencySymbol": config.Bot.CurrencySymbol})
	case "cleared":
		embedColor = 0xf1c40f // Gold
		statusTitle = locale.Text("games.mines.mines_board_cleared")
		statusDesc = locale.Text("games.mines.you_found_all_gems_at_x_earning.formatted", locale.Data{"TotalTiles": g.TotalTiles - g.MinesCount, "CurrentMultiplier": g.CurrentMultiplier, "CurrentPayout": currentPayout, "CurrencySymbol": config.Bot.CurrencySymbol})
	}

	remainingDiamonds := (g.TotalTiles - g.MinesCount) - g.PicksCount
	if remainingDiamonds < 0 {
		remainingDiamonds = 0
	}

	fields := []*discordgo.MessageEmbedField{
		{
			Name:   locale.Text("games.mines.player"),
			Value:  fmt.Sprintf("<@%s>", g.UserID),
			Inline: true,
		},
		{
			Name:   locale.Text("games.mines.initial_bet"),
			Value:  fmt.Sprintf("**%d %s**", g.Bet, config.Bot.CurrencySymbol),
			Inline: true,
		},
		{
			Name:   locale.Text("games.mines.mines_gems"),
			Value:  fmt.Sprintf("**%d 💣** / **%d 💎**", g.MinesCount, g.TotalTiles-g.MinesCount),
			Inline: true,
		},
	}

	if g.Status == "playing" {
		fields = append(fields,
			&discordgo.MessageEmbedField{
				Name:   locale.Text("games.mines.current_multiplier"),
				Value:  fmt.Sprintf("**%.2fx** (+%d %s)", g.CurrentMultiplier, currentProfit, config.Bot.CurrencySymbol),
				Inline: true,
			},
			&discordgo.MessageEmbedField{
				Name:   locale.Text("games.mines.next_step"),
				Value:  fmt.Sprintf("**%.2fx** (+%d %s)", g.NextMultiplier, int(math.Round(float64(g.Bet)*g.NextMultiplier))-g.Bet, config.Bot.CurrencySymbol),
				Inline: true,
			},
			&discordgo.MessageEmbedField{
				Name:   locale.Text("games.mines.remaining"),
				Value:  locale.Text("games.mines.gems.formatted", locale.Data{"RemainingDiamonds": remainingDiamonds}),
				Inline: true,
			},
		)
	} else if g.Status == "cashout" || g.Status == "cleared" || g.Status == "timeout_cashout" {
		fields = append(fields,
			&discordgo.MessageEmbedField{
				Name:   locale.Text("games.mines.total_return"),
				Value:  locale.Text("games.mines.profit.formatted", locale.Data{"CurrentPayout": currentPayout, "CurrencySymbol": config.Bot.CurrencySymbol, "CurrentProfit": currentProfit, "CurrencySymbol4": config.Bot.CurrencySymbol}),
				Inline: true,
			},
			&discordgo.MessageEmbedField{
				Name:   locale.Text("games.mines.gems_collected"),
				Value:  fmt.Sprintf("**%d** / %d", g.PicksCount, g.TotalTiles-g.MinesCount),
				Inline: true,
			},
		)
	}

	footerText := locale.Text("games.mines.provably_fair_hash.formatted", locale.Data{"ServerHash": g.ServerHash})
	if showAll && g.ServerSeed != "" {
		footerText = locale.Text("games.mines.seed_mines.formatted", locale.Data{"ServerSeed": g.ServerSeed, "MinePositions": g.MinePositions})
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
		cashoutLabel := locale.Text("games.cups.cash_out")
		if g.PicksCount > 0 {
			cashoutLabel = locale.Text("games.mines.cash_out_x.formatted", locale.Data{"CurrentMultiplier": g.CurrentMultiplier, "CurrentPayout": currentPayout, "CurrencySymbol": config.Bot.CurrencySymbol})
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
				Label:    locale.Text("games.mines.mines_f37846.formatted", locale.Data{"MinesCount": g.MinesCount}),
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
				Label:    locale.Text("games.mines.play_again"),
				Style:    discordgo.PrimaryButton,
			},
			discordgo.Button{
				CustomID: "mines_end_info",
				Label:    locale.Text("games.mines.mines_f37846.formatted", locale.Data{"MinesCount": g.MinesCount}),
				Style:    discordgo.SecondaryButton,
				Disabled: true,
			},
		)
	}

	rows = append(rows, controlRow)
	return rows
}

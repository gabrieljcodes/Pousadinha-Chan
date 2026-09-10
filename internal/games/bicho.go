package games

import (
	"crypto/rand"
	"encoding/binary"
	"bot/internal/database"
	"bot/pkg/config"
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Animal represents an entry in the Jogo do Bicho table
type Animal struct {
	Number  int    `json:"number"`
	Name    string `json:"name"`
	PtName  string `json:"pt_name"`
	Emoji   string `json:"emoji"`
	Dezenas [4]int `json:"dezenas"`
}

// BichoAnimals contains the official 25 animals of Jogo do Bicho
var BichoAnimals = [25]Animal{
	{Number: 1, Name: "Ostrich", PtName: "Avestruz", Emoji: "🦤", Dezenas: [4]int{1, 2, 3, 4}},
	{Number: 2, Name: "Eagle", PtName: "Águia", Emoji: "🦅", Dezenas: [4]int{5, 6, 7, 8}},
	{Number: 3, Name: "Donkey", PtName: "Burro", Emoji: "🫏", Dezenas: [4]int{9, 10, 11, 12}},
	{Number: 4, Name: "Butterfly", PtName: "Borboleta", Emoji: "🦋", Dezenas: [4]int{13, 14, 15, 16}},
	{Number: 5, Name: "Dog", PtName: "Cachorro", Emoji: "🐶", Dezenas: [4]int{17, 18, 19, 20}},
	{Number: 6, Name: "Goat", PtName: "Cabra", Emoji: "🐐", Dezenas: [4]int{21, 22, 23, 24}},
	{Number: 7, Name: "Ram", PtName: "Carneiro", Emoji: "🐑", Dezenas: [4]int{25, 26, 27, 28}},
	{Number: 8, Name: "Camel", PtName: "Camelo", Emoji: "🐫", Dezenas: [4]int{29, 30, 31, 32}},
	{Number: 9, Name: "Snake", PtName: "Cobra", Emoji: "🐍", Dezenas: [4]int{33, 34, 35, 36}},
	{Number: 10, Name: "Rabbit", PtName: "Coelho", Emoji: "🐇", Dezenas: [4]int{37, 38, 39, 40}},
	{Number: 11, Name: "Horse", PtName: "Cavalo", Emoji: "🐴", Dezenas: [4]int{41, 42, 43, 44}},
	{Number: 12, Name: "Elephant", PtName: "Elefante", Emoji: "🐘", Dezenas: [4]int{45, 46, 47, 48}},
	{Number: 13, Name: "Rooster", PtName: "Galo", Emoji: "🐓", Dezenas: [4]int{49, 50, 51, 52}},
	{Number: 14, Name: "Cat", PtName: "Gato", Emoji: "🐱", Dezenas: [4]int{53, 54, 55, 56}},
	{Number: 15, Name: "Alligator", PtName: "Jacaré", Emoji: "🐊", Dezenas: [4]int{57, 58, 59, 60}},
	{Number: 16, Name: "Lion", PtName: "Leão", Emoji: "🦁", Dezenas: [4]int{61, 62, 63, 64}},
	{Number: 17, Name: "Monkey", PtName: "Macaco", Emoji: "🐒", Dezenas: [4]int{65, 66, 67, 68}},
	{Number: 18, Name: "Pig", PtName: "Porco", Emoji: "🐷", Dezenas: [4]int{69, 70, 71, 72}},
	{Number: 19, Name: "Peacock", PtName: "Pavão", Emoji: "🦚", Dezenas: [4]int{73, 74, 75, 76}},
	{Number: 20, Name: "Turkey", PtName: "Peru", Emoji: "🦃", Dezenas: [4]int{77, 78, 79, 80}},
	{Number: 21, Name: "Bull", PtName: "Touro", Emoji: "🐂", Dezenas: [4]int{81, 82, 83, 84}},
	{Number: 22, Name: "Tiger", PtName: "Tigre", Emoji: "🐯", Dezenas: [4]int{85, 86, 87, 88}},
	{Number: 23, Name: "Bear", PtName: "Urso", Emoji: "🐻", Dezenas: [4]int{89, 90, 91, 92}},
	{Number: 24, Name: "Deer", PtName: "Veado", Emoji: "🦌", Dezenas: [4]int{93, 94, 95, 96}},
	{Number: 25, Name: "Cow", PtName: "Vaca", Emoji: "🐄", Dezenas: [4]int{97, 98, 99, 0}},
}

// Multipliers for each bet modality
const (
	MultGrupoCabeca    = 18.0
	MultGrupoCercado   = 3.6
	MultDezenaCabeca   = 60.0
	MultDezenaCercado  = 12.0
	MultCentenaCabeca  = 600.0
	MultCentenaCercado = 120.0
	MultMilharCabeca   = 4000.0
	MultMilharCercado  = 800.0
	MultDuque          = 18.5
	MultTerno          = 130.0
)

// NormalizeString converts text to lowercase without accents
func NormalizeString(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	replacements := map[rune]rune{
		'á': 'a', 'à': 'a', 'ã': 'a', 'â': 'a', 'ä': 'a',
		'é': 'e', 'è': 'e', 'ê': 'e', 'ë': 'e',
		'í': 'i', 'ì': 'i', 'î': 'i', 'ï': 'i',
		'ó': 'o', 'ò': 'o', 'õ': 'o', 'ô': 'o', 'ö': 'o',
		'ú': 'u', 'ù': 'u', 'û': 'u', 'ü': 'u',
		'ç': 'c',
	}
	var b strings.Builder
	for _, r := range s {
		if rep, ok := replacements[r]; ok {
			b.WriteRune(rep)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// GetAnimalByDezena returns the Animal associated with a 2-digit number (0-99)
func GetAnimalByDezena(dezena int) Animal {
	dezena = ((dezena % 100) + 100) % 100
	if dezena == 0 {
		return BichoAnimals[24] // Cow (97, 98, 99, 00)
	}
	groupIndex := (dezena - 1) / 4
	if groupIndex >= 0 && groupIndex < 25 {
		return BichoAnimals[groupIndex]
	}
	return BichoAnimals[0]
}

// GetAnimalFromMilhar returns the Animal for a 4-digit milhar
func GetAnimalFromMilhar(milhar int) Animal {
	dezena := milhar % 100
	return GetAnimalByDezena(dezena)
}

// FindAnimal searches for an animal by group number (1-25) or name (English or Portuguese)
func FindAnimal(query string) (Animal, bool) {
	norm := NormalizeString(query)
	if norm == "" {
		return Animal{}, false
	}

	// Try numeric group number
	if num, err := strconv.Atoi(norm); err == nil {
		if num >= 1 && num <= 25 {
			return BichoAnimals[num-1], true
		}
	}

	// Try exact or prefix name match (English name or Portuguese name)
	for _, a := range BichoAnimals {
		aNorm := NormalizeString(a.Name)
		ptNorm := NormalizeString(a.PtName)
		if aNorm == norm || strings.HasPrefix(aNorm, norm) || ptNorm == norm || strings.HasPrefix(ptNorm, norm) {
			return a, true
		}
	}

	return Animal{}, false
}

// GenerateBichoDraw generates 5 cryptographically uniform prizes (0000 - 9999)
func GenerateBichoDraw() [5]int {
	var prizes [5]int
	for i := 0; i < 5; i++ {
		var b [4]byte
		_, _ = rand.Read(b[:])
		prizes[i] = int(binary.LittleEndian.Uint32(b[:]) % 10000)
	}
	return prizes
}

// BetEvaluationResult contains payout calculation details
type BetEvaluationResult struct {
	Won         bool
	Multiplier  float64
	Payout      int64
	Description string
}

// EvaluateBichoBet checks if a bet won against the drawn 5 prizes
func EvaluateBichoBet(bet *database.DBBichoBet, prizes [5]int) BetEvaluationResult {
	betType := strings.ToLower(bet.BetType)
	scope := strings.ToLower(bet.Scope)
	if scope == "" || scope == "head" || scope == "cabeca" || scope == "cabeça" {
		scope = "head"
	} else {
		scope = "board"
	}

	switch betType {
	case "group", "grupo":
		animal, ok := FindAnimal(bet.Target)
		if !ok {
			return BetEvaluationResult{Won: false, Description: "Invalid animal"}
		}

		if scope == "head" {
			firstAnimal := GetAnimalFromMilhar(prizes[0])
			if firstAnimal.Number == animal.Number {
				payout := int64(math.Round(float64(bet.Amount) * MultGrupoCabeca))
				return BetEvaluationResult{
					Won:         true,
					Multiplier:  MultGrupoCabeca,
					Payout:      payout,
					Description: fmt.Sprintf("Group %02d (%s %s) on the Head (1st Prize)", animal.Number, animal.Emoji, animal.Name),
				}
			}
		} else { // board (1st to 5th)
			matches := 0
			for _, p := range prizes {
				if GetAnimalFromMilhar(p).Number == animal.Number {
					matches++
				}
			}
			if matches > 0 {
				mult := float64(matches) * MultGrupoCercado
				payout := int64(math.Round(float64(bet.Amount) * mult))
				return BetEvaluationResult{
					Won:         true,
					Multiplier:  mult,
					Payout:      payout,
					Description: fmt.Sprintf("Group %02d (%s %s) drawn %dx on the Board", animal.Number, animal.Emoji, animal.Name, matches),
				}
			}
		}

	case "tens", "dezena":
		targetNum, err := strconv.Atoi(bet.Target)
		if err != nil || targetNum < 0 || targetNum > 99 {
			return BetEvaluationResult{Won: false, Description: "Invalid tens"}
		}

		if scope == "head" {
			if prizes[0]%100 == targetNum {
				payout := int64(math.Round(float64(bet.Amount) * MultDezenaCabeca))
				return BetEvaluationResult{
					Won:         true,
					Multiplier:  MultDezenaCabeca,
					Payout:      payout,
					Description: fmt.Sprintf("Tens %02d on the Head (1st Prize)", targetNum),
				}
			}
		} else {
			matches := 0
			for _, p := range prizes {
				if p%100 == targetNum {
					matches++
				}
			}
			if matches > 0 {
				mult := float64(matches) * MultDezenaCercado
				payout := int64(math.Round(float64(bet.Amount) * mult))
				return BetEvaluationResult{
					Won:         true,
					Multiplier:  mult,
					Payout:      payout,
					Description: fmt.Sprintf("Tens %02d drawn %dx on the Board", targetNum, matches),
				}
			}
		}

	case "hundreds", "centena":
		targetNum, err := strconv.Atoi(bet.Target)
		if err != nil || targetNum < 0 || targetNum > 999 {
			return BetEvaluationResult{Won: false, Description: "Invalid hundreds"}
		}

		if scope == "head" {
			if prizes[0]%1000 == targetNum {
				payout := int64(math.Round(float64(bet.Amount) * MultCentenaCabeca))
				return BetEvaluationResult{
					Won:         true,
					Multiplier:  MultCentenaCabeca,
					Payout:      payout,
					Description: fmt.Sprintf("Hundreds %03d on the Head (1st Prize)", targetNum),
				}
			}
		} else {
			matches := 0
			for _, p := range prizes {
				if p%1000 == targetNum {
					matches++
				}
			}
			if matches > 0 {
				mult := float64(matches) * MultCentenaCercado
				payout := int64(math.Round(float64(bet.Amount) * mult))
				return BetEvaluationResult{
					Won:         true,
					Multiplier:  mult,
					Payout:      payout,
					Description: fmt.Sprintf("Hundreds %03d drawn %dx on the Board", targetNum, matches),
				}
			}
		}

	case "thousands", "milhar":
		targetNum, err := strconv.Atoi(bet.Target)
		if err != nil || targetNum < 0 || targetNum > 9999 {
			return BetEvaluationResult{Won: false, Description: "Invalid thousands"}
		}

		if scope == "head" {
			if prizes[0] == targetNum {
				payout := int64(math.Round(float64(bet.Amount) * MultMilharCabeca))
				return BetEvaluationResult{
					Won:         true,
					Multiplier:  MultMilharCabeca,
					Payout:      payout,
					Description: fmt.Sprintf("Thousands %04d on the Head (1st Prize)", targetNum),
				}
			}
		} else {
			matches := 0
			for _, p := range prizes {
				if p == targetNum {
					matches++
				}
			}
			if matches > 0 {
				mult := float64(matches) * MultMilharCercado
				payout := int64(math.Round(float64(bet.Amount) * mult))
				return BetEvaluationResult{
					Won:         true,
					Multiplier:  mult,
					Payout:      payout,
					Description: fmt.Sprintf("Thousands %04d drawn %dx on the Board", targetNum, matches),
				}
			}
		}

	case "pair", "duque":
		parts := strings.Split(bet.Target, ",")
		if len(parts) < 2 {
			return BetEvaluationResult{Won: false, Description: "Invalid targets for Animal Pair"}
		}
		a1, ok1 := FindAnimal(parts[0])
		a2, ok2 := FindAnimal(parts[1])
		if !ok1 || !ok2 || a1.Number == a2.Number {
			return BetEvaluationResult{Won: false, Description: "Invalid animals for Animal Pair"}
		}

		hasA1, hasA2 := false, false
		for _, p := range prizes {
			an := GetAnimalFromMilhar(p)
			if an.Number == a1.Number {
				hasA1 = true
			}
			if an.Number == a2.Number {
				hasA2 = true
			}
		}

		if hasA1 && hasA2 {
			payout := int64(math.Round(float64(bet.Amount) * MultDuque))
			return BetEvaluationResult{
				Won:         true,
				Multiplier:  MultDuque,
				Payout:      payout,
				Description: fmt.Sprintf("Animal Pair (Duque): %s %s and %s %s!", a1.Emoji, a1.Name, a2.Emoji, a2.Name),
			}
		}

	case "trio", "terno":
		parts := strings.Split(bet.Target, ",")
		if len(parts) < 3 {
			return BetEvaluationResult{Won: false, Description: "Invalid targets for Animal Trio"}
		}
		a1, ok1 := FindAnimal(parts[0])
		a2, ok2 := FindAnimal(parts[1])
		a3, ok3 := FindAnimal(parts[2])
		if !ok1 || !ok2 || !ok3 || a1.Number == a2.Number || a1.Number == a3.Number || a2.Number == a3.Number {
			return BetEvaluationResult{Won: false, Description: "Invalid animals for Animal Trio"}
		}

		hasA1, hasA2, hasA3 := false, false, false
		for _, p := range prizes {
			an := GetAnimalFromMilhar(p)
			if an.Number == a1.Number {
				hasA1 = true
			}
			if an.Number == a2.Number {
				hasA2 = true
			}
			if an.Number == a3.Number {
				hasA3 = true
			}
		}

		if hasA1 && hasA2 && hasA3 {
			payout := int64(math.Round(float64(bet.Amount) * MultTerno))
			return BetEvaluationResult{
				Won:         true,
				Multiplier:  MultTerno,
				Payout:      payout,
				Description: fmt.Sprintf("Animal Trio (Terno): %s %s, %s %s and %s %s!", a1.Emoji, a1.Name, a2.Emoji, a2.Name, a3.Emoji, a3.Name),
			}
		}
	}

	return BetEvaluationResult{Won: false, Multiplier: 0, Payout: 0, Description: "Ticket not drawn"}
}

// CreateBichoRoundEmbed generates the Discord embed for an open betting round
func CreateBichoRoundEmbed(round *database.DBBichoRound, totalBets int, totalAmount int64, distinctUsers int) *discordgo.MessageEmbed {
	unixTime := round.DrawTime.Unix()

	return &discordgo.MessageEmbed{
		Title: fmt.Sprintf("🎫 Jogo do Bicho - Betting Open (Round #%d)", round.RoundNumber),
		Description: fmt.Sprintf(
			"Place your bets for today's official lottery draw!\n\n"+
				"⏰ **Scheduled Draw:** <t:%d:F> (<t:%d:R>)\n"+
				"💰 **Total Wagered:** `%d %s` across **%d ticket(s)** (%d bettors)\n\n"+
				"**How to Bet:**\n"+
				"• `!bicho bet monkey 100` *(Group on Head)*\n"+
				"• `!bicho bet group <animal> <amount> [board]`\n"+
				"• `!bicho bet tens <00-99> <amount>`\n"+
				"• `!bicho bet hundreds <000-999> <amount>`\n"+
				"• `!bicho bet thousands <0000-9999> <amount>`\n"+
				"• `!bicho bet pair <animal1> <animal2> <amount>`\n"+
				"• `!bicho bet trio <animal1> <animal2> <animal3> <amount>`\n\n"+
				"👇 *Or use the interactive buttons below:*",
			unixTime, unixTime, totalAmount, config.Bot.CurrencySymbol, totalBets, distinctUsers,
		),
		Color: 0x2ecc71, // Green
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("Round ID: %d • Daily provably fair lottery", round.ID),
		},
		Timestamp: time.Now().Format(time.RFC3339),
	}
}

// CreateBichoRoundComponents returns the interactive action rows for the round
func CreateBichoRoundComponents(roundID int64) []discordgo.MessageComponent {
	return []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					CustomID: fmt.Sprintf("bicho_btn_bet_%d", roundID),
					Label:    "Place Bet",
					Style:    discordgo.SuccessButton,
					Emoji:    &discordgo.ComponentEmoji{Name: "🎲"},
				},
				discordgo.Button{
					CustomID: fmt.Sprintf("bicho_btn_my_bets_%d", roundID),
					Label:    "My Bets",
					Style:    discordgo.PrimaryButton,
					Emoji:    &discordgo.ComponentEmoji{Name: "💼"},
				},
				discordgo.Button{
					CustomID: "bicho_btn_table",
					Label:    "Animal Table",
					Style:    discordgo.SecondaryButton,
					Emoji:    &discordgo.ComponentEmoji{Name: "📜"},
				},
			},
		},
	}
}

// CreateBichoResultEmbed generates the official lottery result embed
func CreateBichoResultEmbed(round *database.DBBichoRound, prizes [5]int, winnerLines []string, totalPayout int64) *discordgo.MessageEmbed {
	var prizeLines []string
	prizeLabels := []string{"1st Prize", "2nd Prize", "3rd Prize", "4th Prize", "5th Prize"}

	for i := 0; i < 5; i++ {
		p := prizes[i]
		an := GetAnimalFromMilhar(p)
		prizeLines = append(prizeLines, fmt.Sprintf(
			"**%s:** `%04d` ➔ %s **Group %02d** (%s)",
			prizeLabels[i], p, an.Emoji, an.Number, an.Name,
		))
	}

	winnersText := "No winning tickets in this round! Better luck next time!"
	if len(winnerLines) > 0 {
		if len(winnerLines) > 15 {
			winnersText = strings.Join(winnerLines[:15], "\n") + fmt.Sprintf("\n*... and %d more winners!*", len(winnerLines)-15)
		} else {
			winnersText = strings.Join(winnerLines, "\n")
		}
	}

	return &discordgo.MessageEmbed{
		Title: fmt.Sprintf("🎰 OFFICIAL DRAW RESULTS - Jogo do Bicho (Round #%d)", round.RoundNumber),
		Description: fmt.Sprintf(
			"════════════════════════════════════════\n"+
				"%s\n"+
				"════════════════════════════════════════\n\n"+
				"🏆 **Winners & Payouts:**\n%s\n\n"+
				"💰 **Total Prizes Distributed:** `%d %s`\n"+
				"✨ *The ticket office for the next round is now open!*",
			strings.Join(prizeLines, "\n"), winnersText, totalPayout, config.Bot.CurrencySymbol,
		),
		Color: 0xf1c40f, // Gold
		Footer: &discordgo.MessageEmbedFooter{
			Text: "Official lottery draw completed • Audited results",
		},
		Timestamp: time.Now().Format(time.RFC3339),
	}
}

// CreateBichoTableEmbed generates the 25 animals reference catalog
func CreateBichoTableEmbed() *discordgo.MessageEmbed {
	var col1, col2, col3 []string

	for i := 0; i < 25; i++ {
		a := BichoAnimals[i]
		d0 := fmt.Sprintf("%02d", a.Dezenas[0])
		d1 := fmt.Sprintf("%02d", a.Dezenas[1])
		d2 := fmt.Sprintf("%02d", a.Dezenas[2])
		d3 := fmt.Sprintf("%02d", a.Dezenas[3])
		if a.Dezenas[3] == 0 {
			d3 = "00"
		}

		line := fmt.Sprintf("%s **%02d %s**\n`%s, %s, %s, %s`", a.Emoji, a.Number, a.Name, d0, d1, d2, d3)
		if i < 9 {
			col1 = append(col1, line)
		} else if i < 17 {
			col2 = append(col2, line)
		} else {
			col3 = append(col3, line)
		}
	}

	return &discordgo.MessageEmbed{
		Title: "📜 Official Animal Lottery Table (25 Animals & 100 Tens)",
		Description: "Each animal represents a Group and 4 Tens. The group is determined by the **last two digits** of the drawn number.",
		Color:       0x3498db,
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Groups 01 to 09", Value: strings.Join(col1, "\n\n"), Inline: true},
			{Name: "Groups 10 to 17", Value: strings.Join(col2, "\n\n"), Inline: true},
			{Name: "Groups 18 to 25", Value: strings.Join(col3, "\n\n"), Inline: true},
			{
				Name: "💰 Prize Multipliers",
				Value: "• **Group:** `18x` on Head | `3.6x` on Board (1st to 5th)\n" +
					"• **Tens:** `60x` on Head | `12x` on Board\n" +
					"• **Hundreds:** `600x` on Head | `120x` on Board\n" +
					"• **Thousands:** `4,000x` on Head | `800x` on Board\n" +
					"• **Animal Pair (Duque):** `18.5x` | **Animal Trio (Terno):** `130x`",
				Inline: false,
			},
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: "Jogo do Bicho • Gamble responsibly",
		},
	}
}

// BichoManager manages scheduled daily draws and active rounds across all Discord servers
type BichoManager struct {
	session *discordgo.Session
	timers  map[string]*time.Timer
	mu      sync.Mutex
}

var (
	defaultBichoManager *BichoManager
	bichoManagerOnce    sync.Once
)

// GetBichoManager returns the singleton BichoManager
func GetBichoManager() *BichoManager {
	bichoManagerOnce.Do(func() {
		defaultBichoManager = &BichoManager{
			timers: make(map[string]*time.Timer),
		}
	})
	return defaultBichoManager
}

// StartBichoManager initializes rounds and background timers for all enabled guilds
func StartBichoManager(session *discordgo.Session) {
	bm := GetBichoManager()
	bm.session = session

	log.Println("[BichoManager] Starting Jogo do Bicho daily scheduler...")

	guilds, err := database.GetAllActiveBichoGuilds()
	if err != nil {
		log.Printf("[BichoManager] Error querying active guilds: %v", err)
		return
	}

	for _, g := range guilds {
		bm.ScheduleGuildRound(g)
	}
}

// CalculateNextDrawTime calculates the upcoming draw time for a given hour and minute
func CalculateNextDrawTime(hour, minute int) time.Time {
	now := time.Now()
	target := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())
	if !now.Before(target) {
		target = target.Add(24 * time.Hour)
	}
	return target
}

// ScheduleGuildRound ensures a round exists and arms the draw timer
func (bm *BichoManager) ScheduleGuildRound(settings *database.DBBichoSettings) {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	if !settings.Enabled || settings.ChannelID == "" {
		return
	}

	// Cancel existing timer if present
	if existing, ok := bm.timers[settings.GuildID]; ok && existing != nil {
		existing.Stop()
		delete(bm.timers, settings.GuildID)
	}

	// Find or create active round
	round, err := database.GetActiveBichoRound(settings.GuildID)
	if err != nil {
		log.Printf("[BichoManager] Error finding active round for guild %s: %v", settings.GuildID, err)
		return
	}

	nextDraw := CalculateNextDrawTime(settings.DrawHour, settings.DrawMinute)

	if round == nil {
		// Calculate next round number
		lastCompleted, _ := database.GetLastCompletedBichoRound(settings.GuildID)
		roundNumber := 1
		if lastCompleted != nil {
			roundNumber = lastCompleted.RoundNumber + 1
		}

		round, err = database.CreateBichoRound(settings.GuildID, settings.ChannelID, roundNumber, nextDraw)
		if err != nil {
			log.Printf("[BichoManager] Error creating round for guild %s: %v", settings.GuildID, err)
			return
		}

		// Post new round announcement
		bm.postNewRoundEmbed(round, settings)
	}

	timeRemaining := time.Until(round.DrawTime)
	if timeRemaining <= 0 {
		// Time passed while offline: trigger draw right away in a goroutine
		go bm.ExecuteDraw(settings.GuildID)
		return
	}

	log.Printf("[BichoManager] Guild %s round #%d scheduled for %s (in %v)",
		settings.GuildID, round.RoundNumber, round.DrawTime.Format("2006-01-02 15:04:05"), timeRemaining)

	guildID := settings.GuildID
	bm.timers[guildID] = time.AfterFunc(timeRemaining, func() {
		bm.ExecuteDraw(guildID)
	})
}

// postNewRoundEmbed creates and sends the initial round message to the channel
func (bm *BichoManager) postNewRoundEmbed(round *database.DBBichoRound, settings *database.DBBichoSettings) {
	if bm.session == nil || settings.ChannelID == "" {
		return
	}

	embed := CreateBichoRoundEmbed(round, 0, 0, 0)
	components := CreateBichoRoundComponents(round.ID)

	msg, err := bm.session.ChannelMessageSendComplex(settings.ChannelID, &discordgo.MessageSend{
		Embeds:     []*discordgo.MessageEmbed{embed},
		Components: components,
	})
	if err != nil {
		log.Printf("[BichoManager] Error posting round embed to channel %s: %v", settings.ChannelID, err)
		return
	}

	_ = database.UpdateBichoRoundMessageID(round.ID, msg.ID)
}

// ExecuteDraw draws prizes, determines winners, sends announcement, and sets up next day's round
func (bm *BichoManager) ExecuteDraw(guildID string) {
	settings, err := database.GetBichoSettings(guildID)
	if err != nil || settings == nil || settings.ChannelID == "" {
		log.Printf("[BichoManager] Cannot execute draw: missing settings for guild %s", guildID)
		return
	}

	round, err := database.GetActiveBichoRound(guildID)
	if err != nil || round == nil {
		log.Printf("[BichoManager] No active round found to draw for guild %s", guildID)
		return
	}

	log.Printf("[BichoManager] Executing draw for guild %s, round #%d...", guildID, round.RoundNumber)

	// 1. Generate provably fair prizes
	prizes := GenerateBichoDraw()

	// 2. Fetch all bets for this round
	bets, err := database.GetRoundBetsDB(round.ID)
	if err != nil {
		log.Printf("[BichoManager] Error fetching bets for round %d: %v", round.ID, err)
		return
	}

	winningPayouts := make(map[int64]int64)
	betUserMap := make(map[int64]string)
	var winnerLines []string
	var totalPayout int64

	for _, b := range bets {
		betUserMap[b.ID] = b.UserID
		res := EvaluateBichoBet(b, prizes)
		if res.Won {
			winningPayouts[b.ID] = res.Payout
			totalPayout += res.Payout
			winnerLines = append(winnerLines, fmt.Sprintf(
				"• <@%s> hit **%s**! Won **%d %s**!",
				b.UserID, res.Description, res.Payout, config.Bot.CurrencySymbol,
			))
		}
	}

	// 3. Resolve round in database atomically
	if err := database.ResolveBichoRoundDB(round.ID, prizes, winningPayouts, betUserMap); err != nil {
		log.Printf("[BichoManager] Error resolving round %d in DB: %v", round.ID, err)
		return
	}

	// 4. Post results embed to dedicated channel
	if bm.session != nil {
		resultEmbed := CreateBichoResultEmbed(round, prizes, winnerLines, totalPayout)
		_, _ = bm.session.ChannelMessageSendEmbed(settings.ChannelID, resultEmbed)
	}

	// 5. Schedule next round for the guild
	bm.ScheduleGuildRound(settings)
}

// TriggerManualDraw allows admins to trigger a draw on-demand
func (bm *BichoManager) TriggerManualDraw(guildID string) error {
	settings, err := database.GetBichoSettings(guildID)
	if err != nil || settings == nil || settings.ChannelID == "" {
		return fmt.Errorf("Jogo do Bicho channel is not configured")
	}

	round, err := database.GetActiveBichoRound(guildID)
	if err != nil || round == nil {
		return fmt.Errorf("no active round found to draw")
	}

	go bm.ExecuteDraw(guildID)
	return nil
}

// RefreshRoundEmbed updates the pinned round message with live betting stats
func (bm *BichoManager) RefreshRoundEmbed(guildID string) {
	settings, err := database.GetBichoSettings(guildID)
	if err != nil || settings == nil || settings.ChannelID == "" {
		return
	}

	round, err := database.GetActiveBichoRound(guildID)
	if err != nil || round == nil || round.MessageID == "" {
		return
	}

	totalBets, totalAmount, uniqueBettors, err := database.GetBichoRoundStats(round.ID)
	if err != nil {
		return
	}

	embed := CreateBichoRoundEmbed(round, totalBets, totalAmount, uniqueBettors)
	components := CreateBichoRoundComponents(round.ID)

	if bm.session != nil {
		_, _ = bm.session.ChannelMessageEditComplex(&discordgo.MessageEdit{
			Channel:    settings.ChannelID,
			ID:         round.MessageID,
			Embeds:     &[]*discordgo.MessageEmbed{embed},
			Components: &components,
		})
	}
}

// ValidatedBichoBet contains normalized values for placing a bet
type ValidatedBichoBet struct {
	Modality      string
	Scope         string
	Target        string
	Description   string
	PotentialMult string
}

// ValidateAndFormatBichoBet validates and normalizes user input for any betting modality
func ValidateAndFormatBichoBet(modalityRaw, targetRaw, scopeRaw string) (*ValidatedBichoBet, error) {
	mNorm := strings.ToLower(strings.TrimSpace(modalityRaw))
	var modality string
	switch mNorm {
	case "group", "grupo", "g", "bicho":
		modality = "group"
	case "tens", "dezena", "d", "ten", "dez":
		modality = "tens"
	case "hundreds", "centena", "c", "hundred", "cen":
		modality = "hundreds"
	case "thousands", "milhar", "m", "thousand", "mil":
		modality = "thousands"
	case "pair", "duque", "duquedegrupo", "duque_grupo", "dg", "duo":
		modality = "pair"
	case "trio", "terno", "ternodegrupo", "terno_grupo", "tg", "triple":
		modality = "trio"
	default:
		return nil, fmt.Errorf("invalid modality! Choose between: `group`, `tens`, `hundreds`, `thousands`, `pair`, or `trio`")
	}

	var scope string
	sNorm := strings.ToLower(strings.TrimSpace(scopeRaw))
	if modality == "pair" || modality == "trio" {
		scope = "board"
	} else {
		switch sNorm {
		case "head", "cabeca", "cabeça", "1", "1st", "1º", "primeiro", "cab", "head (1st prize)", "cabeca (1º prêmio)":
			scope = "head"
		case "board", "cercado", "cerca", "1-5", "1a5", "all", "todos", "cer", "board (1st to 5th)", "cercado (1º ao 5º prêmio)":
			scope = "board"
		case "":
			scope = "head" // default to head
		default:
			return nil, fmt.Errorf("invalid position! Choose between `head` (1st prize) or `board` (1st to 5th prizes)")
		}
	}

	trimmedTarget := strings.TrimSpace(targetRaw)
	if trimmedTarget == "" {
		return nil, fmt.Errorf("please specify a target (animal name, tens, hundreds, or thousands)")
	}

	switch modality {
	case "group":
		animal, ok := FindAnimal(trimmedTarget)
		if !ok {
			return nil, fmt.Errorf("invalid animal or group! Use the animal name (e.g. `Monkey`) or group number (01 to 25)")
		}
		desc := fmt.Sprintf("Group %02d (%s %s) on Head", animal.Number, animal.Emoji, animal.Name)
		mult := "18x"
		if scope == "board" {
			desc = fmt.Sprintf("Group %02d (%s %s) on Board", animal.Number, animal.Emoji, animal.Name)
			mult = "3.6x per match"
		}
		return &ValidatedBichoBet{
			Modality:      "group",
			Scope:         scope,
			Target:        animal.Name,
			Description:   desc,
			PotentialMult: mult,
		}, nil

	case "tens":
		num, err := strconv.Atoi(trimmedTarget)
		if err != nil || num < 0 || num > 99 {
			return nil, fmt.Errorf("invalid tens! Must be a number between `00` and `99`")
		}
		animal := GetAnimalByDezena(num)
		target := fmt.Sprintf("%02d", num)
		desc := fmt.Sprintf("Tens %02d (%s %s) on Head", num, animal.Emoji, animal.Name)
		mult := "60x"
		if scope == "board" {
			desc = fmt.Sprintf("Tens %02d (%s %s) on Board", num, animal.Emoji, animal.Name)
			mult = "12x per match"
		}
		return &ValidatedBichoBet{
			Modality:      "tens",
			Scope:         scope,
			Target:        target,
			Description:   desc,
			PotentialMult: mult,
		}, nil

	case "hundreds":
		num, err := strconv.Atoi(trimmedTarget)
		if err != nil || num < 0 || num > 999 {
			return nil, fmt.Errorf("invalid hundreds! Must be a number between `000` and `999`")
		}
		animal := GetAnimalByDezena(num % 100)
		target := fmt.Sprintf("%03d", num)
		desc := fmt.Sprintf("Hundreds %03d (%s %s) on Head", num, animal.Emoji, animal.Name)
		mult := "600x"
		if scope == "board" {
			desc = fmt.Sprintf("Hundreds %03d (%s %s) on Board", num, animal.Emoji, animal.Name)
			mult = "120x per match"
		}
		return &ValidatedBichoBet{
			Modality:      "hundreds",
			Scope:         scope,
			Target:        target,
			Description:   desc,
			PotentialMult: mult,
		}, nil

	case "thousands":
		num, err := strconv.Atoi(trimmedTarget)
		if err != nil || num < 0 || num > 9999 {
			return nil, fmt.Errorf("invalid thousands! Must be a number between `0000` and `9999`")
		}
		animal := GetAnimalByDezena(num % 100)
		target := fmt.Sprintf("%04d", num)
		desc := fmt.Sprintf("Thousands %04d (%s %s) on Head", num, animal.Emoji, animal.Name)
		mult := "4,000x"
		if scope == "board" {
			desc = fmt.Sprintf("Thousands %04d (%s %s) on Board", num, animal.Emoji, animal.Name)
			mult = "800x per match"
		}
		return &ValidatedBichoBet{
			Modality:      "thousands",
			Scope:         scope,
			Target:        target,
			Description:   desc,
			PotentialMult: mult,
		}, nil

	case "pair":
		tokens := strings.Fields(strings.ReplaceAll(trimmedTarget, ",", " "))
		if len(tokens) != 2 {
			return nil, fmt.Errorf("animal pair requires 2 animals or groups! E.g. `Monkey, Lion` or `17 16`")
		}
		a1, ok1 := FindAnimal(tokens[0])
		a2, ok2 := FindAnimal(tokens[1])
		if !ok1 || !ok2 {
			return nil, fmt.Errorf("one or more animals specified for the pair are invalid")
		}
		if a1.Number == a2.Number {
			return nil, fmt.Errorf("the two animals in a pair must be different")
		}
		return &ValidatedBichoBet{
			Modality:      "pair",
			Scope:         "board",
			Target:        fmt.Sprintf("%s,%s", a1.Name, a2.Name),
			Description:   fmt.Sprintf("Animal Pair: %s %s and %s %s", a1.Emoji, a1.Name, a2.Emoji, a2.Name),
			PotentialMult: "18.5x",
		}, nil

	case "trio":
		tokens := strings.Fields(strings.ReplaceAll(trimmedTarget, ",", " "))
		if len(tokens) != 3 {
			return nil, fmt.Errorf("animal trio requires 3 animals or groups! E.g. `Monkey, Lion, Tiger` or `17 16 22`")
		}
		a1, ok1 := FindAnimal(tokens[0])
		a2, ok2 := FindAnimal(tokens[1])
		a3, ok3 := FindAnimal(tokens[2])
		if !ok1 || !ok2 || !ok3 {
			return nil, fmt.Errorf("one or more animals specified for the trio are invalid")
		}
		if a1.Number == a2.Number || a1.Number == a3.Number || a2.Number == a3.Number {
			return nil, fmt.Errorf("the three animals in a trio must all be distinct")
		}
		return &ValidatedBichoBet{
			Modality:      "trio",
			Scope:         "board",
			Target:        fmt.Sprintf("%s,%s,%s", a1.Name, a2.Name, a3.Name),
			Description:   fmt.Sprintf("Animal Trio: %s %s, %s %s and %s %s", a1.Emoji, a1.Name, a2.Emoji, a2.Name, a3.Emoji, a3.Name),
			PotentialMult: "130x",
		}, nil
	}

	return nil, fmt.Errorf("unknown modality")
}

// PlaceBetResult details the confirmed bet placement
type PlaceBetResult struct {
	RoundID     int64
	RoundNumber int
	Modality    string
	Scope       string
	Target      string
	Amount      int64
	Description string
	Multiplier  string
	DrawTime    time.Time
}

// PlaceBet validates and executes a bet on the active round
func (bm *BichoManager) PlaceBet(guildID, userID, modalityRaw, targetRaw, scopeRaw string, amount int64) (*PlaceBetResult, error) {
	if amount <= 0 {
		return nil, fmt.Errorf("bet amount must be greater than zero")
	}

	settings, err := database.GetBichoSettings(guildID)
	if err != nil || settings == nil || !settings.Enabled || settings.ChannelID == "" {
		return nil, fmt.Errorf("Jogo do Bicho is not active or configured on this server. An administrator must use `!bicho config channel #channel`")
	}

	if amount < settings.MinBet {
		return nil, fmt.Errorf("the minimum bet amount configured is %d %s", settings.MinBet, config.Bot.CurrencySymbol)
	}

	val, err := ValidateAndFormatBichoBet(modalityRaw, targetRaw, scopeRaw)
	if err != nil {
		return nil, err
	}

	round, err := database.GetActiveBichoRound(guildID)
	if err != nil || round == nil {
		return nil, fmt.Errorf("no active round open at this time. Please wait for the next draw")
	}

	if err := database.PlaceBichoBetDB(round.ID, userID, guildID, val.Modality, val.Scope, val.Target, amount); err != nil {
		return nil, err
	}

	// Refresh round embed asynchronously
	go bm.RefreshRoundEmbed(guildID)

	return &PlaceBetResult{
		RoundID:     round.ID,
		RoundNumber: round.RoundNumber,
		Modality:    val.Modality,
		Scope:       val.Scope,
		Target:      val.Target,
		Amount:      amount,
		Description: val.Description,
		Multiplier:  val.PotentialMult,
		DrawTime:    round.DrawTime,
	}, nil
}


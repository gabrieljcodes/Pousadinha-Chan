package games

import (
	"crypto/rand"
	"encoding/binary"
	"estudocoin/internal/database"
	"estudocoin/pkg/config"
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
	Emoji   string `json:"emoji"`
	Dezenas [4]int `json:"dezenas"`
}

// BichoAnimals contains the official 25 animals of Jogo do Bicho
var BichoAnimals = [25]Animal{
	{Number: 1, Name: "Avestruz", Emoji: "🦤", Dezenas: [4]int{1, 2, 3, 4}},
	{Number: 2, Name: "Águia", Emoji: "🦅", Dezenas: [4]int{5, 6, 7, 8}},
	{Number: 3, Name: "Burro", Emoji: "🫏", Dezenas: [4]int{9, 10, 11, 12}},
	{Number: 4, Name: "Borboleta", Emoji: "🦋", Dezenas: [4]int{13, 14, 15, 16}},
	{Number: 5, Name: "Cachorro", Emoji: "🐶", Dezenas: [4]int{17, 18, 19, 20}},
	{Number: 6, Name: "Cabra", Emoji: "🐐", Dezenas: [4]int{21, 22, 23, 24}},
	{Number: 7, Name: "Carneiro", Emoji: "🐑", Dezenas: [4]int{25, 26, 27, 28}},
	{Number: 8, Name: "Camelo", Emoji: "🐫", Dezenas: [4]int{29, 30, 31, 32}},
	{Number: 9, Name: "Cobra", Emoji: "🐍", Dezenas: [4]int{33, 34, 35, 36}},
	{Number: 10, Name: "Coelho", Emoji: "🐇", Dezenas: [4]int{37, 38, 39, 40}},
	{Number: 11, Name: "Cavalo", Emoji: "🐴", Dezenas: [4]int{41, 42, 43, 44}},
	{Number: 12, Name: "Elefante", Emoji: "🐘", Dezenas: [4]int{45, 46, 47, 48}},
	{Number: 13, Name: "Galo", Emoji: "🐓", Dezenas: [4]int{49, 50, 51, 52}},
	{Number: 14, Name: "Gato", Emoji: "🐱", Dezenas: [4]int{53, 54, 55, 56}},
	{Number: 15, Name: "Jacaré", Emoji: "🐊", Dezenas: [4]int{57, 58, 59, 60}},
	{Number: 16, Name: "Leão", Emoji: "🦁", Dezenas: [4]int{61, 62, 63, 64}},
	{Number: 17, Name: "Macaco", Emoji: "🐒", Dezenas: [4]int{65, 66, 67, 68}},
	{Number: 18, Name: "Porco", Emoji: "🐷", Dezenas: [4]int{69, 70, 71, 72}},
	{Number: 19, Name: "Pavão", Emoji: "🦚", Dezenas: [4]int{73, 74, 75, 76}},
	{Number: 20, Name: "Peru", Emoji: "🦃", Dezenas: [4]int{77, 78, 79, 80}},
	{Number: 21, Name: "Touro", Emoji: "🐂", Dezenas: [4]int{81, 82, 83, 84}},
	{Number: 22, Name: "Tigre", Emoji: "🐯", Dezenas: [4]int{85, 86, 87, 88}},
	{Number: 23, Name: "Urso", Emoji: "🐻", Dezenas: [4]int{89, 90, 91, 92}},
	{Number: 24, Name: "Veado", Emoji: "🦌", Dezenas: [4]int{93, 94, 95, 96}},
	{Number: 25, Name: "Vaca", Emoji: "🐄", Dezenas: [4]int{97, 98, 99, 0}},
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
		return BichoAnimals[24] // Vaca (97, 98, 99, 00)
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

// FindAnimal searches for an animal by group number (1-25) or name
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

	// Try exact or prefix name match
	for _, a := range BichoAnimals {
		aNorm := NormalizeString(a.Name)
		if aNorm == norm || strings.HasPrefix(aNorm, norm) {
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
	if scope == "" {
		scope = "cabeca"
	}

	switch betType {
	case "grupo":
		animal, ok := FindAnimal(bet.Target)
		if !ok {
			return BetEvaluationResult{Won: false, Description: "Animal inválido"}
		}

		if scope == "cabeca" {
			firstAnimal := GetAnimalFromMilhar(prizes[0])
			if firstAnimal.Number == animal.Number {
				payout := int64(math.Round(float64(bet.Amount) * MultGrupoCabeca))
				return BetEvaluationResult{
					Won:         true,
					Multiplier:  MultGrupoCabeca,
					Payout:      payout,
					Description: fmt.Sprintf("Grupo %02d (%s %s) no 1º Prêmio (Cabeça)", animal.Number, animal.Emoji, animal.Name),
				}
			}
		} else { // cercado (1º ao 5º)
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
					Description: fmt.Sprintf("Grupo %02d (%s %s) sorteado %dx no Cercado", animal.Number, animal.Emoji, animal.Name, matches),
				}
			}
		}

	case "dezena":
		targetNum, err := strconv.Atoi(bet.Target)
		if err != nil || targetNum < 0 || targetNum > 99 {
			return BetEvaluationResult{Won: false, Description: "Dezena inválida"}
		}

		if scope == "cabeca" {
			if prizes[0]%100 == targetNum {
				payout := int64(math.Round(float64(bet.Amount) * MultDezenaCabeca))
				return BetEvaluationResult{
					Won:         true,
					Multiplier:  MultDezenaCabeca,
					Payout:      payout,
					Description: fmt.Sprintf("Dezena %02d no 1º Prêmio (Cabeça)", targetNum),
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
					Description: fmt.Sprintf("Dezena %02d sorteada %dx no Cercado", targetNum, matches),
				}
			}
		}

	case "centena":
		targetNum, err := strconv.Atoi(bet.Target)
		if err != nil || targetNum < 0 || targetNum > 999 {
			return BetEvaluationResult{Won: false, Description: "Centena inválida"}
		}

		if scope == "cabeca" {
			if prizes[0]%1000 == targetNum {
				payout := int64(math.Round(float64(bet.Amount) * MultCentenaCabeca))
				return BetEvaluationResult{
					Won:         true,
					Multiplier:  MultCentenaCabeca,
					Payout:      payout,
					Description: fmt.Sprintf("Centena %03d no 1º Prêmio (Cabeça)", targetNum),
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
					Description: fmt.Sprintf("Centena %03d sorteada %dx no Cercado", targetNum, matches),
				}
			}
		}

	case "milhar":
		targetNum, err := strconv.Atoi(bet.Target)
		if err != nil || targetNum < 0 || targetNum > 9999 {
			return BetEvaluationResult{Won: false, Description: "Milhar inválido"}
		}

		if scope == "cabeca" {
			if prizes[0] == targetNum {
				payout := int64(math.Round(float64(bet.Amount) * MultMilharCabeca))
				return BetEvaluationResult{
					Won:         true,
					Multiplier:  MultMilharCabeca,
					Payout:      payout,
					Description: fmt.Sprintf("Milhar %04d no 1º Prêmio (Cabeça)", targetNum),
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
					Description: fmt.Sprintf("Milhar %04d sorteado %dx no Cercado", targetNum, matches),
				}
			}
		}

	case "duque":
		parts := strings.Split(bet.Target, ",")
		if len(parts) < 2 {
			return BetEvaluationResult{Won: false, Description: "Alvos do Duque inválidos"}
		}
		a1, ok1 := FindAnimal(parts[0])
		a2, ok2 := FindAnimal(parts[1])
		if !ok1 || !ok2 || a1.Number == a2.Number {
			return BetEvaluationResult{Won: false, Description: "Animais do Duque inválidos"}
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
				Description: fmt.Sprintf("Duque de Grupo: %s %s e %s %s!", a1.Emoji, a1.Name, a2.Emoji, a2.Name),
			}
		}

	case "terno":
		parts := strings.Split(bet.Target, ",")
		if len(parts) < 3 {
			return BetEvaluationResult{Won: false, Description: "Alvos do Terno inválidos"}
		}
		a1, ok1 := FindAnimal(parts[0])
		a2, ok2 := FindAnimal(parts[1])
		a3, ok3 := FindAnimal(parts[2])
		if !ok1 || !ok2 || !ok3 || a1.Number == a2.Number || a1.Number == a3.Number || a2.Number == a3.Number {
			return BetEvaluationResult{Won: false, Description: "Animais do Terno inválidos"}
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
				Description: fmt.Sprintf("Terno de Grupo: %s %s, %s %s e %s %s!", a1.Emoji, a1.Name, a2.Emoji, a2.Name, a3.Emoji, a3.Name),
			}
		}
	}

	return BetEvaluationResult{Won: false, Multiplier: 0, Payout: 0, Description: "Palpite não sorteado"}
}

// CreateBichoRoundEmbed generates the Discord embed for an open betting round
func CreateBichoRoundEmbed(round *database.DBBichoRound, totalBets int, totalAmount int64, distinctUsers int) *discordgo.MessageEmbed {
	unixTime := round.DrawTime.Unix()

	return &discordgo.MessageEmbed{
		Title: fmt.Sprintf("🎫 Jogo do Bicho - Bilheteria Aberta (Rodada #%d)", round.RoundNumber),
		Description: fmt.Sprintf(
			"Faça seu palpite para o sorteio oficial de hoje!\n\n"+
				"⏰ **Sorteio Programado:** <t:%d:F> (<t:%d:R>)\n"+
				"💰 **Volume Acumulado:** `%d %s` em **%d bilhete(s)** (%d apostadores)\n\n"+
				"**Como Apostar:**\n"+
				"• `!bicho apostar macaco 100` *(Grupo na cabeça)*\n"+
				"• `!bicho apostar grupo <bicho> <valor> [cercado]`\n"+
				"• `!bicho apostar dezena <00-99> <valor>`\n"+
				"• `!bicho apostar centena <000-999> <valor>`\n"+
				"• `!bicho apostar milhar <0000-9999> <valor>`\n"+
				"• `!bicho apostar duque <bicho1> <bicho2> <valor>`\n"+
				"• `!bicho apostar terno <bicho1> <bicho2> <bicho3> <valor>`\n\n"+
				"👇 *Ou utilize os botões rápidos abaixo:*",
			unixTime, unixTime, totalAmount, config.Bot.CurrencySymbol, totalBets, distinctUsers,
		),
		Color: 0x2ecc71, // Green
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("Rodada ID: %d • Sorteio provably fair diário", round.ID),
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
					Label:    "Apostar no Bicho",
					Style:    discordgo.SuccessButton,
					Emoji:    &discordgo.ComponentEmoji{Name: "🎲"},
				},
				discordgo.Button{
					CustomID: fmt.Sprintf("bicho_btn_my_bets_%d", roundID),
					Label:    "Minhas Apostas",
					Style:    discordgo.PrimaryButton,
					Emoji:    &discordgo.ComponentEmoji{Name: "💼"},
				},
				discordgo.Button{
					CustomID: "bicho_btn_table",
					Label:    "Tabela dos 25 Bichos",
					Style:    discordgo.SecondaryButton,
					Emoji:    &discordgo.ComponentEmoji{Name: "📜"},
				},
			},
		},
	}
}

// CreateBichoResultEmbed generates the "Deu no Poste!" lottery result embed
func CreateBichoResultEmbed(round *database.DBBichoRound, prizes [5]int, winnerLines []string, totalPayout int64) *discordgo.MessageEmbed {
	var prizeLines []string
	prizeLabels := []string{"1º Prêmio", "2º Prêmio", "3º Prêmio", "4º Prêmio", "5º Prêmio"}

	for i := 0; i < 5; i++ {
		p := prizes[i]
		an := GetAnimalFromMilhar(p)
		prizeLines = append(prizeLines, fmt.Sprintf(
			"**%s:** `%04d` ➔ %s **Grupo %02d** (%s)",
			prizeLabels[i], p, an.Emoji, an.Number, an.Name,
		))
	}

	winnersText := "Nenhum apostador acertou nesta rodada! Amanhã tem mais!"
	if len(winnerLines) > 0 {
		if len(winnerLines) > 15 {
			winnersText = strings.Join(winnerLines[:15], "\n") + fmt.Sprintf("\n*... e mais %d ganhadores!*", len(winnerLines)-15)
		} else {
			winnersText = strings.Join(winnerLines, "\n")
		}
	}

	return &discordgo.MessageEmbed{
		Title: fmt.Sprintf("🎰 DEU NO POSTE! - Resultado do Jogo do Bicho (Rodada #%d)", round.RoundNumber),
		Description: fmt.Sprintf(
			"════════════════════════════════════════\n"+
				"%s\n"+
				"════════════════════════════════════════\n\n"+
				"🏆 **Premiação e Ganhadores:**\n%s\n\n"+
				"💰 **Total Distribuído em Prêmios:** `%d %s`\n"+
				"✨ *A bilheteria para a próxima rodada já está aberta!*",
			strings.Join(prizeLines, "\n"), winnersText, totalPayout, config.Bot.CurrencySymbol,
		),
		Color: 0xf1c40f, // Gold
		Footer: &discordgo.MessageEmbedFooter{
			Text: "Sorteio oficial concluído • Resultados auditados",
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
		Title: "📜 Tabela Oficial dos 25 Bichos e Dezenas",
		Description: "Cada animal representa um Grupo e 4 Dezenas. O grupo é definido pelos **dois últimos dígitos** do número sorteado.",
		Color:       0x3498db,
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Grupos 01 ao 09", Value: strings.Join(col1, "\n\n"), Inline: true},
			{Name: "Grupos 10 ao 17", Value: strings.Join(col2, "\n\n"), Inline: true},
			{Name: "Grupos 18 ao 25", Value: strings.Join(col3, "\n\n"), Inline: true},
			{
				Name: "💰 Multiplicadores de Prêmios",
				Value: "• **Grupo:** `18x` na cabeça | `3.6x` cercado (1º ao 5º)\n" +
					"• **Dezena:** `60x` na cabeça | `12x` cercado\n" +
					"• **Centena:** `600x` na cabeça | `120x` cercado\n" +
					"• **Milhar:** `4.000x` na cabeça | `800x` cercado\n" +
					"• **Duque de Grupo:** `18.5x` | **Terno de Grupo:** `130x`",
				Inline: false,
			},
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: "Jogo do Bicho • Aposte com responsabilidade",
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
				"• <@%s> acertou **%s**! Ganhou **%d %s**!",
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
		return fmt.Errorf("canal do Jogo do Bicho não configurado")
	}

	round, err := database.GetActiveBichoRound(guildID)
	if err != nil || round == nil {
		return fmt.Errorf("nenhuma rodada ativa encontrada para sortear")
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
	case "grupo", "g", "bicho":
		modality = "grupo"
	case "dezena", "d", "dez":
		modality = "dezena"
	case "centena", "c", "cen":
		modality = "centena"
	case "milhar", "m", "mil":
		modality = "milhar"
	case "duque", "duquedegrupo", "duque_grupo", "dg":
		modality = "duque"
	case "terno", "ternodegrupo", "terno_grupo", "tg":
		modality = "terno"
	default:
		return nil, fmt.Errorf("modalidade inválida! Escolha entre: `grupo`, `dezena`, `centena`, `milhar`, `duque` ou `terno`")
	}

	var scope string
	sNorm := strings.ToLower(strings.TrimSpace(scopeRaw))
	if modality == "duque" || modality == "terno" {
		scope = "cercado"
	} else {
		switch sNorm {
		case "cabeca", "cabeça", "1", "1º", "primeiro", "cab", "cabeca (1º prêmio)":
			scope = "cabeca"
		case "cercado", "cerca", "1-5", "1a5", "todos", "cer", "cercado (1º ao 5º prêmio)":
			scope = "cercado"
		case "":
			scope = "cabeca" // default to cabeça
		default:
			return nil, fmt.Errorf("posição inválida! Escolha entre `cabeca` (1º prêmio) ou `cercado` (1º ao 5º prêmio)")
		}
	}

	trimmedTarget := strings.TrimSpace(targetRaw)
	if trimmedTarget == "" {
		return nil, fmt.Errorf("informe o alvo da aposta (nome do bicho, dezena, centena ou milhar)")
	}

	switch modality {
	case "grupo":
		animal, ok := FindAnimal(trimmedTarget)
		if !ok {
			return nil, fmt.Errorf("animal ou grupo inválido! Use o nome do bicho (ex: `Macaco`) ou o número do grupo (01 a 25)")
		}
		desc := fmt.Sprintf("Grupo %02d (%s %s) na Cabeça", animal.Number, animal.Emoji, animal.Name)
		mult := "18x"
		if scope == "cercado" {
			desc = fmt.Sprintf("Grupo %02d (%s %s) no Cercado", animal.Number, animal.Emoji, animal.Name)
			mult = "3.6x por acerto"
		}
		return &ValidatedBichoBet{
			Modality:      "grupo",
			Scope:         scope,
			Target:        animal.Name,
			Description:   desc,
			PotentialMult: mult,
		}, nil

	case "dezena":
		num, err := strconv.Atoi(trimmedTarget)
		if err != nil || num < 0 || num > 99 {
			return nil, fmt.Errorf("dezena inválida! Deve ser um número entre `00` e `99`")
		}
		animal := GetAnimalByDezena(num)
		target := fmt.Sprintf("%02d", num)
		desc := fmt.Sprintf("Dezena %02d (%s %s) na Cabeça", num, animal.Emoji, animal.Name)
		mult := "60x"
		if scope == "cercado" {
			desc = fmt.Sprintf("Dezena %02d (%s %s) no Cercado", num, animal.Emoji, animal.Name)
			mult = "12x por acerto"
		}
		return &ValidatedBichoBet{
			Modality:      "dezena",
			Scope:         scope,
			Target:        target,
			Description:   desc,
			PotentialMult: mult,
		}, nil

	case "centena":
		num, err := strconv.Atoi(trimmedTarget)
		if err != nil || num < 0 || num > 999 {
			return nil, fmt.Errorf("centena inválida! Deve ser um número entre `000` e `999`")
		}
		animal := GetAnimalByDezena(num % 100)
		target := fmt.Sprintf("%03d", num)
		desc := fmt.Sprintf("Centena %03d (%s %s) na Cabeça", num, animal.Emoji, animal.Name)
		mult := "600x"
		if scope == "cercado" {
			desc = fmt.Sprintf("Centena %03d (%s %s) no Cercado", num, animal.Emoji, animal.Name)
			mult = "120x por acerto"
		}
		return &ValidatedBichoBet{
			Modality:      "centena",
			Scope:         scope,
			Target:        target,
			Description:   desc,
			PotentialMult: mult,
		}, nil

	case "milhar":
		num, err := strconv.Atoi(trimmedTarget)
		if err != nil || num < 0 || num > 9999 {
			return nil, fmt.Errorf("milhar inválido! Deve ser um número entre `0000` e `9999`")
		}
		animal := GetAnimalByDezena(num % 100)
		target := fmt.Sprintf("%04d", num)
		desc := fmt.Sprintf("Milhar %04d (%s %s) na Cabeça", num, animal.Emoji, animal.Name)
		mult := "4.000x"
		if scope == "cercado" {
			desc = fmt.Sprintf("Milhar %04d (%s %s) no Cercado", num, animal.Emoji, animal.Name)
			mult = "800x por acerto"
		}
		return &ValidatedBichoBet{
			Modality:      "milhar",
			Scope:         scope,
			Target:        target,
			Description:   desc,
			PotentialMult: mult,
		}, nil

	case "duque":
		tokens := strings.Fields(strings.ReplaceAll(trimmedTarget, ",", " "))
		if len(tokens) != 2 {
			return nil, fmt.Errorf("duque de grupo requer 2 bichos ou grupos! Ex: `Macaco, Leao` ou `17 16`")
		}
		a1, ok1 := FindAnimal(tokens[0])
		a2, ok2 := FindAnimal(tokens[1])
		if !ok1 || !ok2 {
			return nil, fmt.Errorf("um ou mais bichos informados para o Duque são inválidos")
		}
		if a1.Number == a2.Number {
			return nil, fmt.Errorf("os dois bichos do Duque devem ser diferentes")
		}
		return &ValidatedBichoBet{
			Modality:      "duque",
			Scope:         "cercado",
			Target:        fmt.Sprintf("%s,%s", a1.Name, a2.Name),
			Description:   fmt.Sprintf("Duque: %s %s e %s %s", a1.Emoji, a1.Name, a2.Emoji, a2.Name),
			PotentialMult: "18.5x",
		}, nil

	case "terno":
		tokens := strings.Fields(strings.ReplaceAll(trimmedTarget, ",", " "))
		if len(tokens) != 3 {
			return nil, fmt.Errorf("terno de grupo requer 3 bichos ou grupos! Ex: `Macaco, Leao, Tigre` ou `17 16 22`")
		}
		a1, ok1 := FindAnimal(tokens[0])
		a2, ok2 := FindAnimal(tokens[1])
		a3, ok3 := FindAnimal(tokens[2])
		if !ok1 || !ok2 || !ok3 {
			return nil, fmt.Errorf("um ou mais bichos informados para o Terno são inválidos")
		}
		if a1.Number == a2.Number || a1.Number == a3.Number || a2.Number == a3.Number {
			return nil, fmt.Errorf("os três bichos do Terno devem ser diferentes entre si")
		}
		return &ValidatedBichoBet{
			Modality:      "terno",
			Scope:         "cercado",
			Target:        fmt.Sprintf("%s,%s,%s", a1.Name, a2.Name, a3.Name),
			Description:   fmt.Sprintf("Terno: %s %s, %s %s e %s %s", a1.Emoji, a1.Name, a2.Emoji, a2.Name, a3.Emoji, a3.Name),
			PotentialMult: "130x",
		}, nil
	}

	return nil, fmt.Errorf("modalidade desconhecida")
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
		return nil, fmt.Errorf("o valor da aposta deve ser maior que zero")
	}

	settings, err := database.GetBichoSettings(guildID)
	if err != nil || settings == nil || !settings.Enabled || settings.ChannelID == "" {
		return nil, fmt.Errorf("o Jogo do Bicho não está ativo ou configurado neste servidor. Um administrador deve usar `!bicho config canal #canal`")
	}

	if amount < settings.MinBet {
		return nil, fmt.Errorf("o valor mínimo de aposta configurado é de %d %s", settings.MinBet, config.Bot.CurrencySymbol)
	}

	val, err := ValidateAndFormatBichoBet(modalityRaw, targetRaw, scopeRaw)
	if err != nil {
		return nil, err
	}

	round, err := database.GetActiveBichoRound(guildID)
	if err != nil || round == nil {
		return nil, fmt.Errorf("nenhuma rodada aberta no momento. Aguarde o próximo sorteio")
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


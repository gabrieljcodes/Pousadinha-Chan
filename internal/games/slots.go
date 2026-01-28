package games

import (
	"estudocoin/internal/database"
	"estudocoin/pkg/config"
	"estudocoin/pkg/utils"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

const MinSlotsBet = 10

var (
	slotSymbols = []SlotSymbol{
		{Name: "cherry", Emoji: "🍒", Value: 2, Weight: 35},
		{Name: "lemon", Emoji: "🍋", Value: 3, Weight: 25},
		{Name: "orange", Emoji: "🍊", Value: 4, Weight: 20},
		{Name: "bell", Emoji: "🔔", Value: 6, Weight: 12},
		{Name: "diamond", Emoji: "💎", Value: 10, Weight: 6},
		{Name: "seven", Emoji: "7️⃣", Value: 25, Weight: 2},
	}

	activeSlotsGames = make(map[string]bool)
	slotsMu          sync.Mutex
)

type SlotSymbol struct {
	Name   string
	Emoji  string
	Value  int
	Weight int
}

type SlotsResult struct {
	Reel1       SlotSymbol
	Reel2       SlotSymbol
	Reel3       SlotSymbol
	WinAmount   int
	IsJackpot   bool
	IsTwoMatch  bool
	Multiplier  float64
}

func StartSlotsText(s *discordgo.Session, m *discordgo.MessageCreate, bet int) {
	startSlots(s, m.Author.ID, m.Author.Username, bet, m.ChannelID, func(embed *discordgo.MessageEmbed) {
		s.ChannelMessageSendEmbed(m.ChannelID, embed)
	})
}

func StartSlotsInteraction(s *discordgo.Session, i *discordgo.InteractionCreate, bet int) {
	startSlots(s, i.Member.User.ID, i.Member.User.Username, bet, i.ChannelID, func(embed *discordgo.MessageEmbed) {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Embeds: []*discordgo.MessageEmbed{embed},
			},
		})
	})
}

func startSlots(s *discordgo.Session, userID string, username string, bet int, channelID string, responder func(*discordgo.MessageEmbed)) {
	if bet < MinSlotsBet {
		s.ChannelMessageSend(channelID, fmt.Sprintf("❌ Minimum bet is %d %s", MinSlotsBet, config.Bot.CurrencySymbol))
		return
	}

	balance := database.GetBalance(userID)
	if balance < bet {
		s.ChannelMessageSend(channelID, fmt.Sprintf("❌ <@%s> Insufficient balance! You have %d %s", userID, balance, config.Bot.CurrencySymbol))
		return
	}

	slotsMu.Lock()
	if activeSlotsGames[userID] {
		slotsMu.Unlock()
		s.ChannelMessageSend(channelID, fmt.Sprintf("❌ <@%s> You already have an active slots game!", userID))
		return
	}
	activeSlotsGames[userID] = true
	slotsMu.Unlock()

	job := GameJob{
		UserID: userID,
		OnQueue: func(pos int) {
			s.ChannelMessageSend(channelID, fmt.Sprintf("⏳ <@%s> Queued for Slots (Pos: #%d)", userID, pos))
		},
		Run: func(finishChan chan struct{}) {
			defer close(finishChan)
			defer cleanupSlots(userID)

			if database.GetBalance(userID) < bet {
				s.ChannelMessageSend(channelID, fmt.Sprintf("❌ <@%s> You ran out of funds while waiting.", userID))
				return
			}

			database.RemoveCoins(userID, bet)

			result := spinSlots(bet)
			embed := createSlotsEmbed(username, bet, result)

			if result.WinAmount > 0 {
				database.AddCoins(userID, result.WinAmount)
			}

			responder(embed)
		},
	}

	Enqueue(job)
}

func spinSlots(bet int) SlotsResult {
	rand.Seed(time.Now().UnixNano())

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
		result.Multiplier = float64(r1.Value)
		result.WinAmount = int(float64(bet) * result.Multiplier)
	} else if r1.Name == r2.Name || r2.Name == r3.Name || r1.Name == r3.Name {
		result.IsTwoMatch = true
		var matchSymbol SlotSymbol
		if r1.Name == r2.Name {
			matchSymbol = r1
		} else if r2.Name == r3.Name {
			matchSymbol = r2
		} else {
			matchSymbol = r1
		}
		result.Multiplier = float64(matchSymbol.Value) * 0.3
		result.WinAmount = int(float64(bet) * result.Multiplier)
		if result.WinAmount < bet {
			result.WinAmount = bet
		}
	}

	return result
}

func getWeightedSymbol() SlotSymbol {
	totalWeight := 0
	for _, s := range slotSymbols {
		totalWeight += s.Weight
	}

	r := rand.Intn(totalWeight)
	cumulative := 0

	for _, s := range slotSymbols {
		cumulative += s.Weight
		if r < cumulative {
			return s
		}
	}

	return slotSymbols[0]
}

func createSlotsEmbed(username string, bet int, result SlotsResult) *discordgo.MessageEmbed {
	slotsDisplay := fmt.Sprintf("# %s | %s | %s", result.Reel1.Emoji, result.Reel2.Emoji, result.Reel3.Emoji)

	color := utils.ColorRed
	var title, description string

	if result.IsJackpot {
		color = utils.ColorGold
		title = "🎰 JACKPOT! 🎰"
		description = fmt.Sprintf("**%s** hit the jackpot!\n\n%s\n\n"+
			"**Bet:** %d %s\n"+
			"**Multiplier:** %.1fx\n"+
			"**Won:** %d %s 💰",
			username, slotsDisplay, bet, config.Bot.CurrencySymbol,
			result.Multiplier, result.WinAmount, config.Bot.CurrencySymbol)
	} else if result.IsTwoMatch {
		color = utils.ColorGreen
		title = "🎉 WINNER!"
		description = fmt.Sprintf("**%s** got a match!\n\n%s\n\n"+
			"**Bet:** %d %s\n"+
			"**Multiplier:** %.1fx\n"+
			"**Won:** %d %s",
			username, slotsDisplay, bet, config.Bot.CurrencySymbol,
			result.Multiplier, result.WinAmount, config.Bot.CurrencySymbol)
	} else {
		title = "🎰 Slots"
		description = fmt.Sprintf("**%s** spun the reels...\n\n%s\n\n"+
			"**Bet:** %d %s\n"+
			"💔 No match this time!",
			username, slotsDisplay, bet, config.Bot.CurrencySymbol)
	}

	return &discordgo.MessageEmbed{
		Title:       title,
		Description: description,
		Color:       color,
		Footer: &discordgo.MessageEmbedFooter{
			Text: "🍒🍋🍊 = Small | 🔔 = Medium | 💎 = High | 7️⃣ = JACKPOT!",
		},
	}
}

func cleanupSlots(userID string) {
	slotsMu.Lock()
	delete(activeSlotsGames, userID)
	slotsMu.Unlock()
}

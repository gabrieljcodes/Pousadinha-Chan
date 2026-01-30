package games

import (
	"estudocoin/internal/database"
	"estudocoin/pkg/config"
	"estudocoin/pkg/utils"
	"fmt"
	"math/rand"
	"strings"
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

	activeSlotsSessions = make(map[string]*SlotsSession)
	slotsMu             sync.Mutex
)

type SlotSymbol struct {
	Name   string
	Emoji  string
	Value  int
	Weight int
}

type SlotsSession struct {
	UserID    string
	Username  string
	Bet       int
	ChannelID string
	MessageID string
}

type SlotsResult struct {
	Reel1      SlotSymbol
	Reel2      SlotSymbol
	Reel3      SlotSymbol
	WinAmount  int
	IsJackpot  bool
	IsTwoMatch bool
	Multiplier float64
}

func StartSlotsText(s *discordgo.Session, m *discordgo.MessageCreate, bet int) {
	startSlots(s, m.Author.ID, m.Author.Username, bet, m.ChannelID, func(msg *discordgo.MessageSend) (*discordgo.Message, error) {
		return s.ChannelMessageSendComplex(m.ChannelID, msg)
	})
}

func StartSlotsInteraction(s *discordgo.Session, i *discordgo.InteractionCreate, bet int) {
	var msg *discordgo.Message
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds: []*discordgo.MessageEmbed{createInitialEmbed(i.Member.User.Username, bet)},
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.Button{
							Label:    "🎰 PULL LEVER",
							Style:    discordgo.PrimaryButton,
							CustomID: fmt.Sprintf("slots_spin_%s_%d", i.Member.User.ID, bet),
							Emoji:    &discordgo.ComponentEmoji{Name: "🎰"},
						},
					},
				},
			},
		},
	})
	if err == nil {
		msg, _ = s.InteractionResponse(i.Interaction)
		slotsMu.Lock()
		activeSlotsSessions[i.Member.User.ID] = &SlotsSession{
			UserID:    i.Member.User.ID,
			Username:  i.Member.User.Username,
			Bet:       bet,
			ChannelID: i.ChannelID,
			MessageID: msg.ID,
		}
		slotsMu.Unlock()
	}
}

func startSlots(s *discordgo.Session, userID string, username string, bet int, channelID string, sender func(*discordgo.MessageSend) (*discordgo.Message, error)) {
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
	if activeSlotsSessions[userID] != nil {
		slotsMu.Unlock()
		s.ChannelMessageSend(channelID, fmt.Sprintf("❌ <@%s> You already have an active slots game!", userID))
		return
	}
	slotsMu.Unlock()

	embed := createInitialEmbed(username, bet)
	buttons := []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Label:    "🎰 PULL LEVER",
					Style:    discordgo.PrimaryButton,
					CustomID: fmt.Sprintf("slots_spin_%s_%d", userID, bet),
					Emoji:    &discordgo.ComponentEmoji{Name: "🎰"},
				},
			},
		},
	}

	msg, err := sender(&discordgo.MessageSend{
		Embeds:     []*discordgo.MessageEmbed{embed},
		Components: buttons,
	})

	if err == nil && msg != nil {
		slotsMu.Lock()
		activeSlotsSessions[userID] = &SlotsSession{
			UserID:    userID,
			Username:  username,
			Bet:       bet,
			ChannelID: channelID,
			MessageID: msg.ID,
		}
		slotsMu.Unlock()
	}
}

func createInitialEmbed(username string, bet int) *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{
		Title:       "🎰 Slot Machine",
		Description: fmt.Sprintf("**%s** is ready to play!\n\n# ❓ | ❓ | ❓\n\n**Bet:** %d %s\n\n*Click the button to pull the lever!*", username, bet, config.Bot.CurrencySymbol),
		Color:       0x8B0000,
		Footer: &discordgo.MessageEmbedFooter{
			Text: "🍒🍋🍊 = Small | 🔔 = Medium | 💎 = High | 7️⃣ = JACKPOT!",
		},
	}
}

func HandleSlotsInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	customID := i.MessageComponentData().CustomID
	if !strings.HasPrefix(customID, "slots_spin_") {
		return
	}

	parts := strings.Split(customID, "_")
	if len(parts) < 4 {
		return
	}

	userID := parts[2]

	if i.Member.User.ID != userID {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "❌ This is not your game!",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	slotsMu.Lock()
	session := activeSlotsSessions[userID]
	if session == nil {
		slotsMu.Unlock()
		return
	}
	delete(activeSlotsSessions, userID)
	slotsMu.Unlock()

	balance := database.GetBalance(userID)
	if balance < session.Bet {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Content:    fmt.Sprintf("❌ <@%s> Insufficient balance!", userID),
				Embeds:     []*discordgo.MessageEmbed{},
				Components: []discordgo.MessageComponent{},
			},
		})
		return
	}

	database.CollectLostBet(userID, session.Bet)

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{createSpinningEmbed(session.Username, session.Bet)},
			Components: []discordgo.MessageComponent{},
		},
	})

	go runSlotAnimation(s, i.ChannelID, i.Message.ID, session)
}

func runSlotAnimation(s *discordgo.Session, channelID, messageID string, session *SlotsSession) {
	animationFrames := []string{
		"🍒 | 🍋 | 🍊",
		"🍋 | 🍊 | 🔔",
		"🍊 | 🔔 | 💎",
		"🔔 | 💎 | 7️⃣",
		"💎 | 7️⃣ | 🍒",
		"7️⃣ | 🍒 | 🍋",
		"🍒 | 🔔 | 🍊",
		"🍋 | 💎 | 🔔",
		"🍊 | 7️⃣ | 💎",
	}

	for _, frame := range animationFrames {
		embed := &discordgo.MessageEmbed{
			Title:       "🎰 Slot Machine",
			Description: fmt.Sprintf("**%s** is spinning...\n\n# %s\n\n**Bet:** %d %s", session.Username, frame, session.Bet, config.Bot.CurrencySymbol),
			Color:       0xFFD700,
		}

		s.ChannelMessageEditComplex(&discordgo.MessageEdit{
			Channel: channelID,
			ID:      messageID,
			Embeds:  &[]*discordgo.MessageEmbed{embed},
		})

		time.Sleep(200 * time.Millisecond)
	}

	result := spinSlots(session.Bet)

	if result.WinAmount > 0 {
		database.AddCoins(session.UserID, result.WinAmount)
	}

	finalEmbed := createResultEmbed(session.Username, session.Bet, result)
	s.ChannelMessageEditComplex(&discordgo.MessageEdit{
		Channel:    channelID,
		ID:         messageID,
		Embeds:     &[]*discordgo.MessageEmbed{finalEmbed},
		Components: &[]discordgo.MessageComponent{},
	})
}

func createSpinningEmbed(username string, bet int) *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{
		Title:       "🎰 Slot Machine",
		Description: fmt.Sprintf("**%s** is spinning...\n\n# 🎰 | 🎰 | 🎰\n\n**Bet:** %d %s", username, bet, config.Bot.CurrencySymbol),
		Color:       0xFFD700,
	}
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

func createResultEmbed(username string, bet int, result SlotsResult) *discordgo.MessageEmbed {
	slotsDisplay := fmt.Sprintf("# %s | %s | %s", result.Reel1.Emoji, result.Reel2.Emoji, result.Reel3.Emoji)

	var color int
	var title, description string

	if result.IsJackpot {
		color = utils.ColorGold
		title = "🎰💰 JACKPOT! 💰🎰"
		description = fmt.Sprintf("**%s** hit the JACKPOT!\n\n%s\n\n"+
			"**Bet:** %d %s\n"+
			"**Multiplier:** %.1fx\n"+
			"**Won:** %d %s 🎉",
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
		color = utils.ColorRed
		title = "😢 No Luck!"
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

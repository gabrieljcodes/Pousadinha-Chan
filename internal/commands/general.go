package commands

import (
	"estudocoin/pkg/config"
	"estudocoin/pkg/utils"
	"fmt"
	"strconv"
	"strings"

	"github.com/bwmarrin/discordgo"
)

// HelpSection represents a section of the help menu
type HelpSection struct {
	ID    string
	Name  string
	Emoji string
	Value string
}

// getHelpSections retorna as seções de help em tempo de execução (para usar config carregado)
func getHelpSections() []HelpSection {
	return []HelpSection{
		{
			ID:    "economy",
			Name:  "Economy",
			Emoji: "💰",
			Value: fmt.Sprintf("`!daily` / `/daily`\nCollect your daily reward (**%d %s**).\n*Shows remaining time if not available.*\n\n"+
				"`!balance` / `/balance [user]`\nCheck your wallet or someone else's.\n\n"+
				"`!leaderboard` / `/leaderboard`\nSee the richest users.\n\n"+
				"`!pay` / `/pay <user> <amount>`\nTransfer coins to another user.", config.Economy.DailyAmount, config.Bot.CurrencySymbol),
		},
		{
			ID:    "shop",
			Name:  "Shop",
			Emoji: "🛒",
			Value: fmt.Sprintf("`!shop` / `/shop`\nView available items.\n\n"+
				"`!buy nickname <n>`\nChange your own nickname (**%d %s**).\n\n"+
				"`!buy rename @user <n>`\nChange someone else's nickname (**%d %s**).\n\n"+
				"`!buy punishment @user <min>`\nTimeout user (**%d %s/min**) - text & voice.\n*Note: Punishments are accumulative!*\n\n"+
				"`!buy mute @user <min>`\nMute user in voice (**%d %s/min**) - voice only.\n*User must be in a call!*",
				config.Economy.CostNicknameSelf, config.Bot.CurrencySymbol, config.Economy.CostNicknameOther, config.Bot.CurrencySymbol, config.Economy.CostPerMinutePunishment, config.Bot.CurrencySymbol, config.Economy.CostPerMinuteMute, config.Bot.CurrencySymbol),
		},
		{
			ID:    "gambling",
			Name:  "Gambling",
			Emoji: "🎲",
			Value: "`!bet aviator <amount>` / `/bet aviator`\nPlay the Aviator crash game.\n*Watch out for turbulence!*\n\n" +
				"`!bet cups <amount>` / `/bet cups`\nFind the hidden coin under 6 cups.\n*Win 2x -> Double or Cash Out.*\n\n" +
				"`!bet blackjack <amount>` / `/blackjack`\nClassic Blackjack vs dealer.\n*Hit, Stand, Double, Insurance.*\n\n" +
				"`!bet slots <amount>` / `/slots`\nSpin the slot machine!\n*3 = Jackpot | 2 = Win | Up to 25x!*\n\n" +
				"`!roulette @user <amount>`\nRussian Roulette PvP.\n*Survivor takes all!*",
		},
		{
			ID:    "casino",
			Name:  "Casino Roulette",
			Emoji: "🎡",
			Value: "`!wheel`\nView roulette options and time until spin.\n\n" +
				"`!wheel number <0-36> <amount>` - **35:1**\n" +
				"`!wheel red/black <amount>` - **1:1**\n" +
				"`!wheel even/odd <amount>` - **1:1**\n" +
				"`!wheel low/high <amount>` - **1:1**\n" +
				"`!wheel dozen <1st/2nd/3rd> <amount>` - **2:1**\n\n" +
				"*Rounds every 10 min. Betting closes on spin!*",
		},
		{
			ID:    "events",
			Name:  "Event Betting",
			Emoji: "🎯",
			Value: "`!createevent <q> | <opt1> | <opt2> | <min>`\n*Admin only.* Create betting event.\n\n" +
				"`!betevent <id> <opt_num> <amount>`\nPlace bet on event.\n\n" +
				"`!events` - List active events\n" +
				"`!event <id>` - View event details\n" +
				"`!closeevent <id>` - Close early\n" +
				"`!result <id> <opt>` - Set winner\n\n" +
				"*Dynamic odds: less popular = higher payout!*",
		},
		{
			ID:    "stocks",
			Name:  "Stock Market",
			Emoji: "📈",
			Value: "`!stock market`\nView stocks and prices.\n\n" +
				"`!stock buy <ticker> <amount>`\nBuy shares.\n\n" +
				"`!stock sell <ticker> <shares|all>`\nSell shares.\n\n" +
				"`!stock portfolio`\nView investments.",
		},
		{
			ID:    "voice",
			Name:  "Voice Rewards",
			Emoji: "🎙️",
			Value: fmt.Sprintf("Earn **%d %s/min** in voice channels.\n*Need 2+ people, not muted/deafened.*", config.Economy.VoiceCoinsPerMinute, config.Bot.CurrencySymbol),
		},
		{
			ID:    "api",
			Name:  "Developer & API",
			Emoji: "🔧",
			Value: "`/apikey create` - Generate API key\n"+
				"`/apikey list` - View keys\n"+
				"`/webhook set <url>` - Coin notifications",
		},
	}
}

func getHelpEmbed(sectionIdx int) *discordgo.MessageEmbed {
	sections := getHelpSections()
	
	if sectionIdx < 0 {
		sectionIdx = len(sections) - 1
	}
	if sectionIdx >= len(sections) {
		sectionIdx = 0
	}

	section := sections[sectionIdx]

	embed := utils.NewEmbed()
	embed.Title = fmt.Sprintf("%s %s - Page %d/%d", section.Emoji, section.Name, sectionIdx+1, len(sections))
	embed.Description = section.Value
	embed.Color = utils.ColorBlue
	embed.Footer = &discordgo.MessageEmbedFooter{
		Text: fmt.Sprintf("Use !help <section> to jump | Sections: economy, shop, gambling, casino, events, stocks, voice, api"),
	}

	return embed
}

func getHelpButtons(sectionIdx int) []discordgo.MessageComponent {
	return []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Label:    "⬅️ Previous",
					Style:    discordgo.PrimaryButton,
					CustomID: fmt.Sprintf("help_nav_%d", sectionIdx-1),
					Disabled: false,
				},
				discordgo.Button{
					Label:    "➡️ Next",
					Style:    discordgo.PrimaryButton,
					CustomID: fmt.Sprintf("help_nav_%d", sectionIdx+1),
					Disabled: false,
				},
			},
		},
	}
}

func findSectionIndex(sectionID string) int {
	sections := getHelpSections()
	sectionID = strings.ToLower(sectionID)
	for i, section := range sections {
		if strings.ToLower(section.ID) == sectionID || strings.ToLower(section.Name) == sectionID {
			return i
		}
	}
	return -1
}

func CmdHelp(s *discordgo.Session, m *discordgo.MessageCreate) {
	args := strings.Fields(m.Content)
	
	// Check if user specified a section
	sectionIdx := 0
	if len(args) > 1 {
		sectionArg := strings.ToLower(args[1])
		foundIdx := findSectionIndex(sectionArg)
		if foundIdx >= 0 {
			sectionIdx = foundIdx
		}
	}

	embed := getHelpEmbed(sectionIdx)
	buttons := getHelpButtons(sectionIdx)

	s.ChannelMessageSendComplex(m.ChannelID, &discordgo.MessageSend{
		Embeds:     []*discordgo.MessageEmbed{embed},
		Components: buttons,
	})
}

func HandleHelpNavigation(s *discordgo.Session, i *discordgo.InteractionCreate, customID string) {
	// Parse customID: help_nav_<idx>
	parts := strings.Split(customID, "_")
	if len(parts) != 3 {
		return
	}

	sectionIdx, err := strconv.Atoi(parts[2])
	if err != nil {
		return
	}

	// Wrap around
	sections := getHelpSections()
	if sectionIdx < 0 {
		sectionIdx = len(sections) - 1
	}
	if sectionIdx >= len(sections) {
		sectionIdx = 0
	}

	embed := getHelpEmbed(sectionIdx)
	buttons := getHelpButtons(sectionIdx)

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: buttons,
		},
	})
}

func HandleSlashHelp(s *discordgo.Session, i *discordgo.InteractionCreate) {
	embed := getHelpEmbed(0)
	buttons := getHelpButtons(0)

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: buttons,
		},
	})
}

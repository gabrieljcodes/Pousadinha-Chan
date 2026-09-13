package commands

import (
	"bot/internal/locale"
	"bot/pkg/config"
	"bot/pkg/utils"
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

// getHelpSections returns the help sections at runtime (to use loaded config)
func getHelpSections() []HelpSection {
	return []HelpSection{
		{ID: "gacha", Name: locale.Text("commands.general.character_gacha"), Emoji: "✨", Value: locale.Text("commands.general.roll_pool_roll_anime_or_game_characters")},
		{ID: "economy", Name: locale.Text("commands.general.economy"), Emoji: "💰", Value: locale.Text("commands.general.daily_claim_your_daily_streak_reward_balance")},
		{ID: "shop", Name: locale.Text("commands.general.shop"), Emoji: "🛒", Value: locale.Text("commands.general.shop_browse_items_and_current_prices_buy")},
		{ID: "games", Name: locale.Text("commands.general.casino_games"), Emoji: "🎲", Value: locale.Text("commands.general.bet_aviator_crash_game_with_optional_automatic")},
		{ID: "events", Name: locale.Text("commands.general.event_betting"), Emoji: "🎯", Value: locale.Text("commands.general.event_list_event_view_browse_server_events")},
		{ID: "markets", Name: locale.Text("commands.general.markets"), Emoji: "📈", Value: locale.Text("commands.general.stock_market_stock_buy_stock_sell_stock")},
		{ID: "lottery", Name: locale.Text("commands.general.animal_lottery"), Emoji: "🎫", Value: locale.Text("commands.general.bicho_panel_active_round_bicho_table_animals")},
		{ID: "loans", Name: locale.Text("commands.general.loans"), Emoji: "💳", Value: locale.Text("commands.general.loan_offer_offer_a_loan_with_interest")},
		{ID: "voice", Name: locale.Text("commands.general.voice_rewards"), Emoji: "🎙️", Value: locale.Text("commands.general.earn_min_in_voice_channels_with_at.formatted", locale.Data{"VoiceCoinsPerMinute": config.Economy.VoiceCoinsPerMinute, "CurrencySymbol": config.Bot.CurrencySymbol})},
		{ID: "api", Name: locale.Text("commands.general.developer_tools"), Emoji: "🔧", Value: locale.Text("commands.general.apikey_create_apikey_list_manage_api_access")},
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
	embed.Title = locale.Text("commands.general.page.formatted", locale.Data{"Emoji": section.Emoji, "Name": section.Name, "SectionIdx": sectionIdx + 1, "Value4": len(sections)})
	embed.Description = section.Value
	embed.Color = utils.ColorBlue
	embed.Footer = &discordgo.MessageEmbedFooter{
		Text: locale.Text("commands.general.use_the_buttons_to_browse_command_categories"),
	}

	return embed
}

func getHelpButtons(sectionIdx int) []discordgo.MessageComponent {
	return []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Label:    locale.Text("commands.general.previous"),
					Style:    discordgo.PrimaryButton,
					CustomID: fmt.Sprintf("help_nav_%d", sectionIdx-1),
					Disabled: false,
				},
				discordgo.Button{
					Label:    locale.Text("commands.general.next"),
					Style:    discordgo.PrimaryButton,
					CustomID: fmt.Sprintf("help_nav_%d", sectionIdx+1),
					Disabled: false,
				},
			},
		},
	}
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

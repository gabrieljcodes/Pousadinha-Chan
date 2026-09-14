package commands

import (
	"bot/internal/locale"
	"github.com/bwmarrin/discordgo"
)

func init() {
	dm := false
	id := func() *discordgo.ApplicationCommandOption {
		return &discordgo.ApplicationCommandOption{Type: discordgo.ApplicationCommandOptionString, Name: "character", Description: locale.Text("commands.discovery.character_id"), Required: true}
	}
	page := func() *discordgo.ApplicationCommandOption {
		return &discordgo.ApplicationCommandOption{Type: discordgo.ApplicationCommandOptionInteger, Name: "page", Description: locale.Text("commands.discovery.page"), MinValue: ptr(1), MaxValue: 100000}
	}
	SlashCommands = append(SlashCommands,
		&discordgo.ApplicationCommand{Name: "keys", Description: locale.Text("commands.discovery.keys"), DMPermission: &dm, Options: []*discordgo.ApplicationCommandOption{id()}},
		&discordgo.ApplicationCommand{Name: "search", Description: locale.Text("commands.discovery.search"), DMPermission: &dm, Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionString, Name: "query", Description: locale.Text("commands.discovery.query"), Required: true},
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "type",
				Description: locale.Text("commands.discovery.search_type"),
				Required:    false,
				Choices: []*discordgo.ApplicationCommandOptionChoice{
					{Name: locale.Text("commands.discovery.search_type_characters"), Value: "characters"},
					{Name: locale.Text("commands.discovery.search_type_series"), Value: "series"},
				},
			},
			page(),
		}},
		&discordgo.ApplicationCommand{Name: "series", Description: locale.Text("commands.discovery.series"), DMPermission: &dm, Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionString, Name: "series", Description: locale.Text("commands.discovery.series_query"), Required: true},
			page(),
		}},
		&discordgo.ApplicationCommand{Name: "harem-ranking", Description: locale.Text("commands.discovery.ranking"), DMPermission: &dm, Options: []*discordgo.ApplicationCommandOption{page()}},
		&discordgo.ApplicationCommand{Name: "offers", Description: locale.Text("commands.discovery.offers"), DMPermission: &dm, Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionString, Name: "action", Description: locale.Text("commands.discovery.action"), Choices: []*discordgo.ApplicationCommandOptionChoice{
				{Name: locale.Text("commands.discovery.list"), Value: "offers"}, {Name: locale.Text("commands.discovery.accept"), Value: "accept"}, {Name: locale.Text("commands.discovery.decline"), Value: "decline"}, {Name: locale.Text("commands.discovery.cancel"), Value: "cancel"},
			}},
			{Type: discordgo.ApplicationCommandOptionString, Name: "id", Description: locale.Text("commands.discovery.offer_id")},
		}},
	)
}

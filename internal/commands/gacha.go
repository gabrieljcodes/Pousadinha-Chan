package commands

import (
	"bot/internal/gacha"
	"github.com/bwmarrin/discordgo"
)

func init() {
	choices := []*discordgo.ApplicationCommandOptionChoice{}
	for _, p := range gacha.Pools {
		choices = append(choices, &discordgo.ApplicationCommandOptionChoice{Name: p.Name, Value: p.Code})
	}
	for _, v := range []struct{ name, value string }{{"Harem", "harem"}, {"Search", "search"}, {"Character details", "character"}, {"Top characters", "topchar"}, {"Character keys", "keys"}, {"Image gallery", "gallery"}, {"Wish", "wish"}, {"Remove wish", "unwish"}, {"Wishlist", "wishes"}, {"Roll and claim limits", "status"}, {"Divorce for coins", "divorce"}, {"Trade characters", "trade"}, {"Gift a character", "gift"}, {"Pending offers", "offers"}, {"Harem leaderboard", "top"}, {"Help", "help"}} {
		choices = append(choices, &discordgo.ApplicationCommandOptionChoice{Name: v.name, Value: v.value})
	}
	min := 1.0
	page := func() *discordgo.ApplicationCommandOption {
		return &discordgo.ApplicationCommandOption{Type: discordgo.ApplicationCommandOptionInteger, Name: "page", Description: "Page number", MinValue: &min, MaxValue: 100000}
	}
	member := func(required bool) *discordgo.ApplicationCommandOption {
		return &discordgo.ApplicationCommandOption{Type: discordgo.ApplicationCommandOptionUser, Name: "member", Description: "Server member", Required: required}
	}
	character := func(name, description string, required bool) *discordgo.ApplicationCommandOption {
		return &discordgo.ApplicationCommandOption{Type: discordgo.ApplicationCommandOptionInteger, Name: name, Description: description, Required: required, MinValue: &min}
	}
	dm := false
	SlashCommands = append(SlashCommands, &discordgo.ApplicationCommand{Name: "gacha", Description: "Roll, collect and trade characters in this server", DMPermission: &dm, Options: []*discordgo.ApplicationCommandOption{
		{Type: discordgo.ApplicationCommandOptionString, Name: "action", Description: "What would you like to do?", Required: true, Choices: choices},
		{Type: discordgo.ApplicationCommandOptionString, Name: "query", Description: "Search text or character ID", MaxLength: 100},
		character("character", "Character ID (your character when trading)", false),
		member(false), character("receive", "Character ID you want in exchange", false), page(),
	}})
	for _, p := range gacha.Pools {
		if p.Code == "roll" {
			continue
		}
		SlashCommands = append(SlashCommands, &discordgo.ApplicationCommand{Name: p.Code, Description: "Roll: " + p.Name, DMPermission: &dm})
	}

	claimChoices := []*discordgo.ApplicationCommandOptionChoice{
		{Name: "Não claimados (Livres)", Value: "unclaimed"},
		{Name: "Claimados (Casados)", Value: "claimed"},
		{Name: "Todos os personagens", Value: "all"},
	}
	genderChoices := []*discordgo.ApplicationCommandOptionChoice{
		{Name: "Mulheres (Waifus)", Value: "female"},
		{Name: "Homens (Husbandos)", Value: "male"},
		{Name: "Todos os gêneros", Value: "all"},
	}

	SlashCommands = append(SlashCommands,
		&discordgo.ApplicationCommand{Name: "keys", Description: "View a character's keys and value bonuses in this server", DMPermission: &dm, Options: []*discordgo.ApplicationCommandOption{character("character", "Character ID", true)}},
		&discordgo.ApplicationCommand{Name: "im", Description: "Inspect a character by name or ID, view photo and ownership status in this server", DMPermission: &dm, Options: []*discordgo.ApplicationCommandOption{{Type: discordgo.ApplicationCommandOptionString, Name: "name", Description: "Character name or ID", Required: true}}},
		&discordgo.ApplicationCommand{Name: "topchar", Description: "View top characters ranked by popularity with filters", DMPermission: &dm, Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionString, Name: "posse", Description: "Filtrar por status de posse no servidor", Choices: claimChoices},
			{Type: discordgo.ApplicationCommandOptionString, Name: "genero", Description: "Filtrar por gênero (Waifus / Husbandos)", Choices: genderChoices},
			page(),
		}},
		&discordgo.ApplicationCommand{Name: "harem", Description: "View your harem or another member's collection", DMPermission: &dm, Options: []*discordgo.ApplicationCommandOption{
			member(false),
			{Type: discordgo.ApplicationCommandOptionBoolean, Name: "visual", Description: "Exibir modo visual com fotos e setas de navegação"},
			page(),
		}},
		&discordgo.ApplicationCommand{Name: "divorce", Description: "Release a character for server coins, after confirmation", DMPermission: &dm, Options: []*discordgo.ApplicationCommandOption{character("character", "Character ID to release", true)}},
		&discordgo.ApplicationCommand{Name: "trade", Description: "Offer a character trade to another member", DMPermission: &dm, Options: []*discordgo.ApplicationCommandOption{member(true), character("character", "Your character ID", true), character("receive", "Their character ID", true)}},
		&discordgo.ApplicationCommand{Name: "gift", Description: "Offer a character as a gift to another member", DMPermission: &dm, Options: []*discordgo.ApplicationCommandOption{member(true), character("character", "Your character ID", true)}},
	)
}

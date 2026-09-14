package gacha

import (
	"github.com/bwmarrin/discordgo"
	"testing"
)

func TestSlashRequestMapping(t *testing.T) {
	str := func(name, value string) *discordgo.ApplicationCommandInteractionDataOption {
		return &discordgo.ApplicationCommandInteractionDataOption{Name: name, Type: discordgo.ApplicationCommandOptionString, Value: value}
	}
	num := func(name string, value int) *discordgo.ApplicationCommandInteractionDataOption {
		return &discordgo.ApplicationCommandInteractionDataOption{Name: name, Type: discordgo.ApplicationCommandOptionInteger, Value: float64(value)}
	}
	for _, tc := range []struct {
		name          string
		options       []*discordgo.ApplicationCommandInteractionDataOption
		action, query string
		page          int
	}{
		{"roll", nil, "roll", "", 1}, {"roll", []*discordgo.ApplicationCommandInteractionDataOption{str("pool", "wa")}, "wa", "", 1},
		{"profile", nil, "status", "", 1},
		{"info", []*discordgo.ApplicationCommandInteractionDataOption{str("character", "Rem")}, "character", "Rem", 1},
		{"gallery", []*discordgo.ApplicationCommandInteractionDataOption{num("character", 17), num("page", 2)}, "gallery", "17", 2},
		{"trade", []*discordgo.ApplicationCommandInteractionDataOption{str("member", "123"), num("offer", 17), num("receive", 18)}, "trade", "123 17 18", 1},
		{"gift", []*discordgo.ApplicationCommandInteractionDataOption{str("member", "123"), num("character", 17)}, "gift", "123 17", 1},
		{"divorce", []*discordgo.ApplicationCommandInteractionDataOption{num("character", 17)}, "divorce", "17", 1},
		{"harem", []*discordgo.ApplicationCommandInteractionDataOption{str("member", "123"), str("mode", "visual")}, "harem_visual", "123", 1},
		{"top", []*discordgo.ApplicationCommandInteractionDataOption{str("claim", "unclaimed"), str("gender", "female")}, "topchar", "unclaimed female", 1},
		{"wishlist", []*discordgo.ApplicationCommandInteractionDataOption{str("action", "wish"), num("character", 17)}, "wish", "17", 1},
		{"wishlist", []*discordgo.ApplicationCommandInteractionDataOption{str("action", "wishes")}, "wishes", "", 1},
		{"search", []*discordgo.ApplicationCommandInteractionDataOption{str("query", "Rem")}, "search", "Rem", 1},
		{"search", []*discordgo.ApplicationCommandInteractionDataOption{str("query", "One Piece"), str("type", "series")}, "series", "One Piece", 1},
		{"search", []*discordgo.ApplicationCommandInteractionDataOption{str("query", "Rem"), str("type", "characters")}, "search", "Rem", 1},
		{"series", []*discordgo.ApplicationCommandInteractionDataOption{str("series", "One Piece"), num("page", 2)}, "series", "One Piece", 2},
		{"harem-ranking", nil, "ranking", "", 1}, {"keys", []*discordgo.ApplicationCommandInteractionDataOption{num("character", 17)}, "keys", "17", 1},
		{"offers", []*discordgo.ApplicationCommandInteractionDataOption{str("action", "accept"), str("id", "offer-uuid")}, "accept", "offer-uuid", 1},
		{"alias", []*discordgo.ApplicationCommandInteractionDataOption{str("character", "Artoria"), str("alias", "Saber")}, "alias", "Artoria | Saber", 1},
		{"alias", []*discordgo.ApplicationCommandInteractionDataOption{str("character", "Artoria")}, "alias", "Artoria", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, q, p := parseSlash(discordgo.ApplicationCommandInteractionData{Name: tc.name, Options: tc.options})
			if a != tc.action || q != tc.query || p != tc.page {
				t.Fatalf("got %q %q %d; want %q %q %d", a, q, p, tc.action, tc.query, tc.page)
			}
		})
	}
}

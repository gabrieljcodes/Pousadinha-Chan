package commands

import (
	"regexp"
	"testing"
	"unicode/utf8"

	"github.com/bwmarrin/discordgo"
)

func TestSlashCommandCatalog(t *testing.T) {
	validName := regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)
	commands := ApplicationCommands()
	seen := map[string]bool{}
	var checkOptions func([]*discordgo.ApplicationCommandOption)
	checkOptions = func(options []*discordgo.ApplicationCommandOption) {
		if len(options) > 25 {
			t.Fatal("too many command options")
		}
		names := map[string]bool{}
		optional := false
		for _, o := range options {
			if names[o.Name] || !validName.MatchString(o.Name) {
				t.Fatalf("duplicate or invalid option %s", o.Name)
			}
			names[o.Name] = true
			if size := utf8.RuneCountInString(o.Description); size < 1 || size > 100 {
				t.Errorf("invalid description length for %s: %d", o.Name, size)
			}
			if !o.Required {
				optional = true
			} else if optional {
				t.Errorf("required option after optional: %s", o.Name)
			}
			if len(o.Choices) > 25 {
				t.Errorf("too many choices for %s", o.Name)
			}
			for _, choice := range o.Choices {
				if n := utf8.RuneCountInString(choice.Name); n < 1 || n > 100 {
					t.Errorf("invalid choice label for %s", o.Name)
				}
			}
			checkOptions(o.Options)
		}
	}
	for _, c := range commands {
		if seen[c.Name] || !validName.MatchString(c.Name) {
			t.Fatalf("duplicate or invalid command %s", c.Name)
		}
		seen[c.Name] = true
		if c.DMPermission == nil || *c.DMPermission {
			t.Errorf("command %s must be guild-only", c.Name)
		}
		if n := utf8.RuneCountInString(c.Description); n < 1 || n > 100 {
			t.Errorf("invalid command description %s", c.Name)
		}
		checkOptions(c.Options)
	}
	for _, name := range []string{"profile", "gallery", "trade", "gift", "divorce", "event", "language"} {
		if !seen[name] {
			t.Errorf("missing command %s", name)
		}
	}
	for _, name := range []string{"perfil", "galeria", "troca", "presente", "divorcio", "gacha"} {
		if seen[name] {
			t.Errorf("legacy command %s remains", name)
		}
	}
}

func TestCatalogHashDeterministic(t *testing.T) {
	h1 := CatalogHash()
	h2 := CatalogHash()
	if h1 == "" || h2 == "" {
		t.Fatal("expected non-empty catalog hash")
	}
	if h1 != h2 {
		t.Fatalf("expected deterministic catalog hash, got %s and %s", h1, h2)
	}
	if len(h1) != 64 {
		t.Fatalf("expected 64-char sha256 hex string, got length %d", len(h1))
	}
}

func TestSlashCommandRegistryConsistency(t *testing.T) {
	commands := ApplicationCommands()
	if len(commands) == 0 {
		t.Fatal("expected commands to be non-empty")
	}

	for _, cmd := range commands {
		route, ok := GetSlashRoute(cmd.Name)
		if !ok {
			t.Errorf("command %q is defined in ApplicationCommands but missing from commandRegistry", cmd.Name)
			continue
		}
		if route.Handler == nil {
			t.Errorf("command %q in commandRegistry has a nil Handler", cmd.Name)
		}
		if route.Category == "" {
			t.Errorf("command %q in commandRegistry has an empty Category", cmd.Name)
		}
	}

	for name := range commandRegistry {
		found := false
		for _, cmd := range commands {
			if cmd.Name == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("command %q is registered in commandRegistry but not defined in ApplicationCommands", name)
		}
	}
}

func TestSlashCommandChannelAuthorization(t *testing.T) {
	// 1. Verify Gacha commands (including builds, learnskill, respec, addcustom, roll) are classified as CategoryGacha
	gachaCmds := []string{"builds", "learnskill", "respec", "roll", "rolls", "harem", "top", "wishlist", "addcustom", "customimage", "gachaconfig"}
	for _, name := range gachaCmds {
		route, ok := GetSlashRoute(name)
		if !ok {
			t.Errorf("expected gacha command %q to be registered", name)
			continue
		}
		if route.Category != CategoryGacha {
			t.Errorf("command %q has category %q, want %q", name, route.Category, CategoryGacha)
		}
		if !isGachaCommand(name) {
			t.Errorf("isGachaCommand(%q) = false, want true", name)
		}

		// When used in any channel, isChannelAuthorized should allow Gacha commands so gacha.ValidateChannel can handle them
		interaction := &discordgo.InteractionCreate{
			Interaction: &discordgo.Interaction{
				ChannelID: "some-channel-id",
			},
		}
		if !isChannelAuthorized(interaction, route) {
			t.Errorf("expected isChannelAuthorized to allow gacha command %q in special/gacha channels", name)
		}
	}

	// 2. Verify non-gacha commands (e.g. daily, balance, roulette) are NOT classified as CategoryGacha
	nonGachaCmds := []string{"daily", "balance", "roulette", "blackjack", "help"}
	for _, name := range nonGachaCmds {
		route, ok := GetSlashRoute(name)
		if !ok {
			t.Errorf("expected command %q to be registered", name)
			continue
		}
		if route.Category == CategoryGacha {
			t.Errorf("command %q should not be CategoryGacha", name)
		}
		if isGachaCommand(name) {
			t.Errorf("isGachaCommand(%q) = true, want false", name)
		}
	}
}


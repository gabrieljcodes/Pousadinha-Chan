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

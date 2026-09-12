package commands

import (
	"github.com/bwmarrin/discordgo"
	"testing"
)

func TestGachaSlashDefinitions(t *testing.T) {
	wanted := map[string]bool{"gacha": false, "keys": false, "wa": false, "ha": false, "ma": false, "wg": false, "hg": false, "mg": false, "w": false, "h": false, "harem": false, "trade": false, "gift": false, "divorce": false}
	for _, cmd := range SlashCommands {
		if _, ok := wanted[cmd.Name]; !ok {
			continue
		}
		if wanted[cmd.Name] {
			t.Fatalf("duplicate command %s", cmd.Name)
		}
		wanted[cmd.Name] = true
		if cmd.DMPermission == nil || *cmd.DMPermission {
			t.Fatalf("%s allows DMs", cmd.Name)
		}
		optional := false
		for _, o := range cmd.Options {
			if len(o.Choices) > 25 {
				t.Fatalf("too many choices for %s", cmd.Name)
			}
			if !o.Required {
				optional = true
			} else if optional {
				t.Fatalf("required option follows optional in %s", cmd.Name)
			}
			if o.Name == "member" && o.Type != discordgo.ApplicationCommandOptionUser {
				t.Fatal("member must use Discord user selection")
			}
		}
	}
	for name, found := range wanted {
		if !found {
			t.Fatalf("missing command %s", name)
		}
	}
}

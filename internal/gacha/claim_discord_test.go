package gacha

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/bwmarrin/discordgo"
)

type claimTransport func(*http.Request) (*http.Response, error)

func (f claimTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func testClaimMessageRepair(t *testing.T, store *Store) {
	ctx := context.Background()
	roll, err := store.Roll(ctx, "claim-message-test", "channel", "roller", "claim-message-request")
	if err != nil {
		t.Fatal(err)
	}
	previous := Default
	Default = store
	t.Cleanup(func() { Default = previous })
	source := &discordgo.Message{ID: "message", Embeds: []*discordgo.MessageEmbed{{Title: "Character", Description: "Original roll", Fields: []*discordgo.MessageEmbedField{{Name: "Metadata", Value: "preserved"}}}}}
	var mu sync.Mutex
	var patches []*discordgo.Message
	newSession := func() *discordgo.Session {
		s, err := discordgo.New("Bot test")
		if err != nil {
			t.Fatal(err)
		}
		s.Client = &http.Client{Transport: claimTransport(func(r *http.Request) (*http.Response, error) {
			status, body := 200, `{}`
			if r.Method == http.MethodPatch && strings.Contains(r.URL.Path, "/channels/") {
				var edit discordgo.Message
				if err := json.NewDecoder(r.Body).Decode(&edit); err != nil {
					t.Error(err)
				}
				mu.Lock()
				patches = append(patches, &edit)
				// The first public edit fails after the database claim has committed.
				if len(patches) == 1 {
					status, body = 403, `{"code":50013,"message":"Missing Permissions"}`
				}
				mu.Unlock()
			}
			return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		})}
		return s
	}
	click := func(user string) {
		HandleClaim(newSession(), &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{
			Type: discordgo.InteractionMessageComponent, ID: "click-" + user, AppID: "app", Token: "test", GuildID: "claim-message-test", ChannelID: "channel", Message: source,
			Member: &discordgo.Member{User: &discordgo.User{ID: user}},
			Data:   discordgo.MessageComponentInteractionData{CustomID: "gacha_claim_" + roll.ID},
		}})
	}
	var wg sync.WaitGroup
	for _, user := range []string{"claim-alice", "claim-bob"} {
		wg.Add(1)
		go func(user string) { defer wg.Done(); click(user) }(user)
	}
	wg.Wait()
	owner, err := store.ClaimedRollOwner(ctx, "claim-message-test", "channel", roll.ID)
	if err != nil {
		t.Fatal(err)
	}
	if owner != "claim-alice" && owner != "claim-bob" {
		t.Fatal("invalid winner", owner)
	}
	// A later stale click must repair even when the successful claim's edit failed.
	click("claim-late")
	if len(patches) != 3 {
		t.Fatalf("public reconciliations=%d, want 3", len(patches))
	}
	for _, edit := range patches {
		if !strings.Contains(edit.Content, "<@"+owner+">") {
			t.Fatal("public message used clicker instead of persisted winner")
		}
		if len(edit.Components) != 1 {
			t.Fatal("missing terminal components")
		}
		row := edit.Components[0].(*discordgo.ActionsRow)
		button := row.Components[0].(*discordgo.Button)
		if !button.Disabled {
			t.Fatal("stale click re-enabled claim")
		}
		if len(edit.Embeds) == 0 || len(edit.Embeds[0].Fields) != 2 {
			t.Fatal("missing ownership field")
		}
	}
	if len(source.Embeds[0].Fields) != 1 || source.Embeds[0].Description != "Original roll" {
		t.Fatal("shared interaction snapshot mutated")
	}
	if _, err = store.ClaimedRollOwner(ctx, "other-guild", "channel", roll.ID); err == nil {
		t.Fatal("receipt escaped guild")
	}
	if _, err = store.ClaimedRollOwner(ctx, "claim-message-test", "other-channel", roll.ID); err == nil {
		t.Fatal("receipt escaped channel")
	}
	var ownerCount, quotaCount int
	if err = store.DB.QueryRow(`SELECT count(*) FROM gacha_collection WHERE guild_id='claim-message-test' AND character_id=$1`, roll.Card.ID).Scan(&ownerCount); err != nil {
		t.Fatal(err)
	}
	if err = store.DB.QueryRow(`SELECT count(*) FROM gacha_players WHERE guild_id='claim-message-test' AND claim_after>now()`).Scan(&quotaCount); err != nil {
		t.Fatal(err)
	}
	if ownerCount != 1 || quotaCount != 1 {
		t.Fatalf("ownerships=%d cooldowns=%d", ownerCount, quotaCount)
	}
}

func TestClaimedRollEditIsIdempotent(t *testing.T) {
	message := &discordgo.Message{ID: "message", Embeds: []*discordgo.MessageEmbed{{Description: "Roll"}}}
	first := claimedRollEdit(message, "channel", "roll", "winner")
	second := claimedRollEdit(&discordgo.Message{ID: "message", Embeds: *first.Embeds}, "channel", "roll", "winner")
	if len((*second.Embeds)[0].Fields) != 1 {
		t.Fatal("duplicate claim field")
	}
	if claimedRollEdit(message, "channel", "roll", "") != nil {
		t.Fatal("rendered uncommitted winner")
	}
}

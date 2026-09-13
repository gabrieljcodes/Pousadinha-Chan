package games

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
)

type eventTransport func(*http.Request) (*http.Response, error)

func (f eventTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestEventInteractionAuthorization(t *testing.T) {
	event := &BettingEvent{ID: "event-isolation", GuildID: "guild-a", CreatorID: "creator", Question: "Test event?", Status: "open"}
	eventsMu.Lock()
	previous := activeEvents[event.ID]
	activeEvents[event.ID] = event
	eventsMu.Unlock()
	defer func() {
		eventsMu.Lock()
		if previous == nil {
			delete(activeEvents, event.ID)
		} else {
			activeEvents[event.ID] = previous
		}
		eventsMu.Unlock()
	}()
	for _, tc := range []struct{ guild, user, action, expected string }{
		{"guild-b", "creator", "view", "Event not found in this server."},
		{"guild-a", "other", "cancel", "Only the event creator or a server administrator"},
		{"guild-a", "other", "create", "You need Administrator or Manage Server"},
	} {
		t.Run(tc.guild+tc.action, func(t *testing.T) {
			var payloads []string
			session, _ := discordgo.New("Bot test")
			session.Client = &http.Client{Transport: eventTransport(func(r *http.Request) (*http.Response, error) {
				body, _ := io.ReadAll(r.Body)
				payloads = append(payloads, string(body))
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"reply"}`)), Request: r}, nil
			})}
			interaction := &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{ID: "interaction", AppID: "application", Token: "test", Type: discordgo.InteractionApplicationCommand, GuildID: tc.guild, ChannelID: "channel", Member: &discordgo.Member{User: &discordgo.User{ID: tc.user}}, Data: discordgo.ApplicationCommandInteractionData{Name: "event", Options: []*discordgo.ApplicationCommandInteractionDataOption{{Name: tc.action, Type: discordgo.ApplicationCommandOptionSubCommand, Options: []*discordgo.ApplicationCommandInteractionDataOption{{Name: "id", Type: discordgo.ApplicationCommandOptionString, Value: event.ID}}}}}}}
			HandleEventCommand(session, interaction)
			if !strings.Contains(strings.Join(payloads, "\n"), tc.expected) {
				t.Fatalf("unexpected replies: %v", payloads)
			}
			event.mu.RLock()
			status := event.Status
			event.mu.RUnlock()
			if status != "open" {
				t.Fatal("unauthorized interaction changed the event")
			}
		})
	}
}

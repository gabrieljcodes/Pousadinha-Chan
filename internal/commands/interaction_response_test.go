package commands

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
)

type interactionTransport func(*http.Request) (*http.Response, error)

func (f interactionTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDeferredEmbedAcknowledgesBeforeWork(t *testing.T) {
	for _, reject := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "expired_interaction"}[reject], func(t *testing.T) {
			calls, builds := 0, 0
			session, err := discordgo.New("Bot test")
			if err != nil {
				t.Fatal(err)
			}
			session.Client = &http.Client{Transport: interactionTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				status, body := http.StatusOK, `{}`
				if calls == 1 {
					if builds != 0 {
						t.Fatal("reward work started before acknowledgement")
					}
					var response discordgo.InteractionResponse
					if err := json.NewDecoder(r.Body).Decode(&response); err != nil {
						t.Fatal(err)
					}
					if r.Method != http.MethodPost || response.Type != discordgo.InteractionResponseDeferredChannelMessageWithSource {
						t.Fatalf("unexpected acknowledgement: %s %+v", r.Method, response)
					}
					if reject {
						status, body = http.StatusNotFound, `{"code":10062,"message":"Unknown interaction"}`
					}
				} else {
					if calls != 2 || builds != 1 || r.Method != http.MethodPatch {
						t.Fatal("invalid response sequence")
					}
					var edit discordgo.WebhookEdit
					if err := json.NewDecoder(r.Body).Decode(&edit); err != nil {
						t.Fatal(err)
					}
					if edit.Embeds == nil || len(*edit.Embeds) != 1 || (*edit.Embeds)[0].Title != "Reward result" {
						t.Fatal("reward result missing")
					}
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})}
			interaction := &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{ID: "test", AppID: "app", Token: "token"}}
			respondDeferredEmbed(session, interaction, func() *discordgo.MessageEmbed {
				if calls != 1 {
					t.Fatal("work ran without acknowledgement")
				}
				builds++
				return &discordgo.MessageEmbed{Title: "Reward result"}
			})
			if reject && (builds != 0 || calls != 1) {
				t.Fatal("failed acknowledgement triggered work")
			}
			if !reject && (builds != 1 || calls != 2) {
				t.Fatal("response did not finish")
			}
		})
	}
}

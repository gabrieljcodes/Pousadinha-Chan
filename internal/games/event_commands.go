package games

import (
	"bot/internal/database"
	"bot/internal/locale"
	"bot/pkg/utils"
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
)

// Event commands use structured interaction options and never parse message text.
func HandleEventCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Member == nil || i.Member.User == nil || i.GuildID == "" {
		return
	}
	options := i.ApplicationCommandData().Options
	if len(options) == 0 {
		return
	}
	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{Type: discordgo.InteractionResponseDeferredChannelMessageWithSource}); err != nil {
		return
	}
	reply := func(embed *discordgo.MessageEmbed) {
		embeds := []*discordgo.MessageEmbed{embed}
		_, _ = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Embeds: &embeds, AllowedMentions: &discordgo.MessageAllowedMentions{}})
	}
	values := map[string]*discordgo.ApplicationCommandInteractionDataOption{}
	for _, o := range options[0].Options {
		values[o.Name] = o
	}
	action := options[0].Name
	admin := i.Member.Permissions&(discordgo.PermissionAdministrator|discordgo.PermissionManageServer) != 0
	if action == "create" {
		if !admin {
			reply(utils.ErrorEmbed(locale.Text("games.event_commands.you_need_administrator_or_manage_server_permissions")))
			return
		}
		var choices []string
		for _, choice := range strings.Split(values["options"].StringValue(), "|") {
			if choice = strings.TrimSpace(choice); choice != "" {
				choices = append(choices, choice)
			}
		}
		duration := 60
		if o := values["duration"]; o != nil {
			duration = int(o.IntValue())
		}
		event, msg := CreateEvent(i.GuildID, i.ChannelID, i.Member.User.ID, values["question"].StringValue(), choices, duration)
		if event == nil {
			reply(utils.ErrorEmbed(msg))
			return
		}
		embeds := []*discordgo.MessageEmbed{event.ToEmbed()}
		sent, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Embeds: &embeds, AllowedMentions: &discordgo.MessageAllowedMentions{}})
		if err == nil && sent != nil {
			event.mu.Lock()
			event.MessageID = sent.ID
			event.mu.Unlock()
			_ = database.UpdateEventMessageIDDB(event.ID, sent.ID)
		}
		return
	}
	if action == "list" {
		eventsMu.RLock()
		events := make([]*BettingEvent, 0, len(activeEvents))
		for _, event := range activeEvents {
			events = append(events, event)
		}
		eventsMu.RUnlock()
		var lines []string
		for _, event := range events {
			event.mu.RLock()
			if event.GuildID == i.GuildID && (event.Status == "open" || event.Status == "closed") {
				lines = append(lines, fmt.Sprintf("`%s` — %s", event.ID, event.Question))
			}
			event.mu.RUnlock()
			if len(lines) == 10 {
				break
			}
		}
		content := strings.Join(lines, "\n")
		if content == "" {
			content = locale.Text("games.event_commands.no_active_betting_events_right_now")
		}
		reply(utils.InfoEmbed(locale.Text("games.event_commands.active_events"), content))
		return
	}
	id := values["id"]
	if id == nil {
		reply(utils.ErrorEmbed(locale.Text("games.event_commands.provide_an_event_id")))
		return
	}
	event := eventForGuild(i.GuildID, id.StringValue())
	if event == nil {
		reply(utils.ErrorEmbed(locale.Text("games.event_commands.event_not_found_in_this_server")))
		return
	}
	event.mu.RLock()
	creator := event.CreatorID
	event.mu.RUnlock()
	if action == "view" {
		reply(event.ToEmbed())
		return
	}
	if action == "bet" {
		ok, msg := PlaceBet(i.Member.User.ID, i.Member.User.Username, event.ID, int(values["option"].IntValue()), int(values["amount"].IntValue()))
		if !ok {
			reply(utils.ErrorEmbed(msg))
			return
		}
		reply(utils.SuccessEmbed(locale.Text("games.event_commands.bet_placed"), locale.Text("games.event_commands.your_event_bet_has_been_registered")))
		refreshEventMessage(s, event)
		return
	}
	if !admin && creator != i.Member.User.ID {
		reply(utils.ErrorEmbed(locale.Text("games.event_commands.only_the_event_creator_or_a_server")))
		return
	}
	switch action {
	case "result":
		ok, msg, _ := SetResult(i.Member.User.ID, event.ID, int(values["option"].IntValue()))
		if !ok {
			reply(utils.ErrorEmbed(msg))
			return
		}
		reply(utils.SuccessEmbed(locale.Text("games.event_commands.event_resolved"), msg))
	case "cancel":
		ok, msg, _ := CancelEvent(i.Member.User.ID, event.ID)
		if !ok {
			reply(utils.ErrorEmbed(msg))
			return
		}
		reply(utils.SuccessEmbed(locale.Text("games.event_commands.event_cancelled"), msg))
	case "close":
		if err := database.CloseBettingEventDB(event.ID); err != nil {
			reply(utils.ErrorEmbed(locale.Text("games.event_commands.unable_to_close_this_event")))
			return
		}
		event.mu.Lock()
		event.Status = "closed"
		event.mu.Unlock()
		reply(utils.SuccessEmbed(locale.Text("games.event_commands.event_closed"), locale.Text("games.event_commands.betting_is_now_closed_use_event_result")))
	}
	refreshEventMessage(s, event)
}

func eventForGuild(guild, id string) *BettingEvent {
	eventsMu.RLock()
	event := activeEvents[id]
	eventsMu.RUnlock()
	if event == nil {
		stored, err := database.GetBettingEventByID(id)
		if err != nil || stored == nil || stored.GuildID != guild {
			return nil
		}
		event = fromDBEvent(stored)
		eventsMu.Lock()
		if current := activeEvents[id]; current != nil {
			event = current
		} else {
			activeEvents[id] = event
		}
		eventsMu.Unlock()
	}
	event.mu.RLock()
	matches := event.GuildID == guild
	event.mu.RUnlock()
	if !matches {
		return nil
	}
	return event
}
func refreshEventMessage(s *discordgo.Session, event *BettingEvent) {
	event.mu.RLock()
	channel, id := event.ChannelID, event.MessageID
	event.mu.RUnlock()
	if channel != "" && id != "" {
		_, _ = s.ChannelMessageEditEmbed(channel, id, event.ToEmbed())
	}
}

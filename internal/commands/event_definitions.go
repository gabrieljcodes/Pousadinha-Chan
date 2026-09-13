package commands

import (
	"bot/internal/locale"
	"github.com/bwmarrin/discordgo"
)

func init() {
	dm := false
	str := func(name, description string) *discordgo.ApplicationCommandOption {
		return &discordgo.ApplicationCommandOption{Type: discordgo.ApplicationCommandOptionString, Name: name, Description: description, Required: true}
	}
	integer := func(name, description string) *discordgo.ApplicationCommandOption {
		return &discordgo.ApplicationCommandOption{Type: discordgo.ApplicationCommandOptionInteger, Name: name, Description: description, Required: true, MinValue: ptr(1.0)}
	}
	sub := func(name, description string, options ...*discordgo.ApplicationCommandOption) *discordgo.ApplicationCommandOption {
		return &discordgo.ApplicationCommandOption{Type: discordgo.ApplicationCommandOptionSubCommand, Name: name, Description: description, Options: options}
	}
	id := func() *discordgo.ApplicationCommandOption {
		return str("id", locale.Text("commands.event_definitions.event_id"))
	}
	option := func() *discordgo.ApplicationCommandOption {
		return integer("option", locale.Text("commands.event_definitions.option_number_displayed_in_the_event"))
	}
	duration := integer("duration", locale.Text("commands.event_definitions.duration_in_minutes_default"))
	duration.Required = false
	duration.MaxValue = 1440
	SlashCommands = append(SlashCommands, &discordgo.ApplicationCommand{Name: "event", Description: locale.Text("commands.event_definitions.create_browse_and_manage_server_betting_events"), DMPermission: &dm, Options: []*discordgo.ApplicationCommandOption{
		sub("create", locale.Text("commands.event_definitions.create_a_betting_event_administrator_or_manage"), str("question", locale.Text("commands.event_definitions.question_for_the_event")), str("options", locale.Text("commands.event_definitions.two_or_more_choices_separated_by")), duration),
		sub("list", locale.Text("commands.event_definitions.list_active_events_in_this_server")), sub("view", locale.Text("commands.event_definitions.view_a_betting_event"), id()),
		sub("bet", locale.Text("commands.event_definitions.bet_on_an_event_option"), id(), option(), integer("amount", locale.Text("commands.event_definitions.coins_to_bet"))),
		sub("close", locale.Text("commands.event_definitions.close_an_event_to_new_bets"), id()), sub("result", locale.Text("commands.event_definitions.resolve_an_event_and_pay_winners"), id(), option()), sub("cancel", locale.Text("commands.event_definitions.cancel_an_event_and_refund_bets"), id()),
	}})
}

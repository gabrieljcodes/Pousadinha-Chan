package commands

import (
	"bot/internal/locale"
	"github.com/bwmarrin/discordgo"
)

func init() {
	dm := false
	var adminPerms int64 = discordgo.PermissionAdministrator | discordgo.PermissionManageServer
	minMinute := 0.0
	maxMinute := 59.0
	minRolls := 1.0
	maxRolls := 100.0
	minClaims := 1.0
	maxClaims := 24.0

	SlashCommands = append(SlashCommands,
		&discordgo.ApplicationCommand{
			Name:                     "gachaconfig",
			Description:              locale.Text("commands.gacha.configure_reset_schedule_and_quotas"),
			DMPermission:             &dm,
			DefaultMemberPermissions: &adminPerms,
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Name:        "view",
					Description: locale.Text("commands.gacha.view_current_reset_schedule"),
				},
				{
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Name:        "set",
					Description: locale.Text("commands.gacha.set_reset_schedule_and_quotas"),
					Options: []*discordgo.ApplicationCommandOption{
						{
							Type:        discordgo.ApplicationCommandOptionInteger,
							Name:        "reset_minute",
							Description: locale.Text("commands.gacha.minute_past_hour_when_rolls_reset"),
							MinValue:    &minMinute,
							MaxValue:    maxMinute,
							Required:    false,
						},
						{
							Type:        discordgo.ApplicationCommandOptionInteger,
							Name:        "rolls",
							Description: locale.Text("commands.gacha.number_of_rolls_per_hour"),
							MinValue:    &minRolls,
							MaxValue:    maxRolls,
							Required:    false,
						},
						{
							Type:        discordgo.ApplicationCommandOptionInteger,
							Name:        "claim_interval",
							Description: locale.Text("commands.gacha.claim_reset_interval_in_hours"),
							MinValue:    &minClaims,
							MaxValue:    maxClaims,
							Required:    false,
						},
					},
				},
				{
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Name:        "reset",
					Description: locale.Text("commands.gacha.restore_default_gacha_settings"),
				},
			},
		},
	)
}

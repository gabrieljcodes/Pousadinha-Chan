package gacha

import (
	"bot/internal/locale"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

// HandleConfigSlash handles the /gachaconfig admin slash command.
func HandleConfigSlash(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if Default == nil || i.Member == nil || i.GuildID == "" {
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: locale.Text("gacha.discord.gacha_is_unavailable_use_it_in_a"),
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	isAdmin := i.Member.Permissions&(discordgo.PermissionAdministrator|discordgo.PermissionManageServer) != 0
	if !isAdmin {
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: locale.Text("gacha.config.admin_only"),
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	data := i.ApplicationCommandData()
	subcmd := "view"
	var resetMinuteOpt, rollsOpt, claimOpt *int

	if len(data.Options) > 0 {
		subcmd = data.Options[0].Name
		for _, o := range data.Options[0].Options {
			v := int(o.IntValue())
			switch o.Name {
			case "reset_minute":
				resetMinuteOpt = &v
			case "rolls":
				rollsOpt = &v
			case "claim_interval":
				claimOpt = &v
			}
		}
	}

	switch subcmd {
	case "set":
		current := Default.GuildSchedule(ctx, i.GuildID)
		if resetMinuteOpt == nil && rollsOpt == nil && claimOpt == nil {
			_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: locale.Text("gacha.config.set_no_options"),
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})
			return
		}

		minute := current.ResetMinute
		if resetMinuteOpt != nil {
			minute = *resetMinuteOpt
		}
		rolls := current.RollsPerHour
		if rollsOpt != nil {
			rolls = *rollsOpt
		}
		claims := current.ClaimHours
		if claimOpt != nil {
			claims = *claimOpt
		}

		if err := Default.SetGuildSchedule(ctx, i.GuildID, minute, rolls, claims); err != nil {
			_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: fmt.Sprintf("❌ %s", err.Error()),
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})
			return
		}

		embed := renderScheduleEmbed(Default.GuildSchedule(ctx, i.GuildID), locale.Text("gacha.config.updated_title"))
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Embeds: []*discordgo.MessageEmbed{embed},
			},
		})

	case "channels":
		var rollChannelOpt, cmdChannelOpt string
		if len(data.Options) > 0 {
			for _, o := range data.Options[0].Options {
				ch := o.ChannelValue(s)
				var chID string
				if ch != nil {
					chID = ch.ID
				} else {
					chID = o.StringValue()
				}
				switch o.Name {
				case "roll_channel":
					rollChannelOpt = chID
				case "command_channel":
					cmdChannelOpt = chID
				}
			}
		}

		if rollChannelOpt == "" && cmdChannelOpt == "" {
			_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: locale.Text("gacha.config.channels_no_options"),
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})
			return
		}

		if err := Default.SetGuildChannels(ctx, i.GuildID, rollChannelOpt, cmdChannelOpt); err != nil {
			_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: fmt.Sprintf("❌ %s", err.Error()),
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})
			return
		}

		embed := renderScheduleEmbed(Default.GuildSchedule(ctx, i.GuildID), locale.Text("gacha.config.channels_title"))
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Embeds: []*discordgo.MessageEmbed{embed},
			},
		})

	case "reset":
		if err := Default.ResetGuildSchedule(ctx, i.GuildID); err != nil {
			_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: fmt.Sprintf("❌ %s", err.Error()),
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})
			return
		}

		embed := renderScheduleEmbed(Default.GuildSchedule(ctx, i.GuildID), locale.Text("gacha.config.reset_title"))
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Embeds: []*discordgo.MessageEmbed{embed},
			},
		})

	default: // "view"
		embed := renderScheduleEmbed(Default.GuildSchedule(ctx, i.GuildID), locale.Text("gacha.config.view_title"))
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Embeds: []*discordgo.MessageEmbed{embed},
			},
		})
	}
}

func renderScheduleEmbed(sch ResetSchedule, title string) *discordgo.MessageEmbed {
	now := time.Now()
	rWin := sch.RollWindow(now)
	cWin := sch.ClaimWindow(now)

	var b strings.Builder
	b.WriteString(locale.Text("gacha.config.roll_reset_info.formatted", locale.Data{"Minute": fmt.Sprintf("%02d", sch.ResetMinute)}))
	b.WriteString("\n")
	b.WriteString(locale.Text("gacha.config.rolls_quota_info.formatted", locale.Data{"Rolls": sch.RollsPerHour}))
	b.WriteString("\n")
	b.WriteString(locale.Text("gacha.config.claim_interval_info.formatted", locale.Data{"ClaimHours": sch.ClaimHours}))
	b.WriteString("\n\n")
	b.WriteString(locale.Text("gacha.config.roll_channel_info.formatted", locale.Data{"Channel": sch.RollChannelID}))
	b.WriteString("\n")
	b.WriteString(locale.Text("gacha.config.command_channel_info.formatted", locale.Data{"Channel": sch.CmdChannelID}))
	b.WriteString("\n\n")
	b.WriteString(locale.Text("gacha.config.next_roll_reset.formatted", locale.Data{"Unix": rWin.NextReset.Unix()}))
	b.WriteString("\n")
	b.WriteString(locale.Text("gacha.config.next_claim_reset.formatted", locale.Data{"Unix": cWin.NextReset.Unix()}))

	return &discordgo.MessageEmbed{
		Title:       title,
		Description: b.String(),
		Color:       0x5865F2,
		Footer: &discordgo.MessageEmbedFooter{
			Text: locale.Text("gacha.config.footer"),
		},
	}
}

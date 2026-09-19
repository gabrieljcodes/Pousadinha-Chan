package gacha

import (
	"bot/internal/locale"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

// HandleAddCustom executes custom photo/GIF submission from slash command or prefix trigger.
func HandleAddCustom(ctx context.Context, s *discordgo.Session, guildID, channelID, userID, authorTag, charQuery, mediaURL string, attachment *discordgo.MessageAttachment, isAdmin bool) (*discordgo.MessageSend, error) {
	if Default == nil || guildID == "" {
		return nil, userError(locale.Text("gacha.discord.use_this_command_in_a_server"))
	}

	card, _, err := Default.FindCharacter(ctx, guildID, charQuery)
	if err != nil {
		return nil, err
	}

	sch := Default.GuildSchedule(ctx, guildID)
	autoApprove := sch.CustomImageAutoApprove || isAdmin

	var body io.Reader
	urlToFetch := strings.TrimSpace(mediaURL)

	if attachment != nil && attachment.URL != "" {
		urlToFetch = attachment.URL
	}

	if urlToFetch == "" {
		return nil, userError(locale.Text("gacha.custom.missing_url_or_file"))
	}

	res, err := Default.SubmitCustomImage(ctx, guildID, userID, authorTag, card.ID, urlToFetch, body, autoApprove)
	if err != nil {
		return nil, err
	}

	embed := &discordgo.MessageEmbed{
		Footer: &discordgo.MessageEmbedFooter{
			Text: locale.Text("gacha.custom.usage_footer.formatted", locale.Data{"Used": res.TotalActive}),
		},
	}
	if res.URL != "" {
		embed.Image = &discordgo.MessageEmbedImage{URL: res.URL}
	}

	if res.AutoApproved {
		embed.Title = locale.Text("gacha.custom.submitted_approved_title")
		embed.Description = locale.Text("gacha.custom.submitted_approved_desc.formatted", locale.Data{"Character": card.Name})
		embed.Color = 0x2ecc71 // Green
		return &discordgo.MessageSend{
			Embeds: []*discordgo.MessageEmbed{embed},
		}, nil
	}

	// Pending approval
	embed.Title = locale.Text("gacha.custom.submitted_pending_title")
	embed.Description = locale.Text("gacha.custom.submitted_pending_desc.formatted", locale.Data{"Character": card.Name})
	embed.Color = 0xf39c12 // Amber

	// Send review message to command channel or review channel with interactive buttons
	reviewEmbed := &discordgo.MessageEmbed{
		Title:       locale.Text("gacha.custom.review_title"),
		Description: locale.Text("gacha.custom.review_desc.formatted", locale.Data{"Character": card.Name, "CharID": card.ID, "User": userID, "AssetID": res.AssetID}),
		Color:       0x3498db, // Blue
		Image:       &discordgo.MessageEmbedImage{URL: res.URL},
		Footer: &discordgo.MessageEmbedFooter{
			Text: locale.Text("gacha.custom.usage_footer.formatted", locale.Data{"Used": res.TotalActive}),
		},
	}
	reviewRow := discordgo.ActionsRow{
		Components: []discordgo.MessageComponent{
			discordgo.Button{
				Label:    "Approve",
				Style:    discordgo.SuccessButton,
				CustomID: fmt.Sprintf("gacha_asset_approve_%d", res.AssetID),
				Emoji:    &discordgo.ComponentEmoji{Name: "✅"},
			},
			discordgo.Button{
				Label:    "Reject",
				Style:    discordgo.DangerButton,
				CustomID: fmt.Sprintf("gacha_asset_reject_%d", res.AssetID),
				Emoji:    &discordgo.ComponentEmoji{Name: "❌"},
			},
		},
	}

	targetChannel := channelID
	if sch.CmdChannelID != "" {
		targetChannel = sch.CmdChannelID
	}
	_, _ = s.ChannelMessageSendComplex(targetChannel, &discordgo.MessageSend{
		Embeds:     []*discordgo.MessageEmbed{reviewEmbed},
		Components: []discordgo.MessageComponent{reviewRow},
	})

	return &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{embed},
	}, nil
}

// HandleMyCustoms displays the user's submitted custom images and usage quota.
func HandleMyCustoms(ctx context.Context, guildID, userID string) (*discordgo.MessageSend, error) {
	if Default == nil {
		return nil, errors.New("gacha store unavailable")
	}

	list, err := Default.ListUserCustomImages(ctx, userID)
	if err != nil {
		return nil, err
	}

	if len(list) == 0 {
		return &discordgo.MessageSend{
			Embeds: []*discordgo.MessageEmbed{
				{
					Title:       locale.Text("gacha.custom.mycustoms_title.formatted", locale.Data{"Count": 0}),
					Description: locale.Text("gacha.custom.mycustoms_empty"),
					Color:       0x5865F2,
				},
			},
		}, nil
	}

	var b strings.Builder
	for i, item := range list {
		statusEmoji := "⏳"
		if item.Status == "approved" {
			statusEmoji = "✅"
		} else if item.Status == "rejected" {
			statusEmoji = "❌"
		}
		typeLabel := "PHOTO"
		if item.MediaType == "image/gif" {
			typeLabel = "GIF"
		}
		b.WriteString(fmt.Sprintf("`#%d` %s **%s** `[%s]` — ID: `%d`\n", i+1, statusEmoji, item.CharacterName, typeLabel, item.ID))
	}

	embed := &discordgo.MessageEmbed{
		Title:       locale.Text("gacha.custom.mycustoms_title.formatted", locale.Data{"Count": len(list)}),
		Description: b.String(),
		Color:       0x5865F2,
		Footer: &discordgo.MessageEmbedFooter{
			Text: locale.Text("gacha.custom.usage_footer.formatted", locale.Data{"Used": len(list)}),
		},
	}

	return &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{embed},
	}, nil
}

// HandleRemoveCustom removes a custom image by ID, freeing quota.
func HandleRemoveCustom(ctx context.Context, guildID, userID string, assetID int64, isAdmin bool) (*discordgo.MessageSend, error) {
	if Default == nil {
		return nil, errors.New("gacha store unavailable")
	}

	if err := Default.RemoveCustomImage(ctx, userID, assetID, isAdmin); err != nil {
		return nil, err
	}

	count, _ := Default.CountUserCustomImages(ctx, userID)

	embed := &discordgo.MessageEmbed{
		Title:       locale.Text("gacha.custom.removed_title"),
		Description: locale.Text("gacha.custom.removed_desc.formatted", locale.Data{"AssetID": assetID, "Used": count}),
		Color:       0xe74c3c,
		Footer: &discordgo.MessageEmbedFooter{
			Text: locale.Text("gacha.custom.usage_footer.formatted", locale.Data{"Used": count}),
		},
	}

	return &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{embed},
	}, nil
}

// HandleCustomImageSelect sets which gallery image is displayed for a character in the guild.
func HandleCustomImageSelect(ctx context.Context, guildID, userID, charQuery string, imageIndex int, isAdmin bool) (*discordgo.MessageSend, error) {
	if Default == nil || guildID == "" {
		return nil, userError(locale.Text("gacha.discord.use_this_command_in_a_server"))
	}

	card, _, err := Default.FindCharacter(ctx, guildID, charQuery)
	if err != nil {
		return nil, err
	}

	updated, err := Default.SetCharacterActiveImage(ctx, guildID, userID, card.ID, imageIndex, isAdmin)
	if err != nil {
		return nil, err
	}

	cardEmbed := Default.cardEmbed(*updated)
	cardEmbed.Title = locale.Text("gacha.custom.active_image_set_title") + " — " + updated.Name
	cardEmbed.Description = locale.Text("gacha.custom.active_image_set_desc.formatted", locale.Data{"Character": updated.Name, "Index": imageIndex}) + "\n\n" + cardEmbed.Description

	return &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{cardEmbed},
	}, nil
}

// HandleAssetReviewButton handles moderator clicks on the [Approve] or [Reject] buttons.
func HandleAssetReviewButton(s *discordgo.Session, i *discordgo.InteractionCreate, customID string) {
	if Default == nil || i.GuildID == "" || i.Member == nil {
		return
	}

	isAdmin := (i.Member.Permissions & (discordgo.PermissionAdministrator | discordgo.PermissionManageServer)) != 0
	if !isAdmin {
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: locale.Text("gacha.custom.review_admin_only"),
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	var action string
	var assetID int64
	if strings.HasPrefix(customID, "gacha_asset_approve_") {
		action = "approve"
		idStr := strings.TrimPrefix(customID, "gacha_asset_approve_")
		assetID, _ = strconv.ParseInt(idStr, 10, 64)
	} else if strings.HasPrefix(customID, "gacha_asset_reject_") {
		action = "reject"
		idStr := strings.TrimPrefix(customID, "gacha_asset_reject_")
		assetID, _ = strconv.ParseInt(idStr, 10, 64)
	} else {
		return
	}

	if assetID <= 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var summary *CustomAssetSummary
	var err error
	if action == "approve" {
		summary, err = Default.ApproveCustomImage(ctx, i.Member.User.ID, assetID)
	} else {
		summary, err = Default.RejectCustomImage(ctx, i.Member.User.ID, assetID)
	}

	if err != nil {
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: fmt.Sprintf("❌ %s", err.Error()),
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	// Update the original message
	origEmbeds := i.Message.Embeds
	var updatedEmbed *discordgo.MessageEmbed
	if len(origEmbeds) > 0 {
		updatedEmbed = origEmbeds[0]
	} else {
		updatedEmbed = &discordgo.MessageEmbed{}
	}

	if action == "approve" {
		updatedEmbed.Color = 0x2ecc71
		updatedEmbed.Description += "\n\n" + locale.Text("gacha.custom.review_approved.formatted", locale.Data{"User": i.Member.User.ID})
	} else {
		updatedEmbed.Color = 0xe74c3c
		updatedEmbed.Description += "\n\n" + locale.Text("gacha.custom.review_rejected.formatted", locale.Data{"User": i.Member.User.ID})
	}

	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{updatedEmbed},
			Components: []discordgo.MessageComponent{}, // remove buttons
		},
	})

	_ = summary
}

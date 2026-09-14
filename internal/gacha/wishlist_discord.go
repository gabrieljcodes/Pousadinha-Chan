package gacha

import (
	"bot/internal/locale"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

func wishlistResultMessage(result WishBatchResult, remove bool) *discordgo.MessageSend {
	msg := &discordgo.MessageSend{AllowedMentions: &discordgo.MessageAllowedMentions{}}
	if len(result.Changed) == 1 && len(result.Unchanged)+len(result.Missing)+len(result.Ambiguous)+len(result.Overflow) == 0 {
		entry := result.Changed[0]
		if remove {
			msg.Content = locale.Text("gacha.discord.character_removed_from_your_wishlist.formatted", locale.Data{"Id": entry.ID, "Name": safe(entry.Name)})
		} else {
			msg.Content = locale.Text("gacha.discord.character_added_to_your_wishlist_its_rolls.formatted", locale.Data{"Id": entry.ID, "Name": safe(entry.Name)})
		}
		return msg
	}
	if len(result.Unchanged) == 1 && len(result.Changed)+len(result.Missing)+len(result.Ambiguous)+len(result.Overflow) == 0 {
		entry := result.Unchanged[0]
		msg.Content = locale.Text("gacha.wishlist.already", locale.Data{"Id": entry.ID, "Name": safe(entry.Name)})
		return msg
	}
	var body strings.Builder
	entries := func(title string, list []WishEntry) {
		if len(list) == 0 {
			return
		}
		body.WriteString("**" + title + "**\n")
		for _, entry := range list {
			fmt.Fprintf(&body, "`%d` %s\n", entry.ID, clip(safe(entry.Name), 60))
		}
		body.WriteByte('\n')
	}
	skipped := func(title string, list []string) {
		if len(list) == 0 {
			return
		}
		body.WriteString("**" + title + "**\n")
		for _, query := range list {
			body.WriteString(clip(safe(query), 60) + "\n")
		}
		body.WriteByte('\n')
	}
	if remove {
		entries(locale.Text("gacha.wishlist.removed"), result.Changed)
	} else {
		entries(locale.Text("gacha.wishlist.added"), result.Changed)
	}
	entries(locale.Text("gacha.wishlist.unchanged"), result.Unchanged)
	if remove {
		skipped(locale.Text("gacha.wishlist.not_in_list"), result.Missing)
	} else {
		skipped(locale.Text("gacha.wishlist.not_found"), result.Missing)
	}
	skipped(locale.Text("gacha.wishlist.ambiguous"), result.Ambiguous)
	skipped(locale.Text("gacha.wishlist.overflow"), result.Overflow)
	msg.Embeds = []*discordgo.MessageEmbed{{Title: locale.Text("gacha.wishlist.updated"), Description: clip(body.String(), 3500), Color: 0x9b59b6, Footer: &discordgo.MessageEmbedFooter{Text: locale.Plural("gacha.wishlist.count", result.Count, locale.Data{"Count": result.Count, "Limit": result.Limit})}}}
	return msg
}

func (s *Store) wishlistClearMessage(ctx context.Context, guild, channel, user string) (*discordgo.MessageSend, error) {
	token, count, err := s.PrepareWishlistClear(ctx, guild, channel, user)
	if err != nil {
		return nil, err
	}
	msg := &discordgo.MessageSend{AllowedMentions: &discordgo.MessageAllowedMentions{}}
	if count == 0 {
		msg.Content = locale.Text("gacha.wishlist.empty")
		return msg, nil
	}
	msg.Content = locale.Text("gacha.wishlist.confirm_clear", locale.Data{"Count": count})
	prefix := "gacha_wishclear:" + user + ":" + token + ":"
	msg.Components = []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{
		discordgo.Button{Label: locale.Text("gacha.wishlist.confirm_button"), Style: discordgo.DangerButton, CustomID: prefix + "confirm"},
		discordgo.Button{Label: locale.Text("gacha.wishlist.cancel_button"), Style: discordgo.SecondaryButton, CustomID: prefix + "cancel"},
	}}}
	return msg, nil
}

func HandleWishlistClear(session *discordgo.Session, i *discordgo.InteractionCreate) {
	if i == nil || i.Interaction == nil || i.Member == nil || i.Member.User == nil || i.GuildID == "" || Default == nil {
		return
	}
	parts := strings.Split(i.MessageComponentData().CustomID, ":")
	if len(parts) != 4 || parts[0] != "gacha_wishclear" || (parts[3] != "confirm" && parts[3] != "cancel") {
		return
	}
	if parts[1] != i.Member.User.ID {
		_ = session.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{Type: discordgo.InteractionResponseChannelMessageWithSource, Data: &discordgo.InteractionResponseData{Content: locale.Text("gacha.wishlist.confirmation_owner"), Flags: discordgo.MessageFlagsEphemeral}})
		return
	}
	if err := session.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{Type: discordgo.InteractionResponseDeferredMessageUpdate}); err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	removed, err := Default.ResolveWishlistClear(ctx, i.GuildID, i.ChannelID, i.Member.User.ID, parts[2], parts[3] == "confirm")
	content := locale.Text("gacha.wishlist.cancelled")
	components := []discordgo.MessageComponent{}
	if err != nil {
		content = friendly(err)
		// Keep buttons on transient failures so the author can retry.
		if _, ok := err.(userError); !ok && i.Message != nil {
			components = i.Message.Components
		}
	} else if parts[3] == "confirm" {
		content = locale.Text("gacha.wishlist.cleared", locale.Data{"Count": removed})
	}
	embeds := []*discordgo.MessageEmbed{}
	_, _ = session.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Content: &content, Embeds: &embeds, Components: &components, AllowedMentions: &discordgo.MessageAllowedMentions{}})
}

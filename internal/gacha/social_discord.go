package gacha

import (
	"context"
	"fmt"
	"github.com/bwmarrin/discordgo"
	"github.com/google/uuid"
	"strconv"
	"strings"
	"time"
)

func parseMember(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "<@") && strings.HasSuffix(raw, ">") {
		raw = strings.TrimSuffix(strings.TrimPrefix(raw, "<@"), ">")
		raw = strings.TrimPrefix(raw, "!")
	}
	if len(raw) < 15 || len(raw) > 20 {
		return "", userError("Mention a server member or use their Discord user ID.")
	}
	if _, e := strconv.ParseUint(raw, 10, 64); e != nil {
		return "", userError("Invalid Discord user ID.")
	}
	return raw, nil
}
func parseOffer(kind, query string) (string, int64, int64, error) {
	fields := strings.Fields(query)
	want := 1
	if kind == "gift" {
		want = 2
	}
	if kind == "trade" {
		want = 3
	}
	if len(fields) != want {
		return "", 0, 0, userError("Usage: !divorce <ID> · !gift @member <ID> · !trade @member <your ID> <their ID>")
	}
	recipient := ""
	offset := 0
	var e error
	if kind != "divorce" {
		recipient, e = parseMember(fields[0])
		if e != nil {
			return "", 0, 0, e
		}
		offset = 1
	}
	offered, e := strconv.ParseInt(fields[offset], 10, 64)
	if e != nil || offered <= 0 {
		return "", 0, 0, invalidID()
	}
	var requested int64
	if kind == "trade" {
		requested, e = strconv.ParseInt(fields[2], 10, 64)
		if e != nil || requested <= 0 {
			return "", 0, 0, invalidID()
		}
	}
	return recipient, offered, requested, nil
}
func parseText(args []string) (string, string, int, error) {
	if len(args) == 0 {
		return "help", "", 1, nil
	}
	action := normalizeAction(args[0])
	rest := args[1:]
	page := 1
	if action == "harem" || action == "top" || action == "gallery" || strings.HasPrefix(action, "topchar") {
		minArgs := 0
		if action == "gallery" {
			minArgs = 1
		}
		if len(rest) > minArgs {
			last := rest[len(rest)-1]
			if strings.HasPrefix(action, "topchar") {
				if n, err := strconv.Atoi(last); err == nil && n >= 1 && n <= 100000 {
					page = n
					rest = rest[:len(rest)-1]
				}
			} else if len(last) < 15 && !strings.HasPrefix(last, "<@") {
				n, e := strconv.Atoi(last)
				if e != nil || n < 1 || n > 100000 {
					return "", "", 0, userError("Page must be between 1 and 100000.")
				}
				page = n
				rest = rest[:len(rest)-1]
			}
		}
	}
	return action, strings.Join(rest, " "), page, nil
}
func validateTarget(s *discordgo.Session, guild, user, action, query string) error {
	if action != "trade" && action != "gift" {
		return nil
	}
	target, _, _, e := parseOffer(action, query)
	if e != nil {
		return e
	}
	if target == user {
		return userError("Choose another server member.")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	member, e := s.GuildMember(guild, target, discordgo.WithContext(ctx))
	if e != nil || member == nil || member.User == nil {
		return userError("The recipient must be a current member of this server.")
	}
	if member.User.Bot {
		return userError("You cannot trade with or gift to a bot.")
	}
	return nil
}
func (s *Store) actionMessage(ctx context.Context, a Action) (*discordgo.MessageSend, error) {
	offered, e := scanCard(s.DB.QueryRowContext(ctx, cardSelect+` WHERE c.id=$1`, a.Offered))
	if e != nil {
		return nil, e
	}
	title, description := "Confirm divorce", fmt.Sprintf("Release **%s** (#%d) and receive **%d coins** in this server.\nThis removes the character from your harem and resets its keys. The quoted payout is fixed until expiry.", clip(safe(offered.Name), 180), a.Offered, a.Payout)
	if a.Kind == "gift" {
		title = "Character gift"
		description = fmt.Sprintf("<@%s> offers **%s** (#%d) to <@%s>.\nThe recipient must accept.", a.Proposer, clip(safe(offered.Name), 180), a.Offered, a.Recipient)
	}
	if a.Kind == "trade" {
		wanted, e := scanCard(s.DB.QueryRowContext(ctx, cardSelect+` WHERE c.id=$1`, a.Requested))
		if e != nil {
			return nil, e
		}
		if e = priceCards(ctx, s.DB, a.Guild, &offered, &wanted); e != nil {
			return nil, e
		}
		title = "Character trade"
		description = fmt.Sprintf("<@%s> offers **%s** (#%d, %s)\nfor <@%s>'s **%s** (#%d, %s).\nBoth characters change owners together on acceptance.", a.Proposer, clip(safe(offered.Name), 180), a.Offered, valueLabel(offered), a.Recipient, clip(safe(wanted.Name), 180), a.Requested, valueLabel(wanted))
	}
	status := a.Status
	if status == "pending" && !a.Expires.After(time.Now()) {
		status = "expired"
	}
	description += fmt.Sprintf("\n\n**Status: %s** · expires <t:%d:R>", status, a.Expires.Unix())
	embed := &discordgo.MessageEmbed{Title: title, Description: description, Color: 0xc5a66b, Footer: &discordgo.MessageEmbedFooter{Text: "Offer " + a.ID}}
	buttons := []discordgo.MessageComponent{}
	if status == "pending" {
		label := "Accept"
		style := discordgo.SuccessButton
		if a.Kind == "divorce" {
			label = fmt.Sprintf("Divorce for %d coins", a.Payout)
			style = discordgo.DangerButton
		}
		buttons = append(buttons, discordgo.Button{Label: label, Style: style, CustomID: "gacha_action_accept_" + a.ID})
		if a.Kind != "divorce" {
			buttons = append(buttons, discordgo.Button{Label: "Decline", Style: discordgo.SecondaryButton, CustomID: "gacha_action_decline_" + a.ID})
		}
		buttons = append(buttons, discordgo.Button{Label: "Cancel", Style: discordgo.SecondaryButton, CustomID: "gacha_action_cancel_" + a.ID})
	}
	components := []discordgo.MessageComponent{}
	if len(buttons) > 0 {
		components = append(components, discordgo.ActionsRow{Components: buttons})
	}
	return &discordgo.MessageSend{Embeds: []*discordgo.MessageEmbed{embed}, Components: components, AllowedMentions: &discordgo.MessageAllowedMentions{}}, nil
}
func HandleAction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if Default == nil || i.Member == nil || i.Member.User == nil {
		return
	}
	if e := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{Type: discordgo.InteractionResponseDeferredChannelMessageWithSource, Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral}}); e != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	parts := strings.SplitN(strings.TrimPrefix(i.MessageComponentData().CustomID, "gacha_action_"), "_", 2)
	content := "Invalid offer button."
	if len(parts) == 2 {
		if _, e := uuid.Parse(parts[1]); e == nil {
			a, e := Default.ResolveAction(ctx, i.GuildID, i.ChannelID, i.Member.User.ID, parts[1], parts[0])
			if e != nil {
				content = friendly(e)
			} else {
				content = "Offer status: " + a.Status + "."
				if a.Kind == "divorce" && a.Status == "completed" {
					content = fmt.Sprintf("Divorce completed. %d coins credited to your wallet in this server.", a.Payout)
				}
				if msg, e := Default.actionMessage(ctx, a); e == nil && i.Message != nil {
					_, _ = s.ChannelMessageEditComplex(&discordgo.MessageEdit{ID: i.Message.ID, Channel: i.ChannelID, Embeds: &msg.Embeds, Components: &msg.Components, AllowedMentions: msg.AllowedMentions})
				}
			}
		}
	}
	_, _ = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Content: &content})
}

func navigation(action, subject string, page int, next bool) []discordgo.MessageComponent {
	prev := discordgo.Button{Label: "◀ Anterior", Style: discordgo.SecondaryButton, CustomID: fmt.Sprintf("gacha_page_%s_%s_%d", action, subject, max(1, page-1)), Disabled: page <= 1}
	nextBtn := discordgo.Button{Label: "Próximo ▶", Style: discordgo.SecondaryButton, CustomID: fmt.Sprintf("gacha_page_%s_%s_%d", action, subject, page+1), Disabled: !next || page >= 100000}
	if action == "harem" {
		visualBtn := discordgo.Button{Label: "📷 Ver Fotos", Style: discordgo.PrimaryButton, CustomID: fmt.Sprintf("gacha_page_haremvisual_%s_1", subject)}
		return []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{prev, visualBtn, nextBtn}}}
	}
	return []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{prev, nextBtn}}}
}

func navigationTopChar(claim, gender string, page int, next bool) []discordgo.MessageComponent {
	if claim == "" {
		claim = "all"
	}
	if gender == "" {
		gender = "all"
	}
	prev := discordgo.Button{Label: "◀ Anterior", Style: discordgo.SecondaryButton, CustomID: fmt.Sprintf("gacha_page_topchar_%s_%s_%d", claim, gender, max(1, page-1)), Disabled: page <= 1}
	nextBtn := discordgo.Button{Label: "Próximo ▶", Style: discordgo.SecondaryButton, CustomID: fmt.Sprintf("gacha_page_topchar_%s_%s_%d", claim, gender, page+1), Disabled: !next || page >= 100000}
	return []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{prev, nextBtn}}}
}

func HandlePage(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if Default == nil || i.Member == nil || i.Member.User == nil {
		return
	}
	// The first click opens a private copy so browsing cannot change another member's view.
	responseType := discordgo.InteractionResponseDeferredChannelMessageWithSource
	if i.Message != nil && i.Message.Flags&discordgo.MessageFlagsEphemeral != 0 {
		responseType = discordgo.InteractionResponseDeferredMessageUpdate
	}
	data := &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral}
	if responseType == discordgo.InteractionResponseDeferredMessageUpdate {
		data = nil
	}
	if e := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{Type: responseType, Data: data}); e != nil {
		return
	}
	parts := strings.Split(strings.TrimPrefix(i.MessageComponentData().CustomID, "gacha_page_"), "_")
	content := "Invalid page button."

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	if len(parts) == 4 && parts[0] == "topchar" {
		if page, e := strconv.Atoi(parts[3]); e == nil && page > 0 && page <= 100000 {
			msg, e := Default.Execute(ctx, i.GuildID, i.ChannelID, i.Member.User.ID, i.ID, "topchar", parts[1]+" "+parts[2], page)
			if e == nil {
				_, _ = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Content: &msg.Content, Embeds: &msg.Embeds, Components: &msg.Components, AllowedMentions: msg.AllowedMentions})
				return
			}
			content = friendly(e)
		}
	} else if len(parts) == 3 {
		action := parts[0]
		subject := parts[1]
		page, e := strconv.Atoi(parts[2])
		if e == nil && page > 0 && page <= 100000 {
			targetAction := action
			targetQuery := subject
			if action == "haremlist" {
				targetAction = "harem"
				targetQuery = subject
			} else if action == "haremvisual" {
				targetAction = "harem_visual"
				targetQuery = subject
			}
			msg, e := Default.Execute(ctx, i.GuildID, i.ChannelID, i.Member.User.ID, i.ID, targetAction, targetQuery, page)
			if e == nil {
				_, _ = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Content: &msg.Content, Embeds: &msg.Embeds, Components: &msg.Components, AllowedMentions: msg.AllowedMentions})
				return
			}
			content = friendly(e)
		}
	}
	_, _ = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Content: &content})
}

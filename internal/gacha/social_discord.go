package gacha

import (
	"bot/internal/locale"
	"context"
	"database/sql"
	"errors"
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
		return "", userError(locale.Text("gacha.social_discord.mention_a_server_member_or_use_their"))
	}
	if _, e := strconv.ParseUint(raw, 10, 64); e != nil {
		return "", userError(locale.Text("gacha.social_discord.invalid_discord_user_id"))
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
		return "", 0, 0, userError(locale.Text("gacha.social_discord.usage_divorce_id_gift_member_id_trade"))
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

func parseRecipient(kind, query string) (string, error) {
	if kind == "divorce" {
		return "", nil
	}
	query = strings.TrimSpace(query)
	var first string
	if strings.Contains(query, " | ") {
		parts := strings.Split(query, " | ")
		first = strings.TrimSpace(parts[0])
	} else {
		fields := strings.Fields(query)
		if len(fields) == 0 {
			return "", userError(locale.Text("gacha.social_discord.usage_divorce_id_gift_member_id_trade"))
		}
		first = fields[0]
	}
	return parseMember(first)
}

func validateTarget(s *discordgo.Session, guild, user, action, query string) error {
	if action != "trade" && action != "gift" {
		return nil
	}
	target, e := parseRecipient(action, query)
	if e != nil {
		return e
	}
	if target == user {
		return userError(locale.Text("gacha.social.choose_another_server_member"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	member, e := s.GuildMember(guild, target, discordgo.WithContext(ctx))
	if e != nil || member == nil || member.User == nil {
		return userError(locale.Text("gacha.social_discord.the_recipient_must_be_a_current_member"))
	}
	if member.User.Bot {
		return userError(locale.Text("gacha.social_discord.you_cannot_trade_with_or_gift_to"))
	}
	return nil
}

// ResolveCharacterID resolves a character query (numeric ID, exact name, alias, or substring) to an int64 ID.
// If prioritizeOwnerHarem is true and owner is non-empty, it first searches characters owned by that user in that guild.
func (s *Store) ResolveCharacterID(ctx context.Context, guild, owner, query string, prioritizeOwnerHarem bool) (int64, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return 0, userError(locale.Text("gacha.game.enter_a_character_name_or_id"))
	}

	// 1. Numeric ID
	if id, err := strconv.ParseInt(query, 10, 64); err == nil && id > 0 {
		return id, nil
	}

	// 2. Prioritize owner's harem if requested
	if prioritizeOwnerHarem && owner != "" {
		const haremSearchSQL = `
SELECT c.id
FROM gacha_collection col
JOIN gacha_characters c ON c.id = col.character_id
LEFT JOIN gacha_guild_character_aliases ga ON ga.character_id = c.id AND ga.guild_id = $1
WHERE col.guild_id = $1 AND col.user_id = $2
  AND (
    lower(COALESCE(ga.alias, c.name)) = lower($3)
    OR ga.alias ILIKE '%' || $3 || '%'
    OR lower(c.name) = lower($3)
    OR EXISTS(SELECT 1 FROM jsonb_array_elements_text(c.aliases) al WHERE lower(al) = lower($3))
    OR lower(c.name) LIKE lower($3) || '%'
    OR c.name ILIKE '%' || $3 || '%'
    OR c.native_name ILIKE '%' || $3 || '%'
    OR EXISTS(SELECT 1 FROM jsonb_array_elements_text(c.aliases) al WHERE al ILIKE '%' || $3 || '%')
  )
ORDER BY
  CASE WHEN lower(COALESCE(ga.alias, c.name)) = lower($3) THEN 0
       WHEN lower(c.name) = lower($3) THEN 1
       WHEN EXISTS(SELECT 1 FROM jsonb_array_elements_text(c.aliases) al WHERE lower(al) = lower($3)) THEN 2
       WHEN lower(c.name) LIKE lower($3) || '%' THEN 3
       ELSE 4 END,
  c.favourites DESC, c.id ASC
LIMIT 1`

		var haremID int64
		if err := s.DB.QueryRowContext(ctx, haremSearchSQL, guild, owner, query).Scan(&haremID); err == nil {
			return haremID, nil
		}
	}


	// 3. Fall back to catalog search via FindCharacter
	card, _, err := s.FindCharacter(ctx, guild, query)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, userError(locale.Text("gacha.discord.character_not_found_try_another_name_with.formatted", locale.Data{"Query": query}))
		}
		return 0, err
	}
	return card.ID, nil
}

// ResolveOffer resolves the character names/aliases or numeric IDs for divorce, gift, or trade.
func (s *Store) ResolveOffer(ctx context.Context, guild, user, kind, query string) (string, int64, int64, error) {
	if kind == "divorce" {
		offered, err := s.ResolveCharacterID(ctx, guild, user, query, true)
		if err != nil {
			return "", 0, 0, err
		}
		return "", offered, 0, nil
	}

	if kind == "gift" {
		var recipient, charQuery string
		if strings.Contains(query, " | ") {
			parts := strings.SplitN(query, " | ", 2)
			recipient = strings.TrimSpace(parts[0])
			charQuery = strings.TrimSpace(parts[1])
		} else {
			fields := strings.Fields(query)
			if len(fields) < 2 {
				return "", 0, 0, userError(locale.Text("gacha.social_discord.usage_divorce_id_gift_member_id_trade"))
			}
			recipient = fields[0]
			charQuery = strings.TrimSpace(query[len(fields[0]):])
		}
		recID, err := parseMember(recipient)
		if err != nil {
			return "", 0, 0, err
		}
		offered, err := s.ResolveCharacterID(ctx, guild, user, charQuery, true)
		if err != nil {
			return "", 0, 0, err
		}
		return recID, offered, 0, nil
	}

	if kind == "trade" {
		var recipient, offerQuery, receiveQuery string
		if strings.Contains(query, " | ") {
			parts := strings.Split(query, " | ")
			if len(parts) < 3 {
				return "", 0, 0, userError(locale.Text("gacha.social_discord.usage_divorce_id_gift_member_id_trade"))
			}
			recipient = strings.TrimSpace(parts[0])
			offerQuery = strings.TrimSpace(parts[1])
			receiveQuery = strings.TrimSpace(parts[2])
		} else {
			fields := strings.Fields(query)
			if len(fields) < 3 {
				return "", 0, 0, userError(locale.Text("gacha.social_discord.usage_divorce_id_gift_member_id_trade"))
			}
			recipient = fields[0]
			offerQuery = fields[1]
			receiveQuery = strings.Join(fields[2:], " ")
		}
		recID, err := parseMember(recipient)
		if err != nil {
			return "", 0, 0, err
		}
		offered, err := s.ResolveCharacterID(ctx, guild, user, offerQuery, true)
		if err != nil {
			return "", 0, 0, err
		}
		requested, err := s.ResolveCharacterID(ctx, guild, recID, receiveQuery, true)
		if err != nil {
			return "", 0, 0, err
		}
		return recID, offered, requested, nil
	}

	return "", 0, 0, userError(locale.Text("gacha.social_discord.usage_divorce_id_gift_member_id_trade"))
}
func (s *Store) actionMessage(ctx context.Context, a Action) (*discordgo.MessageSend, error) {
	offered, e := scanCard(s.DB.QueryRowContext(ctx, cardSelect+` WHERE c.id=$1`, a.Offered))
	if e != nil {
		return nil, e
	}
	title, description := locale.Text("gacha.social_discord.confirm_divorce"), locale.Text("gacha.social_discord.release_and_receive_coins_in_this_server.formatted", locale.Data{"Value1": clip(safe(offered.Name), 180), "Offered": a.Offered, "Payout": a.Payout})
	if a.Kind == "gift" {
		title = locale.Text("gacha.social_discord.character_gift")
		description = locale.Text("gacha.social_discord.offers_to_the_recipient_must_accept.formatted", locale.Data{"Proposer": a.Proposer, "Value2": clip(safe(offered.Name), 180), "Offered": a.Offered, "Recipient": a.Recipient})
	}
	if a.Kind == "trade" {
		wanted, e := scanCard(s.DB.QueryRowContext(ctx, cardSelect+` WHERE c.id=$1`, a.Requested))
		if e != nil {
			return nil, e
		}
		if e = priceCards(ctx, s.DB, a.Guild, &offered, &wanted); e != nil {
			return nil, e
		}
		title = locale.Text("gacha.social_discord.character_trade")
		description = locale.Text("gacha.social_discord.offers_for_s_both_characters_change_owners.formatted", locale.Data{"Proposer": a.Proposer, "Value2": clip(safe(offered.Name), 180), "Offered": a.Offered, "Value4": valueLabel(offered), "Recipient": a.Recipient, "Value6": clip(safe(wanted.Name), 180), "Requested": a.Requested, "Value8": valueLabel(wanted)})
	}
	status := a.Status
	if status == "pending" && !a.Expires.After(time.Now()) {
		status = "expired"
	}
	description += locale.Text("gacha.social_discord.status_expires_t_r.formatted", locale.Data{"Status": status, "Expires": a.Expires.Unix()})
	embed := &discordgo.MessageEmbed{Title: title, Description: description, Color: 0xc5a66b, Footer: &discordgo.MessageEmbedFooter{Text: locale.Text("gacha.social_discord.offer") + a.ID}}
	buttons := []discordgo.MessageComponent{}
	if status == "pending" {
		label := locale.Text("gacha.social_discord.accept")
		style := discordgo.SuccessButton
		if a.Kind == "divorce" {
			label = locale.Text("gacha.social_discord.divorce_for_coins.formatted", locale.Data{"Payout": a.Payout})
			style = discordgo.DangerButton
		}
		buttons = append(buttons, discordgo.Button{Label: label, Style: style, CustomID: "gacha_action_accept_" + a.ID})
		if a.Kind != "divorce" {
			buttons = append(buttons, discordgo.Button{Label: locale.Text("gacha.social_discord.decline"), Style: discordgo.SecondaryButton, CustomID: "gacha_action_decline_" + a.ID})
		}
		buttons = append(buttons, discordgo.Button{Label: locale.Text("gacha.social_discord.cancel"), Style: discordgo.SecondaryButton, CustomID: "gacha_action_cancel_" + a.ID})
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
	content := locale.Text("gacha.social_discord.invalid_offer_button")
	if len(parts) == 2 {
		if _, e := uuid.Parse(parts[1]); e == nil {
			a, e := Default.ResolveAction(ctx, i.GuildID, i.ChannelID, i.Member.User.ID, parts[1], parts[0])
			if e != nil {
				content = friendly(e)
			} else {
				content = locale.Text("gacha.social_discord.offer_status") + a.Status + "."
				if a.Kind == "divorce" && a.Status == "completed" {
					content = locale.Text("gacha.social_discord.divorce_completed_coins_credited_to_your_wallet.formatted", locale.Data{"Payout": a.Payout})
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
	prev := discordgo.Button{Label: locale.Text("gacha.discord.previous"), Style: discordgo.SecondaryButton, CustomID: fmt.Sprintf("gacha_page_%s_%s_%d", action, subject, max(1, page-1)), Disabled: page <= 1}
	nextBtn := discordgo.Button{Label: locale.Text("gacha.discord.next"), Style: discordgo.SecondaryButton, CustomID: fmt.Sprintf("gacha_page_%s_%s_%d", action, subject, page+1), Disabled: !next || page >= 100000}
	if action == "harem" {
		visualBtn := discordgo.Button{Label: locale.Text("gacha.social_discord.photo_view"), Style: discordgo.PrimaryButton, CustomID: fmt.Sprintf("gacha_page_haremvisual_%s_1", subject)}
		return []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{prev, visualBtn, nextBtn}}}
	}
	if action == "harem_visual" {
		listBtn := discordgo.Button{Label: locale.Text("gacha.social_discord.list_view"), Style: discordgo.PrimaryButton, CustomID: fmt.Sprintf("gacha_page_haremlist_%s_1", subject)}
		return []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{prev, listBtn, nextBtn}}}
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

	allBtn := discordgo.Button{
		Label:    locale.Text("gacha.social_discord.all"),
		Style:    discordgo.SecondaryButton,
		CustomID: fmt.Sprintf("gacha_page_topfilter_%s_all_1", claim),
	}
	if gender == "all" {
		allBtn.Style = discordgo.PrimaryButton
	}

	waifuBtn := discordgo.Button{
		Label:    locale.Text("gacha.social_discord.waifus"),
		Style:    discordgo.SecondaryButton,
		CustomID: fmt.Sprintf("gacha_page_topfilter_%s_female_1", claim),
	}
	if gender == "female" {
		waifuBtn.Style = discordgo.PrimaryButton
	}

	husbandoBtn := discordgo.Button{
		Label:    locale.Text("gacha.social_discord.husbandos"),
		Style:    discordgo.SecondaryButton,
		CustomID: fmt.Sprintf("gacha_page_topfilter_%s_male_1", claim),
	}
	if gender == "male" {
		husbandoBtn.Style = discordgo.PrimaryButton
	}

	nextClaim := "unclaimed"
	claimLabel := locale.Text("gacha.social_discord.unclaimed_only")
	claimStyle := discordgo.SecondaryButton
	if claim == "unclaimed" {
		nextClaim = "all"
		claimLabel = locale.Text("gacha.social_discord.show_all")
		claimStyle = discordgo.SuccessButton
	}
	claimBtn := discordgo.Button{
		Label:    claimLabel,
		Style:    claimStyle,
		CustomID: fmt.Sprintf("gacha_page_topfilter_%s_%s_1", nextClaim, gender),
	}

	filterRow := discordgo.ActionsRow{
		Components: []discordgo.MessageComponent{allBtn, waifuBtn, husbandoBtn, claimBtn},
	}

	prev := discordgo.Button{Label: locale.Text("gacha.discord.previous"), Style: discordgo.SecondaryButton, CustomID: fmt.Sprintf("gacha_page_topnav_%s_%s_%d", claim, gender, max(1, page-1)), Disabled: page <= 1}
	nextBtn := discordgo.Button{Label: locale.Text("gacha.discord.next"), Style: discordgo.SecondaryButton, CustomID: fmt.Sprintf("gacha_page_topnav_%s_%s_%d", claim, gender, page+1), Disabled: !next || page >= 100000}
	navRow := discordgo.ActionsRow{
		Components: []discordgo.MessageComponent{prev, nextBtn},
	}

	return []discordgo.MessageComponent{filterRow, navRow}
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
	content := locale.Text("gacha.social_discord.invalid_page_button")

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	if len(parts) >= 3 && parts[0] == "search" {
		pageStr := parts[len(parts)-1]
		searchQuery := strings.Join(parts[1:len(parts)-1], "_")
		if page, e := strconv.Atoi(pageStr); e == nil && page > 0 && page <= 100000 {
			msg, e := Default.Execute(ctx, i.GuildID, i.ChannelID, i.Member.User.ID, i.ID, "search", searchQuery, page)
			if e == nil {
				_, _ = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Content: &msg.Content, Embeds: &msg.Embeds, Components: &msg.Components, AllowedMentions: msg.AllowedMentions})
				return
			}
			content = friendly(e)
		}
	} else if len(parts) == 4 && (parts[0] == "topchar" || parts[0] == "topfilter" || parts[0] == "topnav") {
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

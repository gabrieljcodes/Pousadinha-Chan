package gacha

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/bwmarrin/discordgo"
	"log"
	"strconv"
	"strings"
	"time"
)

var Default *Store

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
func safe(s string) string {
	return strings.NewReplacer("@", "＠", "*", "", "_", "", "`", "", "~", "", "[", "", "]", "").Replace(s)
}
func (s *Store) cardEmbed(c Card) *discordgo.MessageEmbed {
	val := c.Value
	if val <= 0 {
		val = CharacterValue(c.Favourites)
	}
	var desc strings.Builder
	if c.Work != "" {
		desc.WriteString(clip(safe(c.Work), 300) + "\n")
	}
	if c.Favourites > 0 {
		desc.WriteString(fmt.Sprintf("Likes: #%d\n", c.Favourites))
	}
	desc.WriteString(fmt.Sprintf("**%d** 🪙", val))
	if c.Owner != "" {
		desc.WriteString(fmt.Sprintf("\nBelongs to <@%s>", c.Owner))
	}

	footerText := fmt.Sprintf("%s - %d coins", clip(c.Name, 35), val)
	if c.Work != "" {
		footerText = fmt.Sprintf("%s / %s - %d coins", clip(c.Name, 25), clip(c.Work, 25), val)
	}

	e := &discordgo.MessageEmbed{
		Title:       clip(c.Name, 200),
		Description: desc.String(),
		Color:       0xe67e22,
		Footer:      &discordgo.MessageEmbedFooter{Text: footerText},
	}
	if c.Image != "" {
		e.Image = &discordgo.MessageEmbedImage{URL: s.Config.PublicURL + "/" + c.Image}
	}
	return e
}
func friendly(e error) string {
	var input userError
	if errors.As(e, &input) {
		return input.Error()
	}
	if errors.Is(e, sql.ErrNoRows) {
		return "Character not found."
	}
	if errors.Is(e, ErrLimit) || errors.Is(e, ErrEmpty) || errors.Is(e, ErrClaim) {
		return e.Error()
	}
	log.Printf("[gacha] %v", e)
	return "Something went wrong. Please try again."
}
func (s *Store) Execute(ctx context.Context, guild, channel, user, request, action, query string, page int) (*discordgo.MessageSend, error) {
	if page < 1 || page > 100000 {
		return nil, userError("Invalid page.")
	}
	if guild == "" {
		return nil, userError("Use this command in a server.")
	}
	action = normalizeAction(action)
	msg := &discordgo.MessageSend{AllowedMentions: &discordgo.MessageAllowedMentions{}}
	switch action {
	case "roll", "w", "h", "wa", "ha", "ma", "wg", "hg", "mg":
		r, e := s.RollPool(ctx, guild, channel, user, request, action)
		if e != nil {
			return nil, e
		}
		embed := s.cardEmbed(r.Card)
		if r.KeyEarned {
			embed.Description += fmt.Sprintf("\n🔑 **+1 key** (Total: %d)", r.Card.Keys)
		}
		var wished bool
		if e = s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM gacha_wishes WHERE guild_id=$1 AND character_id=$2)`, guild, r.Card.ID).Scan(&wished); e != nil {
			return nil, e
		}
		if wished {
			embed.Description += "\n✦ Wished in this server"
		}
		msg.Embeds = []*discordgo.MessageEmbed{embed}
		msg.Components = []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{discordgo.Button{Label: "Claim character", Style: discordgo.SuccessButton, CustomID: "gacha_claim_" + r.ID, Disabled: r.Card.Owner != "" || !r.Expires.After(time.Now())}}}}
	case "harem", "harem_visual", "search":
		owner := ""
		isVisual := action == "harem_visual"
		cleanQuery := query

		if action == "harem" || action == "harem_visual" {
			owner = user
			fields := strings.Fields(query)
			var rest []string
			for _, f := range fields {
				lf := strings.ToLower(f)
				if lf == "-i" || lf == "-v" || lf == "img" || lf == "card" || lf == "visual" || lf == "foto" || lf == "fotos" {
					isVisual = true
				} else if strings.HasPrefix(f, "<@") || (len(f) >= 15 && len(f) <= 20) {
					parsedOwner, err := parseMember(f)
					if err == nil {
						owner = parsedOwner
					}
				} else {
					rest = append(rest, f)
				}
			}
			cleanQuery = strings.Join(rest, " ")
		}

		if isVisual {
			index := page - 1
			if index < 0 {
				index = 0
			}
			c, total, err := s.HaremCardAt(ctx, guild, owner, index)
			if err != nil {
				return nil, err
			}
			if total == 0 {
				msg.Content = fmt.Sprintf("<@%s> doesn't have any characters in their harem yet.", owner)
				return msg, nil
			}
			embed := s.cardEmbed(c)
			embed.Footer = &discordgo.MessageEmbedFooter{
				Text: fmt.Sprintf("Personagem %d de %d • Harem de @%s", index+1, total, owner),
			}
			prevBtn := discordgo.Button{Label: "◀ Anterior", Style: discordgo.SecondaryButton, CustomID: fmt.Sprintf("gacha_page_haremvisual_%s_%d", owner, index), Disabled: index <= 0}
			listBtn := discordgo.Button{Label: "📝 Ver Lista", Style: discordgo.PrimaryButton, CustomID: fmt.Sprintf("gacha_page_haremlist_%s_1", owner)}
			nextBtn := discordgo.Button{Label: "Próximo ▶", Style: discordgo.SecondaryButton, CustomID: fmt.Sprintf("gacha_page_haremvisual_%s_%d", owner, index+2), Disabled: int64(index+1) >= total}
			msg.Components = []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{prevBtn, listBtn, nextBtn}}}
			msg.Embeds = []*discordgo.MessageEmbed{embed}
			return msg, nil
		}

		cards, e := s.Cards(ctx, guild, owner, cleanQuery, page)
		if e != nil {
			return nil, e
		}
		var b strings.Builder
		for _, c := range cards {
			fmt.Fprintf(&b, "`#%d` **%s** · %s · %d coins · 🔑 %d\n", c.ID, clip(safe(c.Name), 70), clip(safe(c.Work), 70), c.Value, c.Keys)
		}
		if b.Len() == 0 {
			b.WriteString("No characters found on this page.")
		}
		msg.Embeds = []*discordgo.MessageEmbed{{Title: fmt.Sprintf("Catalog • page %d", page), Description: b.String(), Color: 0xe67e22, Footer: &discordgo.MessageEmbedFooter{Text: "10 per page • use character <ID> for details"}}}
		if action == "harem" || action == "harem_visual" {
			count, value, e := s.HaremSummary(ctx, guild, owner)
			if e != nil {
				return nil, e
			}
			msg.Components = navigation("harem", owner, page, page*10 < count)
			msg.Embeds[0].Title = fmt.Sprintf("Harem • page %d", page)
			msg.Embeds[0].Description = fmt.Sprintf("<@%s> • **%d characters** • **%d coins**\n\n", owner, count, value) + msg.Embeds[0].Description
		}
	case "character", "gallery", "keys":
		var c Card
		if action == "character" {
			mainCard, alts, err := s.FindCharacter(ctx, guild, query)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return nil, userError(fmt.Sprintf("Personagem '%s' não encontrado. Use `$gacha search <nome>` para buscar.", query))
				}
				return nil, err
			}
			c = mainCard
			embed := s.cardEmbed(c)
			embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{Name: "Character value", Value: valueLabel(c), Inline: true})
			embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{Name: "Catalog", Value: fmt.Sprintf("#%d", c.ID), Inline: true})

			if c.Owner != "" {
				embed.Description += fmt.Sprintf("\n💍 Casado(a) com <@%s>", c.Owner)
			} else {
				embed.Description += "\n✨ Livre (Unclaimed neste servidor)"
			}

			var gender string
			var genres, studios, works string
			_ = s.DB.QueryRowContext(ctx, `SELECT gender FROM gacha_characters WHERE id=$1`, c.ID).Scan(&gender)
			_ = s.DB.QueryRowContext(ctx, `SELECT COALESCE(string_agg(DISTINCT w.title, ', '),''),COALESCE(string_agg(DISTINCT g.value, ', '),''),COALESCE(string_agg(DISTINCT st.value, ', '),'') FROM gacha_character_works cw JOIN gacha_works w ON w.id=cw.work_id LEFT JOIN LATERAL jsonb_array_elements_text(w.genres) g ON true LEFT JOIN LATERAL jsonb_array_elements_text(w.studios) st ON true WHERE cw.character_id=$1`, c.ID).Scan(&works, &genres, &studios)
			for _, v := range []struct{ name, value string }{{"Works", works}, {"Work genres", genres}, {"Studios", studios}, {"Character gender", gender}} {
				if v.value != "" {
					embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{Name: v.name, Value: clip(safe(v.value), 900)})
				}
			}

			if len(alts) > 0 {
				var altNames []string
				for _, alt := range alts {
					altNames = append(altNames, fmt.Sprintf("`#%d` %s", alt.ID, clip(safe(alt.Name), 25)))
				}
				embed.Footer.Text += " • Outros: " + strings.Join(altNames, ", ")
			}
			msg.Embeds = []*discordgo.MessageEmbed{embed}
			return msg, nil
		}

		id, e := strconv.ParseInt(query, 10, 64)
		if e != nil || id <= 0 {
			return nil, invalidID()
		}
		c, e = scanCard(s.DB.QueryRowContext(ctx, cardSelect+` WHERE c.id=$1`, id))
		if e != nil {
			return nil, e
		}
		if action == "gallery" {
			e = s.DB.QueryRowContext(ctx, `SELECT path,source_url,attribution FROM gacha_assets WHERE character_id=$1 AND status='approved' ORDER BY id OFFSET $2 LIMIT 1`, id, page-1).Scan(&c.Image, &c.Source, &c.Attribution)
			if e == sql.ErrNoRows {
				msg.Content = "There is no approved image on this page."
				return msg, nil
			}
			if e != nil {
				return nil, e
			}
		}
		if e = priceCards(ctx, s.DB, guild, &c); e != nil {
			return nil, e
		}
		embed := s.cardEmbed(c)
		if action == "keys" {
			embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{Name: "Key milestones", Value: fmt.Sprintf("+2%% per key, plus +10%% every 10 keys.\n%d keys to the next milestone. Keys follow trades/gifts and reset on divorce.", 10-c.Keys%10)})
		}
		if action == "gallery" {
			var total int
			if e = s.DB.QueryRowContext(ctx, `SELECT count(*) FROM gacha_assets WHERE character_id=$1 AND status='approved'`, id).Scan(&total); e != nil {
				return nil, e
			}
			embed.Footer.Text = fmt.Sprintf("Image %d of %d • Character #%d", page, total, id)
			msg.Components = navigation("gallery", strconv.FormatInt(id, 10), page, page < total)
		}
		msg.Embeds = []*discordgo.MessageEmbed{embed}
	case "topchar", "topchar_unclaimed", "topchar_waifu", "topchar_husbando":
		claimFilter := "all"
		genderFilter := ""
		if action == "topchar_unclaimed" {
			claimFilter = "unclaimed"
		} else if action == "topchar_waifu" {
			genderFilter = "female"
		} else if action == "topchar_husbando" {
			genderFilter = "male"
		}

		p := page
		parts := strings.Fields(strings.ToLower(query))
		for _, part := range parts {
			switch part {
			case "unclaimed", "u", "livres", "livre":
				claimFilter = "unclaimed"
			case "claimed", "c", "casados", "casado", "owned":
				claimFilter = "claimed"
			case "all", "todos", "tudo":
				claimFilter = "all"
			case "female", "w", "f", "waifu", "waifus", "mulher", "mulheres":
				genderFilter = "female"
			case "male", "h", "m", "husbando", "husbandos", "homem", "homens":
				genderFilter = "male"
			default:
				if n, err := strconv.Atoi(part); err == nil && n > 0 && n <= 100 {
					p = n
				}
			}
		}

		entries, total, err := s.TopCharacters(ctx, guild, claimFilter, genderFilter, p, 10)
		if err != nil {
			return nil, err
		}

		title := "Top Personagens"
		if claimFilter == "unclaimed" {
			title = "Top Personagens Livres (Unclaimed)"
		} else if claimFilter == "claimed" {
			title = "Top Personagens Casados"
		}
		if genderFilter == "female" {
			title += " • Waifus"
		} else if genderFilter == "male" {
			title += " • Husbandos"
		}

		var b strings.Builder
		for _, item := range entries {
			status := "✨ Livre"
			if item.Owner != "" {
				status = fmt.Sprintf("💍 <@%s>", item.Owner)
			}
			fmt.Fprintf(&b, "**#%d** `#%d` **%s** · %s\n♥ %d · **%d** 🪙 · %s\n\n", item.Rank, item.ID, clip(safe(item.Name), 35), clip(safe(item.Work), 35), item.Favourites, item.Value, status)
		}
		if len(entries) == 0 {
			b.WriteString("Nenhum personagem encontrado nesta página com esses filtros.")
		}

		maxPages := int((total + 9) / 10)
		if maxPages == 0 {
			maxPages = 1
		}
		footer := fmt.Sprintf("Página %d de %d • Total: %d personagens", p, maxPages, total)

		embed := &discordgo.MessageEmbed{
			Title:       fmt.Sprintf("%s • Página %d", title, p),
			Description: b.String(),
			Color:       0xe67e22,
			Footer:      &discordgo.MessageEmbedFooter{Text: footer},
		}
		msg.Embeds = []*discordgo.MessageEmbed{embed}
		msg.Components = navigationTopChar(claimFilter, genderFilter, p, p < maxPages)
		return msg, nil
	case "status":
		var used int
		var reset, claim time.Time
		e := s.DB.QueryRowContext(ctx, `SELECT CASE WHEN window_start<=now()-interval '1 hour' THEN 0 ELSE rolls_used END,GREATEST(window_start+interval '1 hour',now()),GREATEST(claim_after,now()) FROM gacha_players WHERE guild_id=$1 AND user_id=$2`, guild, user).Scan(&used, &reset, &claim)

		rollsLeft := s.Config.RollsPerHour
		if e == nil {
			rollsLeft = max(0, s.Config.RollsPerHour-used)
		} else if e != sql.ErrNoRows {
			return nil, e
		}

		claimStatus := "**Disponível agora!**"
		if e == nil && claim.After(time.Now()) {
			claimStatus = fmt.Sprintf("Disponível <t:%d:R>", claim.Unix())
		}

		count, value, _ := s.HaremSummary(ctx, guild, user)
		var claimedTotal int64
		_ = s.DB.QueryRowContext(ctx, `SELECT count(*) FROM gacha_collection WHERE guild_id=$1`, guild).Scan(&claimedTotal)

		var desc strings.Builder
		desc.WriteString(fmt.Sprintf("🎲 **Rolls:** **%d/%d** (reseta <t:%d:R>)\n", rollsLeft, s.Config.RollsPerHour, reset.Unix()))
		desc.WriteString(fmt.Sprintf("💍 **Marry / Claim:** %s\n\n", claimStatus))
		desc.WriteString(fmt.Sprintf("✨ **Seu Harem:** **%d** personagens • **%d** 🪙 valor total\n", count, value))
		desc.WriteString(fmt.Sprintf("🌐 **Bônus do Servidor:** +%.2f%% (%d personagens casados)", float64(claimedTotal)/100, claimedTotal))

		embed := &discordgo.MessageEmbed{
			Title:       "Status de Jogador",
			Description: desc.String(),
			Color:       0xe67e22,
			Footer:      &discordgo.MessageEmbedFooter{Text: "Pousadinha Gacha • Dica: use $wa ou $harem"},
		}
		msg.Embeds = []*discordgo.MessageEmbed{embed}
		return msg, nil
	case "divorce", "trade", "gift":
		recipient, offered, requested, e := parseOffer(action, query)
		if e != nil {
			return nil, e
		}
		a, e := s.CreateAction(ctx, guild, channel, user, recipient, request, action, offered, requested)
		if e != nil {
			return nil, e
		}
		return s.actionMessage(ctx, a)
	case "accept", "decline", "cancel":
		a, e := s.ResolveAction(ctx, guild, channel, user, strings.TrimSpace(query), action)
		if e != nil {
			return nil, e
		}
		return s.actionMessage(ctx, a)
	case "offers":
		offers, e := s.Offers(ctx, guild, user)
		if e != nil {
			return nil, e
		}
		var b strings.Builder
		for _, a := range offers {
			recipient := a.Recipient
			if recipient == "" {
				recipient = a.Proposer
			}
			fmt.Fprintf(&b, "**%s** · #%d → <@%s> · expires <t:%d:R>\n`%s`\n", a.Kind, a.Offered, recipient, a.Expires.Unix(), a.ID)
		}
		if len(offers) == 0 {
			b.WriteString("No pending offers.")
		}
		msg.Content = b.String() + "\nUse `!gacha accept <offer ID>`, `decline` or `cancel` in the original channel."
	case "top":
		ranks, e := s.Rankings(ctx, guild, page)
		if e != nil {
			return nil, e
		}
		var b strings.Builder
		for n, r := range ranks {
			fmt.Fprintf(&b, "**%d.** <@%s> · %d characters · **%d coins**\n", (page-1)*10+n+1, r.User, r.Count, r.Value)
		}
		if len(ranks) == 0 {
			b.WriteString("No harems yet.")
		}
		msg.Components = navigation("top", "0", page, len(ranks) == 10)
		msg.Embeds = []*discordgo.MessageEmbed{{Title: fmt.Sprintf("Harem leaderboard • page %d", page), Description: b.String(), Color: 0xc5a66b}}
	default:
		msg.Content = fmt.Sprintf("**Pousadinha Gacha**\n**Roll:** `!wa` female anime · `!ha` male anime · `!ma` all anime\n`!wg` female games · `!hg` male games · `!mg` all games\n`!w` all female · `!h` all male · `!gacha roll` everyone\n**Collection:** `!harem [@member] [page]` · `!gacha top [page]`\n**Discover:** `!gacha search <name>` · `character <ID>` · `gallery <ID> [page]`\n**Wishlist:** `!gacha wish <ID>` · `unwish <ID>` · `wishes`\n**Social:** `!divorce <ID>` · `!trade @member <your ID> <their ID>` · `!gift @member <ID>`\n`!gacha offers` · `!gacha status` · `!keys <ID>`\nRoll your own character for +1 key. +2%% per key, +10%% every 10 keys.\n%d shared rolls/hour • 1 claim every %d hours • 45-second claim window. Divorce pays virtual coins in this server after confirmation. Gifts/trades require acceptance. Offers expire in 10 minutes.", s.Config.RollsPerHour, s.Config.ClaimHours)
	}
	return msg, nil
}
func HandleClaim(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if Default == nil || i.Member == nil || i.Member.User == nil {
		return
	}
	if e := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{Type: discordgo.InteractionResponseDeferredChannelMessageWithSource, Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral}}); e != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	e := Default.Claim(ctx, i.GuildID, i.ChannelID, i.Member.User.ID, strings.TrimPrefix(i.MessageComponentData().CustomID, "gacha_claim_"))
	content := "Character claimed! Check your harem."
	if e != nil {
		content = friendly(e)
	}
	_, _ = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Content: &content})
	if e == nil && i.Message != nil {
		components := []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{discordgo.Button{Label: "Claimed", Style: discordgo.SecondaryButton, CustomID: "gacha_claim_done", Disabled: true}}}}
		embeds := []*discordgo.MessageEmbed{}
		for _, original := range i.Message.Embeds {
			copyEmbed := *original
			if !strings.Contains(copyEmbed.Description, "Belongs to") {
				copyEmbed.Description += fmt.Sprintf("\nBelongs to <@%s>", i.Member.User.ID)
			}
			embeds = append(embeds, &copyEmbed)
		}
		_, _ = s.ChannelMessageEditComplex(&discordgo.MessageEdit{ID: i.Message.ID, Channel: i.ChannelID, Components: &components, Embeds: &embeds, AllowedMentions: &discordgo.MessageAllowedMentions{}})
	}
}
func Text(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	if Default == nil {
		_, _ = s.ChannelMessageSend(m.ChannelID, "Gacha is not enabled yet.")
		return
	}
	action, query, page, e := parseText(args)
	if e != nil {
		_, _ = s.ChannelMessageSend(m.ChannelID, friendly(e))
		return
	}
	if e = validateTarget(s, m.GuildID, m.Author.ID, action, query); e != nil {
		_, _ = s.ChannelMessageSend(m.ChannelID, friendly(e))
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	msg, e := Default.Execute(ctx, m.GuildID, m.ChannelID, m.Author.ID, m.ID, action, query, page)
	if e != nil {
		_, _ = s.ChannelMessageSend(m.ChannelID, friendly(e))
		return
	}
	if _, e = s.ChannelMessageSendComplex(m.ChannelID, msg); e != nil {
		log.Printf("[gacha] Discord delivery failed: %v", e)
		// Only explicit HTTP rejection proves Discord did not publish the message.
		// A transport timeout is ambiguous and must not allow a free visible roll.
		var rest *discordgo.RESTError
		if errors.As(e, &rest) && rest.Response != nil && rest.Response.StatusCode >= 400 && rest.Response.StatusCode < 500 {
			refundCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := Default.CancelDelivery(refundCtx, m.ID); err != nil {
				log.Printf("[gacha] refund failed: %v", err)
			}
		}
	}
}
func Slash(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if Default == nil || i.Member == nil {
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{Type: discordgo.InteractionResponseChannelMessageWithSource, Data: &discordgo.InteractionResponseData{Content: "Gacha is unavailable. Use it in a server with the feature enabled.", Flags: discordgo.MessageFlagsEphemeral}})
		return
	}
	if e := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{Type: discordgo.InteractionResponseDeferredChannelMessageWithSource}); e != nil {
		return
	}
	action, query, page := "help", "", 1
	member := ""
	var offered, requested int64
	var visual bool
	var claimFilter, genderFilter string
	if i.ApplicationCommandData().Name != "gacha" {
		action = i.ApplicationCommandData().Name
	}
	for _, o := range i.ApplicationCommandData().Options {
		switch o.Name {
		case "action", "acao":
			action = o.StringValue()
		case "query", "busca", "name", "nome":
			query = o.StringValue()
		case "page", "pagina":
			page = int(o.IntValue())
		case "member":
			member = o.Value.(string)
		case "character":
			offered = o.IntValue()
		case "receive":
			requested = o.IntValue()
		case "visual":
			visual = o.BoolValue()
		case "posse", "claim":
			claimFilter = o.StringValue()
		case "genero", "gender":
			genderFilter = o.StringValue()
		}
	}
	action = normalizeAction(action)
	if offered > 0 && query == "" {
		query = strconv.FormatInt(offered, 10)
	}
	if action == "harem" {
		query = member
		if visual {
			action = "harem_visual"
		}
	}
	if action == "topchar" {
		query = strings.TrimSpace(fmt.Sprintf("%s %s", claimFilter, genderFilter))
	}
	if action == "trade" {
		query = fmt.Sprintf("%s %d %d", member, offered, requested)
	}
	if action == "gift" {
		query = fmt.Sprintf("%s %d", member, offered)
	}
	if e := validateTarget(s, i.GuildID, i.Member.User.ID, action, query); e != nil {
		content := friendly(e)
		_, _ = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Content: &content})
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	msg, e := Default.Execute(ctx, i.GuildID, i.ChannelID, i.Member.User.ID, i.ID, action, query, page)
	if e != nil {
		content := friendly(e)
		_, _ = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Content: &content})
		return
	}
	_, e = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Content: &msg.Content, Embeds: &msg.Embeds, Components: &msg.Components, AllowedMentions: msg.AllowedMentions})
	if e != nil {
		log.Printf("[gacha] Discord delivery failed: %v", e)
		var rest *discordgo.RESTError
		if errors.As(e, &rest) && rest.Response != nil && rest.Response.StatusCode >= 400 && rest.Response.StatusCode < 500 {
			refundCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := Default.CancelDelivery(refundCtx, i.ID); err != nil {
				log.Printf("[gacha] refund failed: %v", err)
			}
		}
	}
}

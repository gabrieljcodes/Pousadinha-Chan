package gacha

import (
	"bot/internal/locale"
	"bot/pkg/config"
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
		desc.WriteString(locale.Text("gacha.discord.likes.formatted", locale.Data{"Favourites": c.Favourites}))
	}
	desc.WriteString(fmt.Sprintf("**%d** 🪙", val))
	if c.Owner != "" {
		desc.WriteString(locale.Text("gacha.discord.belongs_to.formatted", locale.Data{"Owner": c.Owner}))
	}

	footerText := locale.Text("gacha.discord.coins.formatted", locale.Data{"Value1": clip(c.Name, 35), "Val": val})
	if c.Work != "" {
		footerText = locale.Text("gacha.discord.coins_136392.formatted", locale.Data{"Value1": clip(c.Name, 25), "Value2": clip(c.Work, 25), "Val": val})
	}

	e := &discordgo.MessageEmbed{
		Title:       clip(c.Name, 200),
		Description: desc.String(),
		Color:       0xe67e22,
		Footer:      &discordgo.MessageEmbedFooter{Text: footerText},
	}
	var imgURL string
	if strings.HasPrefix(c.Image, "http://") || strings.HasPrefix(c.Image, "https://") {
		imgURL = c.Image
	} else if c.Image != "" {
		imgURL = s.Config.PublicURL + "/" + c.Image
	} else if strings.HasPrefix(c.Source, "http://") || strings.HasPrefix(c.Source, "https://") {
		imgURL = c.Source
	}
	if imgURL != "" {
		e.Image = &discordgo.MessageEmbedImage{URL: imgURL}
	}
	return e
}
func friendly(e error) string {
	var input userError
	if errors.As(e, &input) {
		return input.Error()
	}
	if errors.Is(e, sql.ErrNoRows) {
		return locale.Text("gacha.discord.character_not_found")
	}
	if errors.Is(e, ErrLimit) || errors.Is(e, ErrEmpty) || errors.Is(e, ErrClaim) {
		return e.Error()
	}
	log.Printf("[gacha] %v", e)
	return locale.Text("gacha.discord.something_went_wrong_please_try_again")
}
func (s *Store) Execute(ctx context.Context, guild, channel, user, request, action, query string, page int) (*discordgo.MessageSend, error) {
	if page < 1 || page > 100000 {
		return nil, userError(locale.Text("gacha.discord.invalid_page"))
	}
	if guild == "" {
		return nil, userError(locale.Text("gacha.discord.use_this_command_in_a_server"))
	}
	action = normalizeAction(action)
	msg := &discordgo.MessageSend{AllowedMentions: &discordgo.MessageAllowedMentions{}}
	switch action {
	case "roll", "w", "h", "wa", "ha", "ma", "wg", "hg", "mg":
		r, e := s.RollPool(ctx, guild, channel, user, request, action)
		if e != nil {
			return nil, e
		}
		displayCard := r.Card
		if r.IsTrap && r.FakeCard != nil {
			displayCard = *r.FakeCard
		}
		embed := s.cardEmbed(displayCard)
		if r.KeyEarned {
			embed.Description += locale.Text("gacha.discord.key_total.formatted", locale.Data{"Keys": r.Card.Keys})
		}
		if r.BonusCoins > 0 {
			embed.Description += "\n" + locale.Text("gacha.roll.bonus_coins", locale.Data{"Coins": r.BonusCoins})
		}
		if r.BonusRollRefund {
			embed.Description += "\n" + locale.Text("gacha.roll.bonus_refund")
		}
		if embed.Footer != nil {
			if r.RollsLeft == 2 {
				embed.Footer.Text += locale.Text("gacha.discord.two_rolls_left")
			} else if r.RollsLeft == 1 {
				embed.Footer.Text += locale.Text("gacha.discord.one_roll_left")
			} else if r.RollsLeft <= 0 {
				embed.Footer.Text += locale.Text("gacha.discord.no_rolls_left")
			}
		}
		// Only notify wish users if the character is unclaimed
		if displayCard.Owner == "" {
			targetCID := displayCard.ID
			wishRows, qErr := s.DB.QueryContext(ctx, `SELECT user_id FROM gacha_wishes WHERE guild_id=$1 AND character_id=$2 ORDER BY (user_id=$3) DESC, user_id ASC`, guild, targetCID, user)
			if qErr != nil {
				return nil, qErr
			}
			var wishUsers []string
			for wishRows.Next() {
				var wUID string
				if scanErr := wishRows.Scan(&wUID); scanErr != nil {
					wishRows.Close()
					return nil, scanErr
				}
				wishUsers = append(wishUsers, wUID)
			}
			wishErr := wishRows.Err()
			wishRows.Close()
			if wishErr != nil {
				return nil, wishErr
			}
			if len(wishUsers) > 0 {
				embed.Color = 0xffd700
				var mentions []string
				for _, uid := range wishUsers {
					mentions = append(mentions, "<@"+uid+">")
				}
				msg.Content = strings.Join(mentions, " ")
				msg.AllowedMentions = &discordgo.MessageAllowedMentions{Users: wishUsers}
				embed.Description += "\n" + locale.Text("gacha.discord.wished_by.formatted", locale.Data{"Users": strings.Join(mentions, ", ")})
			}
		}
		msg.Embeds = []*discordgo.MessageEmbed{embed}
		var buttons []discordgo.MessageComponent
		if r.Gem != nil {
			currencyName := config.Bot.CurrencyName
			if currencyName == "" {
				currencyName = "Coins"
			}
			gemBtn := discordgo.Button{
				Label:    fmt.Sprintf("%s (+%d)", r.Gem.Name, r.Gem.Value),
				Style:    discordgo.SecondaryButton,
				CustomID: "gacha_gem_" + r.ID,
				Emoji:    r.Gem.ComponentEmoji(),
			}
			buttons = append(buttons, gemBtn)
			embed.Description += fmt.Sprintf("\n%s **%s (+%d %s)**", r.Gem.DiscordString(), r.Gem.Name, r.Gem.Value, currencyName)
			if r.Gem.Description != "" {
				embed.Description += fmt.Sprintf(" • *%s*", r.Gem.Description)
			}
		} else if r.Card.Owner == "" {
			// Only show claim button for unclaimed characters
			buttons = append(buttons, discordgo.Button{
				Label:    locale.Text("gacha.discord.claim_character"),
				Style:    discordgo.SuccessButton,
				CustomID: "gacha_claim_" + r.ID,
				Disabled: !r.Expires.After(time.Now()),
			})
		}
		if len(buttons) > 0 {
			msg.Components = []discordgo.MessageComponent{discordgo.ActionsRow{Components: buttons}}
		}
	case "harem", "harem_visual":
		owner := user
		isVisual := action == "harem_visual"
		cleanQuery := ""
		if query != "" {
			parsedOwner, err := parseMember(query)
			if err != nil {
				return nil, err
			}
			owner = parsedOwner
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
				msg.Content = locale.Text("gacha.discord.doesn_t_have_any_characters_in_their.formatted", locale.Data{"Owner": owner})
				return msg, nil
			}
			embed := s.cardEmbed(c)
			embed.Footer = &discordgo.MessageEmbedFooter{
				Text: locale.Text("gacha.discord.character_of_s_harem.formatted", locale.Data{"Index": index + 1, "Total": total, "Owner": owner}),
			}
			prevBtn := discordgo.Button{Label: locale.Text("gacha.discord.previous"), Style: discordgo.SecondaryButton, CustomID: fmt.Sprintf("gacha_page_haremvisual_%s_%d", owner, index), Disabled: index <= 0}
			listBtn := discordgo.Button{Label: locale.Text("gacha.discord.list_view"), Style: discordgo.PrimaryButton, CustomID: fmt.Sprintf("gacha_page_haremlist_%s_1", owner)}
			nextBtn := discordgo.Button{Label: locale.Text("gacha.discord.next"), Style: discordgo.SecondaryButton, CustomID: fmt.Sprintf("gacha_page_haremvisual_%s_%d", owner, index+2), Disabled: int64(index+1) >= total}
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
			fmt.Fprintf(&b, locale.Text("gacha.discord.coins_d8c438"), c.ID, clip(safe(c.Name), 70), clip(safe(c.Work), 70), c.Value, c.Keys)
		}
		if b.Len() == 0 {
			b.WriteString(locale.Text("gacha.discord.no_characters_found_on_this_page"))
		}
		msg.Embeds = []*discordgo.MessageEmbed{{Title: locale.Text("gacha.discord.catalog_page.formatted", locale.Data{"Page": page}), Description: b.String(), Color: 0xe67e22, Footer: &discordgo.MessageEmbedFooter{Text: locale.Text("gacha.discord.per_page_use_character_id_for_details")}}}
		count, value, e := s.HaremSummary(ctx, guild, owner)
		if e != nil {
			return nil, e
		}
		msg.Components = navigation(action, owner, page, page*10 < count)
		msg.Embeds[0].Title = locale.Text("gacha.discord.harem_page.formatted", locale.Data{"Page": page})
		msg.Embeds[0].Description = locale.Text("gacha.discord.characters_coins.formatted", locale.Data{"Owner": owner, "Count": count, "Value": value}) + msg.Embeds[0].Description
	case "search":
		entries, total, err := s.SearchCharacters(ctx, guild, query, page, 10)
		if err != nil {
			return nil, err
		}
		if total == 0 {
			msg.Content = locale.Text("gacha.discord.character_not_found_try_another_name_with.formatted", locale.Data{"Query": query})
			return msg, nil
		}

		totalPages := int((total + 9) / 10)
		title := fmt.Sprintf("🔍 %s • %s", clip(query, 30), locale.Text("gacha.discord.catalog_page.formatted", locale.Data{"Page": page}))
		if totalPages > 1 {
			title = fmt.Sprintf("🔍 %s • Page %d/%d (%d)", clip(query, 30), page, totalPages, total)
		}

		var b strings.Builder
		for _, item := range entries {
			ownerInfo := ""
			if item.Owner != "" {
				ownerInfo = fmt.Sprintf(" · 💍 <@%s>", item.Owner)
			}
			fmt.Fprintf(&b, "**#%d** `#%d` **%s** — *%s*\n♥ %d%s\n\n", item.Rank, item.ID, clip(safe(item.Name), 40), clip(safe(item.Work), 40), item.Favourites, ownerInfo)
		}

		embed := &discordgo.MessageEmbed{
			Title:       title,
			Description: b.String(),
			Color:       0x3498db,
			Footer: &discordgo.MessageEmbedFooter{
				Text: locale.Text("gacha.discord.per_page_use_character_id_for_details"),
			},
		}
		msg.Embeds = []*discordgo.MessageEmbed{embed}
		msg.Components = navigation("search", clip(query, 40), page, page < totalPages)
	case "series":
		entries, total, workTitle, err := s.SearchCharactersByWork(ctx, guild, query, page, 10)
		if err != nil {
			return nil, err
		}
		if workTitle == "" {
			msg.Content = locale.Text("gacha.discord.no_works_found.formatted", locale.Data{"Query": query})
			return msg, nil
		}
		if total == 0 {
			msg.Content = locale.Text("gacha.discord.no_characters_found_for_work.formatted", locale.Data{"Work": workTitle})
			return msg, nil
		}

		totalPages := int((total + 9) / 10)
		title := locale.Text("gacha.discord.work_catalog_single.formatted", locale.Data{"Work": clip(workTitle, 35), "Total": total})
		if totalPages > 1 {
			title = locale.Text("gacha.discord.work_catalog_page.formatted", locale.Data{"Work": clip(workTitle, 35), "Page": page, "TotalPages": totalPages, "Total": total})
		}

		var b strings.Builder
		for _, item := range entries {
			ownerInfo := ""
			if item.Owner != "" {
				ownerInfo = fmt.Sprintf(" · 💍 <@%s>", item.Owner)
			}
			fmt.Fprintf(&b, "**#%d** `#%d` **%s** — *%s*\n♥ %d%s\n\n", item.Rank, item.ID, clip(safe(item.Name), 40), clip(safe(item.Work), 40), item.Favourites, ownerInfo)
		}

		embed := &discordgo.MessageEmbed{
			Title:       title,
			Description: b.String(),
			Color:       0x8e44ad,
			Footer: &discordgo.MessageEmbedFooter{
				Text: locale.Text("gacha.discord.per_page_use_character_id_for_details"),
			},
		}
		msg.Embeds = []*discordgo.MessageEmbed{embed}
		msg.Components = navigation("series", clip(query, 40), page, page < totalPages)
	case "alias":
		var charQuery, aliasChoice string
		if strings.Contains(query, " | ") {
			parts := strings.SplitN(query, " | ", 2)
			charQuery = strings.TrimSpace(parts[0])
			aliasChoice = strings.TrimSpace(parts[1])
		} else {
			trimmed := strings.TrimSpace(query)
			fields := strings.Fields(trimmed)
			if len(fields) >= 2 {
				if _, err := strconv.ParseInt(fields[0], 10, 64); err == nil {
					charQuery = fields[0]
					aliasChoice = strings.Join(fields[1:], " ")
				} else {
					if _, err := s.ResolveCharacterID(ctx, guild, user, trimmed, true); err == nil {
						charQuery = trimmed
						aliasChoice = ""
					} else {
						charQuery = fields[0]
						aliasChoice = strings.Join(fields[1:], " ")
					}
				}
			} else {
				charQuery = trimmed
				aliasChoice = ""
			}
		}

		if charQuery == "" {
			return nil, userError(locale.Text("gacha.discord.enter_character_to_manage_alias"))
		}

		charID, err := s.ResolveCharacterID(ctx, guild, user, charQuery, true)
		if err != nil {
			return nil, err
		}

		res, err := s.SetCharacterAlias(ctx, guild, user, charID, aliasChoice)
		if err != nil {
			return nil, err
		}

		if res.Reset {
			embed := &discordgo.MessageEmbed{
				Title:       locale.Text("gacha.discord.alias_reset_title"),
				Description: locale.Text("gacha.discord.alias_reset_success.formatted", locale.Data{"Name": res.CanonicalName}),
				Color:       0x2ecc71,
			}
			msg.Embeds = []*discordgo.MessageEmbed{embed}
			return msg, nil
		}

		if aliasChoice == "" {
			var aliasLines []string
			for _, a := range res.Available {
				aliasLines = append(aliasLines, fmt.Sprintf("• **%s**", a))
			}
			activeStatus := locale.Text("gacha.discord.no_alias_active")
			if res.ActiveAlias != "" {
				activeStatus = locale.Text("gacha.discord.current_alias_server.formatted", locale.Data{"Alias": res.ActiveAlias})
			}

			desc := fmt.Sprintf("%s\n\n%s\n\n%s",
				locale.Text("gacha.discord.available_aliases_for.formatted", locale.Data{"Name": res.CanonicalName}),
				strings.Join(aliasLines, "\n"),
				activeStatus,
			)

			embed := &discordgo.MessageEmbed{
				Title:       fmt.Sprintf("🏷️ %s", res.CanonicalName),
				Description: desc,
				Color:       0x3498db,
				Footer: &discordgo.MessageEmbedFooter{
					Text: locale.Text("gacha.discord.alias_instructions.formatted", locale.Data{"Name": res.CanonicalName}),
				},
			}
			msg.Embeds = []*discordgo.MessageEmbed{embed}
			return msg, nil
		}

		embed := &discordgo.MessageEmbed{
			Title: locale.Text("gacha.discord.alias_changed_title"),
			Description: locale.Text("gacha.discord.alias_set_success.formatted", locale.Data{
				"Name":  res.CanonicalName,
				"Alias": res.ActiveAlias,
			}),
			Color: 0x2ecc71,
			Footer: &discordgo.MessageEmbedFooter{
				Text: locale.Text("gacha.discord.alias_persist_note"),
			},
		}
		msg.Embeds = []*discordgo.MessageEmbed{embed}
		return msg, nil
	case "character", "gallery", "keys":
		var c Card
		if action == "character" {
			mainCard, alts, err := s.FindCharacter(ctx, guild, query)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return nil, userError(locale.Text("gacha.discord.character_not_found_try_another_name_with.formatted", locale.Data{"Query": query}))
				}
				return nil, err
			}
			c = mainCard
			embed := s.cardEmbed(c)
			embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{Name: locale.Text("gacha.discord.character_value"), Value: valueLabel(c), Inline: true})
			embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{Name: locale.Text("gacha.discord.catalog"), Value: fmt.Sprintf("#%d", c.ID), Inline: true})
			if c.OriginalName != "" {
				embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{Name: locale.Text("gacha.discord.original_name"), Value: clip(safe(c.OriginalName), 100), Inline: true})
			}

			if c.Owner != "" {
				embed.Description += locale.Text("gacha.discord.claimed_by.formatted", locale.Data{"Owner": c.Owner})
			} else {
				embed.Description += locale.Text("gacha.discord.unclaimed_in_this_server")
			}

			var gender string
			var genres, studios, works string
			_ = s.DB.QueryRowContext(ctx, `SELECT gender FROM gacha_characters WHERE id=$1`, c.ID).Scan(&gender)
			_ = s.DB.QueryRowContext(ctx, `SELECT COALESCE(string_agg(DISTINCT w.title, ', '),''),COALESCE(string_agg(DISTINCT g.value, ', '),''),COALESCE(string_agg(DISTINCT st.value, ', '),'') FROM gacha_character_works cw JOIN gacha_works w ON w.id=cw.work_id LEFT JOIN LATERAL jsonb_array_elements_text(w.genres) g ON true LEFT JOIN LATERAL jsonb_array_elements_text(w.studios) st ON true WHERE cw.character_id=$1`, c.ID).Scan(&works, &genres, &studios)
			for _, v := range []struct{ name, value string }{{locale.Text("gacha.discord.works"), works}, {locale.Text("gacha.discord.work_genres"), genres}, {locale.Text("gacha.discord.studios"), studios}, {locale.Text("gacha.discord.character_gender"), gender}} {
				if v.value != "" {
					embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{Name: v.name, Value: clip(safe(v.value), 900)})
				}
			}

			if len(alts) > 0 {
				var altNames []string
				for _, alt := range alts {
					altNames = append(altNames, fmt.Sprintf("`#%d` %s", alt.ID, clip(safe(alt.Name), 25)))
				}
				embed.Footer.Text += locale.Text("gacha.discord.others") + strings.Join(altNames, ", ")
			}
			msg.Embeds = []*discordgo.MessageEmbed{embed}
			return msg, nil
		}

		card, _, err := s.FindCharacter(ctx, guild, query)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, userError(locale.Text("gacha.discord.character_not_found_try_another_name_with.formatted", locale.Data{"Query": query}))
			}
			return nil, err
		}
		c = card
		id := c.ID
		if action == "gallery" {
			e := s.DB.QueryRowContext(ctx, `SELECT path,source_url,attribution FROM gacha_assets WHERE character_id=$1 AND status='approved' ORDER BY id OFFSET $2 LIMIT 1`, id, page-1).Scan(&c.Image, &c.Source, &c.Attribution)
			if e == sql.ErrNoRows {
				msg.Content = locale.Text("gacha.discord.there_is_no_approved_image_on_this")
				return msg, nil
			}
			if e != nil {
				return nil, e
			}
		}
		if e := priceCards(ctx, s.DB, guild, &c); e != nil {
			return nil, e
		}
		embed := s.cardEmbed(c)
		if action == "keys" {
			embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{Name: locale.Text("gacha.discord.key_milestones"), Value: locale.Text("gacha.discord.per_key_plus_every_keys_keys_to.formatted", locale.Data{"Value1": 10 - c.Keys%10})})
		}
		if action == "gallery" {
			var total int
			if e := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM gacha_assets WHERE character_id=$1 AND status='approved'`, id).Scan(&total); e != nil {
				return nil, e
			}
			embed.Footer.Text = locale.Text("gacha.discord.image_of_character.formatted", locale.Data{"Page": page, "Total": total, "Id": id})
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

		title := locale.Text("gacha.discord.top_characters")
		if claimFilter == "unclaimed" {
			title = locale.Text("gacha.discord.top_unclaimed_characters")
		} else if claimFilter == "claimed" {
			title = locale.Text("gacha.discord.top_claimed_characters")
		}
		if genderFilter == "female" {
			title += locale.Text("gacha.discord.waifus")
		} else if genderFilter == "male" {
			title += locale.Text("gacha.discord.husbandos")
		}

		var b strings.Builder
		for _, item := range entries {
			status := locale.Text("gacha.discord.unclaimed")
			if item.Owner != "" {
				status = fmt.Sprintf("💍 <@%s>", item.Owner)
			}
			fmt.Fprintf(&b, "**#%d** `#%d` **%s** · %s\n♥ %d · **%d** 🪙 · %s\n\n", item.Rank, item.ID, clip(safe(item.Name), 35), clip(safe(item.Work), 35), item.Favourites, item.Value, status)
		}
		if len(entries) == 0 {
			b.WriteString(locale.Text("gacha.discord.no_characters_match_these_filters_on_this"))
		}

		maxPages := int((total + 9) / 10)
		if maxPages == 0 {
			maxPages = 1
		}
		footer := locale.Text("gacha.discord.page_of_total_characters.formatted", locale.Data{"P": p, "MaxPages": maxPages, "Total": total})

		embed := &discordgo.MessageEmbed{
			Title:       locale.Text("gacha.discord.page.formatted", locale.Data{"Title": title, "P": p}),
			Description: b.String(),
			Color:       0xe67e22,
			Footer:      &discordgo.MessageEmbedFooter{Text: footer},
		}
		msg.Embeds = []*discordgo.MessageEmbed{embed}
		msg.Components = navigationTopChar(claimFilter, genderFilter, p, p < maxPages)
		return msg, nil
	case "status":
		schedule := s.GuildSchedule(ctx, guild)
		now := time.Now()
		rollWin := schedule.RollWindow(now)

		var used int
		var windowStart, claimReset time.Time
		var claimActive bool
		var gemPower int
		e := s.DB.QueryRowContext(ctx, `SELECT rolls_used, window_start, CASE WHEN claim_after > now() THEN claim_after ELSE now() END, claim_after > now(), COALESCE(gem_power, 100) FROM gacha_players WHERE guild_id=$1 AND user_id=$2`, guild, user).Scan(&used, &windowStart, &claimReset, &claimActive, &gemPower)

		rollsLeft := schedule.RollsPerHour
		effectiveGemPower := MaxGemPower
		if e == nil {
			if windowStart.Before(rollWin.CurrentStart) {
				rollsLeft = schedule.RollsPerHour
				effectiveGemPower = MaxGemPower
			} else {
				rollsLeft = max(0, schedule.RollsPerHour-used)
				effectiveGemPower = max(0, min(MaxGemPower, gemPower))
			}
		} else if e != sql.ErrNoRows {
			return nil, e
		}

		canClaim := true
		resetsLeft := 0
		if e == nil && claimActive {
			canClaim = false
			t := rollWin.NextReset
			for !t.After(claimReset) {
				resetsLeft++
				t = t.Add(1 * time.Hour)
			}
			if resetsLeft == 0 {
				resetsLeft = 1
			}
		}

		var claimStatus string
		if canClaim {
			claimStatus = locale.Text("gacha.discord.available_now")
		} else if resetsLeft == 1 {
			claimStatus = locale.Text("gacha.discord.claim_reset_single.formatted", locale.Data{"Claim": claimReset.Unix()})
		} else {
			claimStatus = locale.Text("gacha.discord.claim_reset_plural.formatted", locale.Data{"Claim": claimReset.Unix(), "Resets": resetsLeft})
		}

		count, value, _ := s.HaremSummary(ctx, guild, user)
		var claimedTotal int64
		_ = s.DB.QueryRowContext(ctx, `SELECT gacha_claimed_count($1)`, guild).Scan(&claimedTotal)

		var desc strings.Builder
		desc.WriteString(locale.Text("gacha.discord.rolls_resets_t_r.formatted", locale.Data{"RollsLeft": rollsLeft, "RollsPerHour": schedule.RollsPerHour, "Reset": rollWin.NextReset.Unix()}))
		desc.WriteString(locale.Text("gacha.discord.marry_claim.formatted", locale.Data{"ClaimStatus": claimStatus}))
		if effectiveGemPower >= MaxGemPower {
			desc.WriteString(locale.Text("gacha.discord.gem_power_full.formatted", locale.Data{"Power": effectiveGemPower, "Max": MaxGemPower}))
		} else {
			desc.WriteString(locale.Text("gacha.discord.gem_power_status.formatted", locale.Data{"Power": effectiveGemPower, "Max": MaxGemPower, "Reset": rollWin.NextReset.Unix()}))
		}
		desc.WriteString(locale.Text("gacha.discord.your_harem_characters_total_value.formatted", locale.Data{"Count": count, "Value": value}))
		desc.WriteString(locale.Text("gacha.discord.server_bonus_claimed_characters.formatted", locale.Data{"Value1": float64(claimedTotal) / 100, "ClaimedTotal": claimedTotal}))

		embed := &discordgo.MessageEmbed{
			Title:       locale.Text("gacha.discord.player_profile"),
			Description: desc.String(),
			Color:       0xe67e22,
			Footer:      &discordgo.MessageEmbedFooter{Text: locale.Text("gacha.discord.pousadinha_gacha_use_roll_or_harem")},
		}
		msg.Embeds = []*discordgo.MessageEmbed{embed}
		return msg, nil
	case "divorce", "trade", "gift":
		recipient, offered, requested, e := s.ResolveOffer(ctx, guild, user, action, query)
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
			fmt.Fprintf(&b, locale.Text("gacha.discord.expires_t_r"), a.Kind, a.Offered, recipient, a.Expires.Unix(), a.ID)
		}
		if len(offers) == 0 {
			b.WriteString(locale.Text("gacha.discord.no_pending_offers"))
		}
		msg.Content = b.String() + locale.Text("gacha.discord.use_the_buttons_on_the_original_offer")
	case "ranking":
		ranks, e := s.Rankings(ctx, guild, page)
		if e != nil {
			return nil, e
		}
		var b strings.Builder
		for n, r := range ranks {
			fmt.Fprintf(&b, locale.Text("gacha.discord.characters_coins_82844f"), (page-1)*10+n+1, r.User, r.Count, r.Value)
		}
		if len(ranks) == 0 {
			b.WriteString(locale.Text("gacha.discord.no_harems_yet"))
		}
		msg.Components = navigation("top", "0", page, len(ranks) == 10)
		msg.Embeds = []*discordgo.MessageEmbed{{Title: locale.Text("gacha.discord.harem_leaderboard_page.formatted", locale.Data{"Page": page}), Description: b.String(), Color: 0xc5a66b}}
	case "wish", "unwish":
		inputs, err := SplitWishlistInput(query)
		if err != nil {
			return nil, err
		}
		result, err := s.UpdateWishlist(ctx, guild, user, inputs, action == "unwish")
		if err != nil {
			return nil, err
		}
		return wishlistResultMessage(result, action == "unwish"), nil
	case "wishclear":
		return s.wishlistClearMessage(ctx, guild, channel, user)
	case "wishes":
		rows, err := s.DB.QueryContext(ctx, `SELECT c.id, c.name, COALESCE(cw.title, '') FROM gacha_wishes w JOIN gacha_characters c ON c.id=w.character_id LEFT JOIN LATERAL (SELECT title FROM gacha_character_works rel JOIN gacha_works gw ON gw.id=rel.work_id WHERE rel.character_id=c.id ORDER BY gw.id LIMIT 1) cw ON true WHERE w.guild_id=$1 AND w.user_id=$2 ORDER BY c.id`, guild, user)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var b strings.Builder
		count := 0
		for rows.Next() {
			count++
			var id int64
			var name, work string
			if err := rows.Scan(&id, &name, &work); err != nil {
				return nil, err
			} else {
				fmt.Fprintf(&b, "**#%d** `%d` %s (%s)\n", count, id, clip(safe(name), 30), clip(safe(work), 30))
			}
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
		if count == 0 {
			b.WriteString(locale.Text("gacha.discord.your_wishlist_is_empty_use_wishlist_with"))
		}
		embed := &discordgo.MessageEmbed{
			Title:       locale.Text("gacha.discord.your_wishlist"),
			Description: b.String(),
			Color:       0x9b59b6,
			Footer:      &discordgo.MessageEmbedFooter{Text: locale.Plural("gacha.wishlist.count", count, locale.Data{"Count": count, "Limit": s.PlayerWishlistLimit(ctx, guild, user)})},
		}
		msg.Embeds = []*discordgo.MessageEmbed{embed}
	case "shop":
		return s.RenderShop(ctx, guild, user)
	case "inventory":
		return s.RenderInventory(ctx, guild, user)
	case "buy":
		fields := strings.Fields(query)
		if len(fields) == 0 {
			return s.RenderShop(ctx, guild, user)
		}
		itemID := ItemID(strings.ToLower(fields[0]))
		qty := 1
		if len(fields) > 1 {
			if parsed, err := strconv.Atoi(fields[1]); err == nil && parsed > 0 {
				qty = parsed
			}
		}
		buyRes, err := s.BuyItem(ctx, guild, user, itemID, qty)
		if err != nil {
			return nil, err
		}
		msg.Content = locale.Text("gacha.shop.buy_success", locale.Data{
			"Quantity":   buyRes.Quantity,
			"ItemName":   buyRes.Item.Name(),
			"TotalCost":  buyRes.TotalCost,
			"NewBalance": buyRes.NewBalance,
		})
		return msg, nil
	case "use":
		itemID := ItemID(strings.ToLower(strings.TrimSpace(query)))
		if itemID == "" {
			return s.RenderInventory(ctx, guild, user)
		}
		useRes, err := s.UseItem(ctx, guild, user, itemID)
		if err != nil {
			return nil, err
		}
		if useRes.LootReward != nil {
			embed := &discordgo.MessageEmbed{
				Title:       useRes.LootReward.Title,
				Description: useRes.LootReward.Description,
				Color:       0xe67e22,
				Footer: &discordgo.MessageEmbedFooter{
					Text: "Cosmic Chest Rewards • Pousadinha Gacha",
				},
			}
			msg.Embeds = []*discordgo.MessageEmbed{embed}
			return msg, nil
		}
		msg.Content = useRes.Message
		return msg, nil
	case "open":
		reward, err := s.OpenLootbox(ctx, guild, user)
		if err != nil {
			return nil, err
		}
		embed := &discordgo.MessageEmbed{
			Title:       reward.Title,
			Description: reward.Description,
			Color:       0xe67e22,
			Footer: &discordgo.MessageEmbedFooter{
				Text: "Cosmic Chest Rewards • Pousadinha Gacha",
			},
		}
		msg.Embeds = []*discordgo.MessageEmbed{embed}
		return msg, nil
	case "mycustoms":
		return HandleMyCustoms(ctx, guild, user)
	case "builds", "build", "skills", "tree":
		if query != "" {
			return s.RenderClassTree(ctx, guild, user, SkillClassID(query))
		}
		return s.RenderBuildOverview(ctx, guild, user)
	case "learnskill", "learn":
		_, err := s.UnlockSkill(ctx, guild, user, query)
		if err != nil {
			return nil, err
		}
		sk, _ := s.GetSkillDef(query)
		return s.RenderClassTree(ctx, guild, user, sk.ClassID)
	case "respec":
		_, err := s.RespecBuild(ctx, guild, user)
		if err != nil {
			return nil, err
		}
		m, err := s.RenderBuildOverview(ctx, guild, user)
		if err == nil {
			m.Content = locale.Text("gacha.builds.respec_success")
		}
		return m, err
	default:
		sch := s.GuildSchedule(ctx, guild)
		msg.Content = locale.Text("gacha.discord.pousadinha_gacha_use_roll_top_info_harem.formatted", locale.Data{"RollsPerHour": sch.RollsPerHour, "ClaimHours": sch.ClaimHours})
	}
	return msg, nil
}

// updateComponentsWithClaimedGem safely handles both pointer and value ActionsRow/Button components.
func updateComponentsWithClaimedGem(components []discordgo.MessageComponent, rollID string, gem Gem, username string) []discordgo.MessageComponent {
	newComponents := make([]discordgo.MessageComponent, 0, len(components))
	for _, rowComp := range components {
		var btns []discordgo.MessageComponent
		switch row := rowComp.(type) {
		case discordgo.ActionsRow:
			btns = row.Components
		case *discordgo.ActionsRow:
			btns = row.Components
		default:
			newComponents = append(newComponents, rowComp)
			continue
		}

		newBtns := make([]discordgo.MessageComponent, 0, len(btns))
		for _, btnComp := range btns {
			switch btn := btnComp.(type) {
			case discordgo.Button:
				if btn.CustomID == "gacha_gem_"+rollID {
					btn.Disabled = true
					btn.Style = discordgo.SecondaryButton
					btn.Label = fmt.Sprintf("%s (%s)", gem.Name, username)
				}
				newBtns = append(newBtns, btn)
			case *discordgo.Button:
				copied := *btn
				if copied.CustomID == "gacha_gem_"+rollID {
					copied.Disabled = true
					copied.Style = discordgo.SecondaryButton
					copied.Label = fmt.Sprintf("%s (%s)", gem.Name, username)
				}
				newBtns = append(newBtns, copied)
			default:
				newBtns = append(newBtns, btnComp)
			}
		}
		newComponents = append(newComponents, discordgo.ActionsRow{Components: newBtns})
	}
	return newComponents
}

// HandleClaimGem handles button interactions when a user clicks to absorb a gem.
func HandleClaimGem(s *discordgo.Session, i *discordgo.InteractionCreate) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[HandleClaimGem] PANIC recovered: %v", r)
		}
	}()

	if Default == nil || i.Member == nil || i.Member.User == nil || i.GuildID == "" {
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: locale.Text("gacha.discord.gacha_is_unavailable_use_it_in_a"),
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	rollID := strings.TrimPrefix(i.MessageComponentData().CustomID, "gacha_gem_")
	if rollID == "" {
		return
	}

	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{Type: discordgo.InteractionResponseDeferredChannelMessageWithSource, Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral}}); err != nil {
		log.Printf("[gacha] gem acknowledgement failed roll=%s: %v", rollID, err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	res, err := Default.ClaimGemAtomic(ctx, i.GuildID, i.ChannelID, rollID, i.Member.User.ID)
	if err != nil {
		log.Printf("[HandleClaimGem] ClaimGemAtomic error: %v", err)
		errContent := friendly(err)
		if _, deliveryErr := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Content: &errContent}); deliveryErr != nil {
			log.Printf("[gacha] gem error delivery failed roll=%s: %v", rollID, deliveryErr)
		}
		return
	}

	var responseContent string
	if res.AlreadyClaimed {
		responseContent = locale.Text("gacha.gems.already_claimed.formatted", locale.Data{"User": res.ClaimedByOther})
	} else if res.Expired {
		responseContent = locale.Text("gacha.gems.expired")
	} else if res.InsufficientPower {
		responseContent = locale.Text("gacha.gems.insufficient_power.formatted", locale.Data{
			"Current":  res.CurrentPower,
			"Required": res.RequiredPower,
			"Reset":    res.NextReset.Unix(),
		})
	} else {
		currencyName := config.Bot.CurrencyName
		if currencyName == "" {
			currencyName = "Coins"
		}
		responseContent = locale.Text("gacha.gems.claim_success.formatted", locale.Data{
			"Emoji":          res.Gem.DiscordString(),
			"Gem":            res.Gem.Name,
			"Value":          res.Gem.Value,
			"Currency":       currencyName,
			"RemainingPower": res.RemainingPower,
			"Reset":          res.NextReset.Unix(),
		})
	}

	_, respErr := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Content: &responseContent})
	if respErr != nil {
		log.Printf("[HandleClaimGem] InteractionRespond error: %v", respErr)
	}

	// If successfully claimed and message exists, update the public message button and embed
	if !res.AlreadyClaimed && !res.Expired && !res.InsufficientPower && i.Message != nil {
		currencyName := config.Bot.CurrencyName
		if currencyName == "" {
			currencyName = "Coins"
		}
		newComponents := updateComponentsWithClaimedGem(i.Message.Components, rollID, res.Gem, i.Member.User.Username)

		newEmbeds := make([]*discordgo.MessageEmbed, 0, len(i.Message.Embeds))
		for _, origEmbed := range i.Message.Embeds {
			cp := *origEmbed
			claimedLine := "\n" + locale.Text("gacha.gems.absorbed_by.formatted", locale.Data{
				"Emoji":    res.Gem.DiscordString(),
				"User":     i.Member.User.ID,
				"Value":    res.Gem.Value,
				"Currency": currencyName,
			})
			cp.Description += claimedLine
			newEmbeds = append(newEmbeds, &cp)
		}

		_, editErr := s.ChannelMessageEditComplex(&discordgo.MessageEdit{
			ID:              i.Message.ID,
			Channel:         i.ChannelID,
			Components:      &newComponents,
			Embeds:          &newEmbeds,
			AllowedMentions: &discordgo.MessageAllowedMentions{},
		})
		if editErr != nil {
			log.Printf("[HandleClaimGem] ChannelMessageEditComplex error: %v", editErr)
		}
	}
}

func Slash(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if Default == nil || i.Member == nil {
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{Type: discordgo.InteractionResponseChannelMessageWithSource, Data: &discordgo.InteractionResponseData{Content: locale.Text("gacha.discord.gacha_is_unavailable_use_it_in_a"), Flags: discordgo.MessageFlagsEphemeral}})
		return
	}
	if e := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{Type: discordgo.InteractionResponseDeferredChannelMessageWithSource}); e != nil {
		return
	}
	action, query, page := parseSlash(i.ApplicationCommandData())
	if e := Default.validateChannel(context.Background(), i.GuildID, i.ChannelID, action); e != nil {
		content := friendly(e)
		_, _ = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Content: &content})
		return
	}
	if e := validateTarget(s, i.GuildID, i.Member.User.ID, action, query); e != nil {
		content := friendly(e)
		_, _ = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Content: &content})
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
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

// parseSlash maps the public command schema to the internal game actions.
func parseSlash(data discordgo.ApplicationCommandInteractionData) (string, string, int) {
	action, query, page := data.Name, "", 1
	member := ""
	var offerStr, receiveStr, aliasStr, searchType string
	var offered, requested int64
	var visual bool
	var claimFilter, genderFilter string
	for _, o := range data.Options {
		switch o.Name {
		case "pool":
			action = o.StringValue()
		case "action":
			action = o.StringValue()
		case "character", "query", "id", "name", "series", "work":
			if o.Type == discordgo.ApplicationCommandOptionInteger {
				query = strconv.FormatInt(o.IntValue(), 10)
				offered = o.IntValue()
			} else {
				query = o.StringValue()
			}
		case "type":
			searchType = o.StringValue()
		case "page":
			page = int(o.IntValue())
		case "member":
			member = o.Value.(string)
		case "offer":
			if o.Type == discordgo.ApplicationCommandOptionInteger {
				offered = o.IntValue()
				offerStr = strconv.FormatInt(o.IntValue(), 10)
			} else {
				offerStr = o.StringValue()
			}
		case "receive":
			if o.Type == discordgo.ApplicationCommandOptionInteger {
				requested = o.IntValue()
				receiveStr = strconv.FormatInt(o.IntValue(), 10)
			} else {
				receiveStr = o.StringValue()
			}
		case "alias":
			aliasStr = o.StringValue()
		case "mode":
			if o.StringValue() == "visual" {
				visual = true
			}
		case "claim":
			claimFilter = o.StringValue()
		case "gender":
			genderFilter = o.StringValue()
		case "item":
			if query == "" {
				query = o.StringValue()
			} else {
				query = fmt.Sprintf("%s %s", o.StringValue(), query)
			}
		case "quantity":
			if o.IntValue() > 0 {
				query = fmt.Sprintf("%s %d", strings.TrimSpace(query), o.IntValue())
			}
		case "class", "skill":
			query = o.StringValue()
		}
	}
	action = normalizeAction(action)
	if action == "search" && searchType == "series" {
		action = "series"
	}
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
		if offerStr == "" && offered > 0 {
			offerStr = strconv.FormatInt(offered, 10)
		}
		if receiveStr == "" && requested > 0 {
			receiveStr = strconv.FormatInt(requested, 10)
		}
		if strings.Contains(offerStr, " ") || strings.Contains(receiveStr, " ") {
			query = fmt.Sprintf("%s | %s | %s", member, offerStr, receiveStr)
		} else {
			query = fmt.Sprintf("%s %s %s", member, offerStr, receiveStr)
		}
	}
	if action == "gift" {
		charStr := query
		if charStr == "" && offered > 0 {
			charStr = strconv.FormatInt(offered, 10)
		}
		if strings.Contains(charStr, " ") {
			query = fmt.Sprintf("%s | %s", member, charStr)
		} else {
			query = fmt.Sprintf("%s %s", member, charStr)
		}
	}
	if action == "divorce" && query == "" && offered > 0 {
		query = strconv.FormatInt(offered, 10)
	}
	if action == "gallery" && query == "" && offered > 0 {
		query = strconv.FormatInt(offered, 10)
	}
	if (action == "wish" || action == "unwish") && query == "" && offered > 0 {
		query = strconv.FormatInt(offered, 10)
	}
	if action == "alias" {
		if query == "" && offered > 0 {
			query = strconv.FormatInt(offered, 10)
		}
		if aliasStr != "" {
			query = fmt.Sprintf("%s | %s", query, aliasStr)
		}
	}

	return action, query, page
}

func isRollAction(action string) bool {
	action = normalizeAction(action)
	switch action {
	case "roll", "w", "h", "wa", "ha", "ma", "wg", "hg", "mg":
		return true
	default:
		return false
	}
}

// ValidateChannel checks whether the given action is allowed in channelID for guildID.
func ValidateChannel(ctx context.Context, guildID, channelID, action string) error {
	if Default == nil {
		return nil
	}
	return Default.validateChannel(ctx, guildID, channelID, action)
}

func (s *Store) validateChannel(ctx context.Context, guildID, channelID, action string) error {
	if guildID == "" || channelID == "" || action == "gachaconfig" {
		return nil
	}
	sch := s.GuildSchedule(ctx, guildID)

	if sch.RollChannelID == "" && sch.CmdChannelID == "" {
		return userError(locale.Text("gacha.channels.not_configured"))
	}

	norm := normalizeAction(action)
	if norm == "status" || norm == "shop" || norm == "inventory" || norm == "buy" || norm == "use" || norm == "open" || norm == "mycustoms" || norm == "addcustom" || norm == "removecustom" || norm == "customimage" || norm == "builds" || norm == "learnskill" || norm == "respec" {
		if (sch.RollChannelID != "" && channelID == sch.RollChannelID) || (sch.CmdChannelID != "" && channelID == sch.CmdChannelID) {
			return nil
		}
		target := sch.CmdChannelID
		if target == "" {
			target = sch.RollChannelID
		}
		return userError(locale.Text("gacha.channels.commands_only_in.formatted", locale.Data{"Channel": target}))
	}

	if isRollAction(action) {
		if sch.RollChannelID == "" {
			return userError(locale.Text("gacha.channels.roll_channel_not_configured"))
		}
		if channelID != sch.RollChannelID {
			return userError(locale.Text("gacha.channels.rolls_only_in.formatted", locale.Data{"Channel": sch.RollChannelID}))
		}
	} else {
		target := sch.CmdChannelID
		if target == "" {
			target = sch.RollChannelID
		}
		if target == "" {
			return userError(locale.Text("gacha.channels.cmd_channel_not_configured"))
		}
		if channelID != target {
			return userError(locale.Text("gacha.channels.commands_only_in.formatted", locale.Data{"Channel": target}))
		}
	}
	return nil
}

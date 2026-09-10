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
	e := &discordgo.MessageEmbed{Title: clip(c.Name, 200), Description: clip(safe(c.Work), 300), Color: 0xc5a66b, Fields: []*discordgo.MessageEmbedField{{Name: "Popularidade AniList", Value: fmt.Sprintf("♥ %d favoritos", c.Favourites), Inline: true}, {Name: "Catálogo", Value: fmt.Sprintf("#%d", c.ID), Inline: true}}, Footer: &discordgo.MessageEmbedFooter{Text: "Pousadinha • Colecione suas histórias"}}
	if c.Image != "" {
		e.Image = &discordgo.MessageEmbedImage{URL: s.Config.PublicURL + "/" + c.Image}
	}
	if c.Source != "" {
		e.URL = c.Source
		e.Fields = append(e.Fields, &discordgo.MessageEmbedField{Name: "Créditos", Value: "[Publicação original](" + c.Source + ")"})
	}
	return e
}
func friendly(e error) string {
	if errors.Is(e, ErrLimit) || errors.Is(e, ErrEmpty) || errors.Is(e, ErrClaim) {
		return e.Error()
	}
	log.Printf("[gacha] %v", e)
	return "Não foi possível concluir. Verifique os parâmetros e tente novamente."
}
func (s *Store) Execute(ctx context.Context, guild, channel, user, request, action, query string, page int) (*discordgo.MessageSend, error) {
	if page < 1 || page > 100000 {
		return nil, fmt.Errorf("invalid page")
	}
	if guild == "" {
		return nil, fmt.Errorf("guild required")
	}
	msg := &discordgo.MessageSend{AllowedMentions: &discordgo.MessageAllowedMentions{}}
	switch action {
	case "roll", "sortear":
		r, e := s.Roll(ctx, guild, channel, user, request)
		if e != nil {
			return nil, e
		}
		embed := s.cardEmbed(r.Card)
		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{Name: "Reserva", Value: fmt.Sprintf("Disponível até <t:%d:T> • primeiro a reservar", r.Expires.Unix())})
		if r.Card.Owner != "" {
			embed.Fields[len(embed.Fields)-1].Value = "Já pertence a <@" + r.Card.Owner + ">"
		}
		var wished bool
		if e = s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM gacha_wishes WHERE guild_id=$1 AND character_id=$2)`, guild, r.Card.ID).Scan(&wished); e != nil {
			return nil, e
		}
		if wished {
			embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{Name: "✦ Desejado", Value: "Está na lista de desejos de alguém deste servidor."})
		}
		msg.Embeds = []*discordgo.MessageEmbed{embed}
		msg.Components = []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{discordgo.Button{Label: "Reservar personagem", Style: discordgo.SuccessButton, CustomID: "gacha_claim_" + r.ID, Disabled: r.Card.Owner != ""}}}}
	case "collection", "colecao", "search", "buscar":
		owner := ""
		if action == "collection" || action == "colecao" {
			owner = user
		}
		cards, e := s.Cards(ctx, guild, owner, query, page)
		if e != nil {
			return nil, e
		}
		var b strings.Builder
		for _, c := range cards {
			fmt.Fprintf(&b, "`#%d` **%s** · %s · ♥ %d\n", c.ID, clip(safe(c.Name), 70), clip(safe(c.Work), 70), c.Favourites)
		}
		if b.Len() == 0 {
			b.WriteString("Nenhum personagem encontrado nesta página.")
		}
		msg.Embeds = []*discordgo.MessageEmbed{{Title: fmt.Sprintf("Catálogo • página %d", page), Description: b.String(), Color: 0xc5a66b, Footer: &discordgo.MessageEmbedFooter{Text: "10 por página • use personagem <ID> para detalhes"}}}
	case "character", "personagem", "gallery", "galeria":
		id, e := strconv.ParseInt(query, 10, 64)
		if e != nil {
			return nil, e
		}
		c, e := scanCard(s.DB.QueryRowContext(ctx, cardSelect+` WHERE c.id=$1`, id))
		if e != nil {
			return nil, e
		}
		if action == "gallery" || action == "galeria" {
			e = s.DB.QueryRowContext(ctx, `SELECT path,source_url,attribution FROM gacha_assets WHERE character_id=$1 AND status='approved' ORDER BY id OFFSET $2 LIMIT 1`, id, page-1).Scan(&c.Image, &c.Source, &c.Attribution)
			if e == sql.ErrNoRows {
				msg.Content = "Não há imagem aprovada nesta página."
				return msg, nil
			}
			if e != nil {
				return nil, e
			}
		}
		embed := s.cardEmbed(c)
		if action == "character" || action == "personagem" {
			var gender string
			var genres, studios, works string
			e = s.DB.QueryRowContext(ctx, `SELECT gender FROM gacha_characters WHERE id=$1`, id).Scan(&gender)
			if e != nil {
				return nil, e
			}
			e = s.DB.QueryRowContext(ctx, `SELECT COALESCE(string_agg(DISTINCT w.title, ', '),''),COALESCE(string_agg(DISTINCT g.value, ', '),''),COALESCE(string_agg(DISTINCT st.value, ', '),'') FROM gacha_character_works cw JOIN gacha_works w ON w.id=cw.work_id LEFT JOIN LATERAL jsonb_array_elements_text(w.genres) g ON true LEFT JOIN LATERAL jsonb_array_elements_text(w.studios) st ON true WHERE cw.character_id=$1`, id).Scan(&works, &genres, &studios)
			if e != nil {
				return nil, e
			}
			for _, v := range []struct{ name, value string }{{"Obras", works}, {"Gêneros das obras", genres}, {"Estúdios", studios}, {"Gênero do personagem", gender}} {
				if v.value != "" {
					embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{Name: v.name, Value: clip(safe(v.value), 900)})
				}
			}
		}
		msg.Embeds = []*discordgo.MessageEmbed{embed}
	case "wish", "desejar", "unwish", "remover":
		id, e := strconv.ParseInt(query, 10, 64)
		if e != nil || id <= 0 {
			return nil, fmt.Errorf("invalid character ID")
		}
		if e = s.Wish(ctx, guild, user, id, action == "unwish" || action == "remover"); e != nil {
			return nil, e
		}
		msg.Content = "Lista de desejos atualizada."
	case "wishes", "desejos":
		rows, e := s.DB.QueryContext(ctx, `SELECT c.id,c.name FROM gacha_wishes w JOIN gacha_characters c ON c.id=w.character_id WHERE w.guild_id=$1 AND w.user_id=$2 ORDER BY c.name`, guild, user)
		if e != nil {
			return nil, e
		}
		defer rows.Close()
		var b strings.Builder
		for rows.Next() {
			var id int64
			var name string
			if e = rows.Scan(&id, &name); e != nil {
				return nil, e
			}
			fmt.Fprintf(&b, "`#%d` %s\n", id, clip(safe(name), 70))
		}
		if e = rows.Err(); e != nil {
			return nil, e
		}
		if b.Len() == 0 {
			b.WriteString("Sua lista está vazia. Use desejar <ID>.")
		}
		msg.Content = b.String()
	case "status":
		var used int
		var reset, claim time.Time
		e := s.DB.QueryRowContext(ctx, `SELECT CASE WHEN window_start<=now()-interval '1 hour' THEN 0 ELSE rolls_used END,GREATEST(window_start+interval '1 hour',now()),GREATEST(claim_after,now()) FROM gacha_players WHERE guild_id=$1 AND user_id=$2`, guild, user).Scan(&used, &reset, &claim)
		if e == sql.ErrNoRows {
			msg.Content = fmt.Sprintf("Você tem %d sorteios disponíveis e pode reservar um personagem.", s.Config.RollsPerHour)
		} else if e != nil {
			return nil, e
		} else {
			msg.Content = fmt.Sprintf("Sorteios disponíveis: **%d/%d** · renovação <t:%d:R>\nReserva disponível <t:%d:R>", max(0, s.Config.RollsPerHour-used), s.Config.RollsPerHour, reset.Unix(), claim.Unix())
		}
	default:
		msg.Content = fmt.Sprintf("**Gacha Pousadinha**\n`!gacha sortear` · `colecao [página]` · `buscar <nome>`\n`personagem <ID>` · `galeria <ID> [página]`\n`desejar <ID>` · `remover <ID>` · `desejos` · `status`\n%d sorteios/hora; 1 reserva a cada %d horas; disputa de 45 segundos. Chances iguais entre personagens habilitados. Coleções por servidor, sem custo em moedas.", s.Config.RollsPerHour, s.Config.ClaimHours)
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
	content := "Personagem reservado! Confira sua coleção."
	if e != nil {
		content = friendly(e)
	}
	_, _ = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Content: &content})
	if e == nil && i.Message != nil {
		components := []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{discordgo.Button{Label: "Reservado", Style: discordgo.SecondaryButton, CustomID: "gacha_claim_done", Disabled: true}}}}
		_, _ = s.ChannelMessageEditComplex(&discordgo.MessageEdit{ID: i.Message.ID, Channel: i.ChannelID, Components: &components})
	}
}
func Text(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	if Default == nil {
		_, _ = s.ChannelMessageSend(m.ChannelID, "Gacha ainda não está habilitado.")
		return
	}
	action, query, page := "help", "", 1
	if len(args) > 0 {
		action = args[0]
	}
	if len(args) > 1 {
		query = strings.Join(args[1:], " ")
	}
	if action == "colecao" || action == "collection" {
		query = ""
		if len(args) > 1 {
			page, _ = strconv.Atoi(args[1])
		}
	}
	if action == "galeria" || action == "gallery" {
		if len(args) > 1 {
			query = args[1]
		}
		if len(args) > 2 {
			page, _ = strconv.Atoi(args[2])
		}
	}
	if page < 1 || page > 100000 {
		_, _ = s.ChannelMessageSend(m.ChannelID, "Página inválida.")
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
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{Type: discordgo.InteractionResponseChannelMessageWithSource, Data: &discordgo.InteractionResponseData{Content: "Gacha indisponível. Use em um servidor com o recurso habilitado.", Flags: discordgo.MessageFlagsEphemeral}})
		return
	}
	if e := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{Type: discordgo.InteractionResponseDeferredChannelMessageWithSource}); e != nil {
		return
	}
	action, query, page := "help", "", 1
	for _, o := range i.ApplicationCommandData().Options {
		switch o.Name {
		case "acao":
			action = o.StringValue()
		case "busca":
			query = o.StringValue()
		case "pagina":
			page = int(o.IntValue())
		}
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

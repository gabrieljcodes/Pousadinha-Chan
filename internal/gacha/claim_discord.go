package gacha

import (
	"bot/internal/locale"
	"context"
	"database/sql"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/google/uuid"
)

// ClaimedRollOwner returns the immutable claim receipt, not the clicker's
// identity or current collection owner (which may change after a trade).
func (s *Store) ClaimedRollOwner(ctx context.Context, guild, channel, roll string) (string, error) {
	var owner string
	err := s.DB.QueryRowContext(ctx, `SELECT claimed_by FROM gacha_rolls WHERE id=$1 AND guild_id=$2 AND channel_id=$3 AND claimed_by IS NOT NULL`, roll, guild, channel).Scan(&owner)
	return owner, err
}

// claimedRollEdit only produces terminal state. Concurrent or delayed clicks
// always render the same persisted winner and can never restore a claim button.
func claimedRollEdit(message *discordgo.Message, channel, roll, owner string) *discordgo.MessageEdit {
	if message == nil || owner == "" {
		return nil
	}
	content := locale.Text("gacha.claim.receipt", locale.Data{"Owner": owner})
	components := []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{
		discordgo.Button{Label: locale.Text("gacha.discord.claimed"), Style: discordgo.SecondaryButton, CustomID: "gacha_claim_" + roll, Disabled: true},
	}}}

	var embeds []*discordgo.MessageEmbed
	fieldName := locale.Text("gacha.claim.claimed_by")

	// Check if this was a trap: if so, shatter illusion and display real character embed!
	var cid int64
	var isTrap bool
	if Default != nil && Default.DB != nil {
		_ = Default.DB.QueryRowContext(context.Background(), `SELECT character_id, COALESCE(is_trap, false) FROM gacha_rolls WHERE id=$1`, roll).Scan(&cid, &isTrap)
		if isTrap && cid > 0 {
			card, err := scanCard(Default.DB.QueryRowContext(context.Background(), cardSelect+` WHERE c.id=$1`, cid))
			if err == nil {
				Default.applyGuildImage(context.Background(), message.GuildID, &card)
				realEmbed := Default.cardEmbed(card)
				realEmbed.Fields = append(realEmbed.Fields, &discordgo.MessageEmbedField{Name: fieldName, Value: "<@" + owner + ">"})
				embeds = []*discordgo.MessageEmbed{realEmbed}
			}
		}
	}

	if len(embeds) == 0 {
		embeds = make([]*discordgo.MessageEmbed, 0, len(message.Embeds))
		for _, original := range message.Embeds {
			if original == nil {
				continue
			}
			copied := *original
			copied.Fields = make([]*discordgo.MessageEmbedField, 0, len(original.Fields)+1)
			for _, field := range original.Fields {
				if field != nil && field.Name != fieldName && len(copied.Fields) < 24 {
					clone := *field
					copied.Fields = append(copied.Fields, &clone)
				}
			}
			copied.Fields = append(copied.Fields, &discordgo.MessageEmbedField{Name: fieldName, Value: "<@" + owner + ">"})
			embeds = append(embeds, &copied)
		}
	}
	return &discordgo.MessageEdit{ID: message.ID, Channel: channel, Content: &content, Components: &components, Embeds: &embeds, AllowedMentions: &discordgo.MessageAllowedMentions{}}
}

func (s *Store) revealTrapAfterDelay(session *discordgo.Session, guildID, channelID string, message *discordgo.Message, rollID string, delay time.Duration) {
	time.Sleep(delay)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var cid int64
	var expiresAt time.Time
	var claimedBy sql.NullString
	err := s.DB.QueryRowContext(ctx, `
		UPDATE gacha_rolls
		SET trap_revealed = true
		WHERE id = $1 AND guild_id = $2
		RETURNING character_id, expires_at, claimed_by
	`, rollID, guildID).Scan(&cid, &expiresAt, &claimedBy)
	if err != nil || (claimedBy.Valid && claimedBy.String != "") {
		return
	}

	card, err := scanCard(s.DB.QueryRowContext(ctx, cardSelect+` WHERE c.id=$1`, cid))
	if err != nil {
		return
	}
	s.applyGuildImage(ctx, guildID, &card)

	embed := s.cardEmbed(card)
	embed.Description += "\n\n" + locale.Text("gacha.claim.trap_revealed_notice")

	claimButton := discordgo.Button{
		Label:    locale.Text("gacha.discord.claim_character"),
		Style:    discordgo.SuccessButton,
		CustomID: "gacha_claim_" + rollID,
		Disabled: !expiresAt.After(time.Now()),
	}

	components := []discordgo.MessageComponent{
		discordgo.ActionsRow{Components: []discordgo.MessageComponent{claimButton}},
	}

	_, _ = session.ChannelMessageEditComplex(&discordgo.MessageEdit{
		ID:         message.ID,
		Channel:    channelID,
		Embeds:     &[]*discordgo.MessageEmbed{embed},
		Components: &components,
	})
}

func HandleClaim(session *discordgo.Session, i *discordgo.InteractionCreate) {
	if session == nil || Default == nil || i == nil || i.Interaction == nil || i.Type != discordgo.InteractionMessageComponent || i.Member == nil || i.Member.User == nil || i.GuildID == "" {
		return
	}
	customID := i.MessageComponentData().CustomID
	if !strings.HasPrefix(customID, "gacha_claim_") {
		return
	}
	roll := strings.TrimPrefix(customID, "gacha_claim_")
	if _, err := uuid.Parse(roll); err != nil {
		return
	}
	if err := session.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{Type: discordgo.InteractionResponseDeferredChannelMessageWithSource, Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral}}); err != nil {
		log.Printf("[gacha] claim acknowledgement failed roll=%s: %v", roll, err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	var wasTrap bool
	var rollerID string
	_ = Default.DB.QueryRowContext(ctx, `SELECT COALESCE(is_trap, false), user_id FROM gacha_rolls WHERE id=$1`, roll).Scan(&wasTrap, &rollerID)

	claimErr := Default.Claim(ctx, i.GuildID, i.ChannelID, i.Member.User.ID, roll)
	if errors.Is(claimErr, ErrTrapRollerClick) {
		warning := locale.Text("gacha.claim.trap_roller_warning")
		_, _ = session.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Content: &warning})
		go Default.revealTrapAfterDelay(session, i.GuildID, i.ChannelID, i.Message, roll, 3*time.Second)
		return
	}

	content := locale.Text("gacha.discord.character_claimed_check_your_harem")
	if claimErr != nil {
		content = friendly(claimErr)
	} else if wasTrap && i.Member.User.ID != rollerID {
		content = locale.Text("gacha.claim.trap_sniper_bamboozled")
	}

	if _, err := session.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Content: &content}, discordgo.WithContext(ctx)); err != nil {
		log.Printf("[gacha] claim receipt delivery failed roll=%s: %v", roll, err)
	}
	// A losing click also repairs an earlier failed public update. Read using a
	// fresh deadline: a transaction timeout does not prove its commit failed.
	repairCtx, repairCancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer repairCancel()
	owner, err := Default.ClaimedRollOwner(repairCtx, i.GuildID, i.ChannelID, roll)
	if errors.Is(err, sql.ErrNoRows) {
		return
	}
	if err != nil {
		log.Printf("[gacha] claim reconciliation failed roll=%s: %v", roll, err)
		return
	}
	edit := claimedRollEdit(i.Message, i.ChannelID, roll, owner)
	if edit == nil {
		return
	}
	if _, err = session.ChannelMessageEditComplex(edit, discordgo.WithContext(repairCtx)); err != nil {
		log.Printf("[gacha] public claim update failed roll=%s message=%s winner=%s: %v", roll, i.Message.ID, owner, err)
	}
}

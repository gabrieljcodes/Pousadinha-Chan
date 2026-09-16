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
	embeds := make([]*discordgo.MessageEmbed, 0, len(message.Embeds))
	fieldName := locale.Text("gacha.claim.claimed_by")
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
	return &discordgo.MessageEdit{ID: message.ID, Channel: channel, Content: &content, Components: &components, Embeds: &embeds, AllowedMentions: &discordgo.MessageAllowedMentions{}}
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
	claimErr := Default.Claim(ctx, i.GuildID, i.ChannelID, i.Member.User.ID, roll)
	content := locale.Text("gacha.discord.character_claimed_check_your_harem")
	if claimErr != nil {
		content = friendly(claimErr)
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

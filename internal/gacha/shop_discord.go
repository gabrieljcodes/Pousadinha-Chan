package gacha

import (
	"bot/internal/locale"
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

// RenderShop creates an interactive Discord shop message with buttons.
func (s *Store) RenderShop(ctx context.Context, guildID, userID string) (*discordgo.MessageSend, error) {
	catalog := s.GetShopCatalog()

	var balance int64
	_ = s.DB.QueryRowContext(ctx, `SELECT balance FROM guild_members WHERE guild_id=$1 AND user_id=$2`, guildID, userID).Scan(&balance)

	embed := &discordgo.MessageEmbed{
		Title:       locale.Text("gacha.shop.title"),
		Description: locale.Text("gacha.shop.description", locale.Data{"Balance": balance}),
		Color:       0xf1c40f, // Golden
		Fields:      make([]*discordgo.MessageEmbedField, 0, len(catalog)),
		Footer: &discordgo.MessageEmbedFooter{
			Text: "Pousadinha-Chan • Mudae-style Shop",
		},
	}

	for _, it := range catalog {
		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
			Name:   fmt.Sprintf("%s %s (%d 🪙)", it.Emoji, it.Name(), it.Price),
			Value:  fmt.Sprintf("ID: `%s`\n%s", it.ID, it.Description()),
			Inline: false,
		})
	}

	// Buttons for fast purchasing
	row1 := discordgo.ActionsRow{
		Components: []discordgo.MessageComponent{
			discordgo.Button{
				Label:    "📦 Chest (5k)",
				Style:    discordgo.PrimaryButton,
				CustomID: "gacha_shop_buy:lootbox",
			},
			discordgo.Button{
				Label:    "⏳ Rolls (2.5k)",
				Style:    discordgo.PrimaryButton,
				CustomID: "gacha_shop_buy:roll_reset",
			},
			discordgo.Button{
				Label:    "💍 Claim (8k)",
				Style:    discordgo.PrimaryButton,
				CustomID: "gacha_shop_buy:claim_reset",
			},
		},
	}

	row2 := discordgo.ActionsRow{
		Components: []discordgo.MessageComponent{
			discordgo.Button{
				Label:    "🛡️ Shield (10k)",
				Style:    discordgo.PrimaryButton,
				CustomID: "gacha_shop_buy:snipe_shield",
			},
			discordgo.Button{
				Label:    "🌟 Flare (4k)",
				Style:    discordgo.PrimaryButton,
				CustomID: "gacha_shop_buy:wish_flare",
			},
			discordgo.Button{
				Label:    "🔋 Battery (1k)",
				Style:    discordgo.PrimaryButton,
				CustomID: "gacha_shop_buy:gem_battery",
			},
		},
	}

	row3 := discordgo.ActionsRow{
		Components: []discordgo.MessageComponent{
			discordgo.Button{
				Label:    locale.Text("gacha.shop.btn_inventory"),
				Style:    discordgo.SecondaryButton,
				CustomID: "gacha_shop_inv",
			},
			discordgo.Button{
				Label:    locale.Text("gacha.shop.btn_open_box"),
				Style:    discordgo.SuccessButton,
				CustomID: "gacha_shop_open:lootbox",
			},
		},
	}

	return &discordgo.MessageSend{
		Embeds:     []*discordgo.MessageEmbed{embed},
		Components: []discordgo.MessageComponent{row1, row2, row3},
	}, nil
}

// RenderInventory creates an interactive Discord inventory message with active buffs and use buttons.
func (s *Store) RenderInventory(ctx context.Context, guildID, userID string) (*discordgo.MessageSend, error) {
	inv, err := s.GetPlayerInventory(ctx, guildID, userID)
	if err != nil {
		return nil, err
	}

	embed := &discordgo.MessageEmbed{
		Title:       locale.Text("gacha.inventory.title"),
		Description: locale.Text("gacha.inventory.description", locale.Data{"User": userID, "Balance": inv.Balance}),
		Color:       0x9b59b6, // Amethyst Purple
		Fields:      []*discordgo.MessageEmbedField{},
		Footer: &discordgo.MessageEmbedFooter{
			Text: "Use '!use <id>' or click below to activate consumables.",
		},
	}

	// 1. Consumable Items Field
	var bagLines []string
	if len(inv.Items) == 0 {
		bagLines = append(bagLines, locale.Text("gacha.inventory.consumables_empty"))
	} else {
		for _, it := range inv.Items {
			bagLines = append(bagLines, fmt.Sprintf("%s **%s** (`%s`): **%dx**", it.Item.Emoji, it.Item.Name(), it.Item.ID, it.Quantity))
		}
	}
	embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
		Name:   locale.Text("gacha.inventory.consumables_header"),
		Value:  strings.Join(bagLines, "\n"),
		Inline: false,
	})

	// 2. Active Perks & Permanent Upgrades Field
	var perkLines []string
	perkLines = append(perkLines, locale.Text("gacha.inventory.perm_rolls", locale.Data{"Count": inv.ExtraPermanentRolls}))
	perkLines = append(perkLines, locale.Text("gacha.inventory.perm_wish_slots", locale.Data{"Count": inv.ExtraWishSlots}))
	perkLines = append(perkLines, locale.Text("gacha.inventory.perm_wish_bonus", locale.Data{"Bonus": inv.PermanentWishBonus}))

	if inv.StoredExtraRolls > 0 {
		perkLines = append(perkLines, locale.Text("gacha.inventory.stored_rolls", locale.Data{"Count": inv.StoredExtraRolls}))
	}

	if inv.WishFlareRolls > 0 {
		perkLines = append(perkLines, locale.Text("gacha.inventory.wish_flare", locale.Data{"Count": inv.WishFlareRolls}))
	}

	if inv.SnipeShieldUntil.After(time.Now()) {
		perkLines = append(perkLines, locale.Text("gacha.inventory.snipe_shield_active", locale.Data{"Unix": inv.SnipeShieldUntil.Unix()}))
	} else {
		perkLines = append(perkLines, locale.Text("gacha.inventory.snipe_shield_inactive"))
	}

	embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
		Name:   locale.Text("gacha.inventory.perks_header"),
		Value:  strings.Join(perkLines, "\n"),
		Inline: false,
	})

	// Buttons: Provide quick "Use" buttons for items currently held in bag
	var useComponents []discordgo.MessageComponent
	for _, it := range inv.Items {
		if it.Quantity <= 0 {
			continue
		}
		btnLabel := fmt.Sprintf("%s Use %s (%d)", it.Item.Emoji, it.Item.Name(), it.Quantity)
		customID := "gacha_shop_use:" + string(it.Item.ID)
		if it.Item.ID == ItemLootbox {
			customID = "gacha_shop_open:lootbox"
			btnLabel = fmt.Sprintf("📦 Open Chest (%d)", it.Quantity)
		}
		useComponents = append(useComponents, discordgo.Button{
			Label:    btnLabel,
			Style:    discordgo.SecondaryButton,
			CustomID: customID,
		})
		if len(useComponents) >= 4 {
			break // Keep row compact
		}
	}

	useComponents = append(useComponents, discordgo.Button{
		Label:    "🛒 Astral Shop",
		Style:    discordgo.PrimaryButton,
		CustomID: "gacha_shop_catalog",
	})

	return &discordgo.MessageSend{
		Embeds:     []*discordgo.MessageEmbed{embed},
		Components: []discordgo.MessageComponent{discordgo.ActionsRow{Components: useComponents}},
	}, nil
}

// HandleShopComponent routes button clicks originating from shop and inventory embeds.
func HandleShopComponent(session *discordgo.Session, i *discordgo.InteractionCreate) {
	if session == nil || Default == nil || i == nil || i.Interaction == nil || i.Member == nil || i.Member.User == nil || i.GuildID == "" {
		return
	}

	customID := i.MessageComponentData().CustomID
	if !strings.HasPrefix(customID, "gacha_shop_") {
		return
	}

	// Immediate ephemeral acknowledgment
	if err := session.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral},
	}); err != nil {
		log.Printf("[gacha] shop component interaction ack failed: %v", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	sub := strings.TrimPrefix(customID, "gacha_shop_")
	userID := i.Member.User.ID
	guildID := i.GuildID

	var responseContent string
	var responseEmbeds []*discordgo.MessageEmbed

	switch {
	case strings.HasPrefix(sub, "buy:"):
		itemID := ItemID(strings.TrimPrefix(sub, "buy:"))
		buyRes, err := Default.BuyItem(ctx, guildID, userID, itemID, 1)
		if err != nil {
			responseContent = friendly(err)
		} else {
			responseContent = locale.Text("gacha.shop.buy_success", locale.Data{
				"Quantity":   buyRes.Quantity,
				"ItemName":   buyRes.Item.Name(),
				"TotalCost":  buyRes.TotalCost,
				"NewBalance": buyRes.NewBalance,
			})
		}

	case strings.HasPrefix(sub, "use:"):
		itemID := ItemID(strings.TrimPrefix(sub, "use:"))
		useRes, err := Default.UseItem(ctx, guildID, userID, itemID)
		if err != nil {
			responseContent = friendly(err)
		} else {
			responseContent = useRes.Message
		}

	case strings.HasPrefix(sub, "open:"):
		reward, err := Default.OpenLootbox(ctx, guildID, userID)
		if err != nil {
			responseContent = friendly(err)
		} else {
			embed := &discordgo.MessageEmbed{
				Title:       reward.Title,
				Description: reward.Description,
				Color:       0xe67e22, // Epic Orange
				Footer: &discordgo.MessageEmbedFooter{
					Text: "Cosmic Chest Rewards • Pousadinha Gacha",
				},
			}
			responseEmbeds = append(responseEmbeds, embed)
		}

	case sub == "inv":
		invMsg, err := Default.RenderInventory(ctx, guildID, userID)
		if err != nil {
			responseContent = friendly(err)
		} else {
			responseEmbeds = invMsg.Embeds
		}

	case sub == "catalog":
		shopMsg, err := Default.RenderShop(ctx, guildID, userID)
		if err != nil {
			responseContent = friendly(err)
		} else {
			responseEmbeds = shopMsg.Embeds
		}

	default:
		responseContent = locale.Text("gacha.discord.something_went_wrong_please_try_again")
	}

	edit := &discordgo.WebhookEdit{}
	if responseContent != "" {
		edit.Content = &responseContent
	}
	if len(responseEmbeds) > 0 {
		edit.Embeds = &responseEmbeds
	}

	if _, err := session.InteractionResponseEdit(i.Interaction, edit, discordgo.WithContext(ctx)); err != nil {
		log.Printf("[gacha] shop component response edit failed: %v", err)
	}
}

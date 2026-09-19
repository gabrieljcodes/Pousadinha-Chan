package gacha

import (
	"bot/internal/locale"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

// RenderBuildOverview builds the main player profile & build tree overview embed.
func (s *Store) RenderBuildOverview(ctx context.Context, guildID, userID string) (*discordgo.MessageSend, error) {
	profile, err := s.GetPlayerBuildProfile(ctx, guildID, userID)
	if err != nil {
		return nil, err
	}

	classes := s.GetClasses()

	embed := &discordgo.MessageEmbed{
		Title:       locale.Text("gacha.builds.title"),
		Description: locale.Text("gacha.builds.desc", locale.Data{"Available": profile.AvailablePoints, "Total": profile.TotalPoints, "Spent": profile.SpentPoints}),
		Color:       0x9b59b6, // Amethyst purple
		Fields:      make([]*discordgo.MessageEmbedField, 0, len(classes)+1),
		Footer: &discordgo.MessageEmbedFooter{
			Text: locale.Text("gacha.builds.footer"),
		},
	}

	for _, c := range classes {
		tier := profile.ClassTiers[c.ID]
		tierStr := locale.Text("gacha.builds.not_started")
		if tier > 0 {
			tierStr = fmt.Sprintf("Tier %d / 4", tier)
		}

		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
			Name:   fmt.Sprintf("%s %s • %s", c.Emoji, c.Name, tierStr),
			Value:  fmt.Sprintf("*%s*\n%s", c.Title, c.Description),
			Inline: false,
		})
	}

	// Class selection buttons
	row1 := discordgo.ActionsRow{
		Components: []discordgo.MessageComponent{
			discordgo.Button{
				Label:    "🃏 Trapaceiro",
				Style:    discordgo.PrimaryButton,
				CustomID: "gacha_build_class:trickster",
			},
			discordgo.Button{
				Label:    "🔮 Oráculo",
				Style:    discordgo.PrimaryButton,
				CustomID: "gacha_build_class:oracle",
			},
			discordgo.Button{
				Label:    "⚖️ Mercador",
				Style:    discordgo.PrimaryButton,
				CustomID: "gacha_build_class:merchant",
			},
			discordgo.Button{
				Label:    "🛡️ Guardião",
				Style:    discordgo.PrimaryButton,
				CustomID: "gacha_build_class:guardian",
			},
		},
	}

	row2 := discordgo.ActionsRow{
		Components: []discordgo.MessageComponent{
			discordgo.Button{
				Label:    locale.Text("gacha.builds.btn_info"),
				Style:    discordgo.SecondaryButton,
				CustomID: "gacha_build_info",
				Emoji:    &discordgo.ComponentEmoji{Name: "📜"},
			},
			discordgo.Button{
				Label:    locale.Text("gacha.builds.btn_respec"),
				Style:    discordgo.DangerButton,
				CustomID: "gacha_build_respec",
				Emoji:    &discordgo.ComponentEmoji{Name: "🔄"},
			},
		},
	}

	return &discordgo.MessageSend{
		Embeds:     []*discordgo.MessageEmbed{embed},
		Components: []discordgo.MessageComponent{row1, row2},
	}, nil
}

// RenderClassTree builds an interactive embed showing the specific class skill tree.
func (s *Store) RenderClassTree(ctx context.Context, guildID, userID string, classID SkillClassID) (*discordgo.MessageSend, error) {
	class, ok := s.GetClassInfo(classID)
	if !ok {
		return nil, ErrSkillNotFound
	}

	profile, err := s.GetPlayerBuildProfile(ctx, guildID, userID)
	if err != nil {
		return nil, err
	}

	skills := s.GetClassSkills(classID)

	embed := &discordgo.MessageEmbed{
		Title: fmt.Sprintf("%s Árvore de Habilidades: %s", class.Emoji, class.Name),
		Description: fmt.Sprintf(
			"**%s**\n%s\n\n🎯 **Pontos Disponíveis:** `%d / %d` (Gastos: `%d`)\n─────────────────────────",
			class.Title, class.Description, profile.AvailablePoints, profile.TotalPoints, profile.SpentPoints,
		),
		Color:  0x3498db, // Sky blue
		Fields: make([]*discordgo.MessageEmbedField, 0, len(skills)),
		Footer: &discordgo.MessageEmbedFooter{
			Text: "Selecione uma habilidade para aprender ou volte ao menu.",
		},
	}

	var learnButtons []discordgo.MessageComponent

	for _, sk := range skills {
		status := "🔒 Bloqueado"
		isUnlocked := profile.UnlockedSkills[sk.ID]
		canUnlock := false

		if isUnlocked {
			status = "🟢 Ativo"
		} else {
			prereqMet := sk.PrereqID == "" || profile.UnlockedSkills[sk.PrereqID]
			hasPoints := profile.AvailablePoints >= sk.PointsCost
			if prereqMet && hasPoints {
				status = "🔓 Disponível"
				canUnlock = true
			} else if prereqMet && !hasPoints {
				status = fmt.Sprintf("⚠️ Pontos insuficientes (requer %d BP)", sk.PointsCost)
			} else {
				prereqSkill, _ := s.GetSkillDef(sk.PrereqID)
				status = fmt.Sprintf("🔒 Requer: %s (Tier %d)", prereqSkill.Name, prereqSkill.Tier)
			}
		}

		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
			Name:   fmt.Sprintf("[Tier %d] %s %s (%d BP) — %s", sk.Tier, sk.Emoji, sk.Name, sk.PointsCost, status),
			Value:  sk.Description,
			Inline: false,
		})

		if canUnlock && len(learnButtons) < 3 {
			learnButtons = append(learnButtons, discordgo.Button{
				Label:    fmt.Sprintf("Aprender T%d: %s (%d BP)", sk.Tier, sk.Name, sk.PointsCost),
				Style:    discordgo.SuccessButton,
				CustomID: "gacha_build_learn:" + sk.ID,
				Emoji:    &discordgo.ComponentEmoji{Name: sk.Emoji},
			})
		}
	}

	var rows []discordgo.MessageComponent

	if len(learnButtons) > 0 {
		rows = append(rows, discordgo.ActionsRow{Components: learnButtons})
	}

	navRow := discordgo.ActionsRow{
		Components: []discordgo.MessageComponent{
			discordgo.Button{
				Label:    "🔙 Voltar para Visão Geral",
				Style:    discordgo.SecondaryButton,
				CustomID: "gacha_build_overview",
			},
			discordgo.Button{
				Label:    locale.Text("gacha.builds.btn_respec"),
				Style:    discordgo.DangerButton,
				CustomID: "gacha_build_respec",
				Emoji:    &discordgo.ComponentEmoji{Name: "🔄"},
			},
		},
	}
	rows = append(rows, navRow)

	return &discordgo.MessageSend{
		Embeds:     []*discordgo.MessageEmbed{embed},
		Components: rows,
	}, nil
}

// HandleBuildComponent routes button interactions for the build tree system.
func HandleBuildComponent(session *discordgo.Session, i *discordgo.InteractionCreate) {
	if session == nil || Default == nil || i == nil || i.Interaction == nil || i.Type != discordgo.InteractionMessageComponent || i.Member == nil || i.Member.User == nil || i.GuildID == "" {
		return
	}

	customID := i.MessageComponentData().CustomID
	if !strings.HasPrefix(customID, "gacha_build_") {
		return
	}

	guildID := i.GuildID
	userID := i.Member.User.ID

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if strings.HasPrefix(customID, "gacha_build_class:") {
		classID := SkillClassID(strings.TrimPrefix(customID, "gacha_build_class:"))
		msg, err := Default.RenderClassTree(ctx, guildID, userID, classID)
		if err != nil {
			errText := friendly(err)
			_ = session.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{Content: errText, Flags: discordgo.MessageFlagsEphemeral},
			})
			return
		}
		_ = session.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Embeds:     msg.Embeds,
				Components: msg.Components,
			},
		})
		return
	}

	if customID == "gacha_build_overview" {
		msg, err := Default.RenderBuildOverview(ctx, guildID, userID)
		if err != nil {
			errText := friendly(err)
			_ = session.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{Content: errText, Flags: discordgo.MessageFlagsEphemeral},
			})
			return
		}
		_ = session.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Embeds:     msg.Embeds,
				Components: msg.Components,
			},
		})
		return
	}

	if strings.HasPrefix(customID, "gacha_build_learn:") {
		skillID := strings.TrimPrefix(customID, "gacha_build_learn:")
		_, err := Default.UnlockSkill(ctx, guildID, userID, skillID)
		if err != nil {
			errText := friendly(err)
			_ = session.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{Content: errText, Flags: discordgo.MessageFlagsEphemeral},
			})
			return
		}

		skill, _ := Default.GetSkillDef(skillID)
		// Update view
		msg, err := Default.RenderClassTree(ctx, guildID, userID, skill.ClassID)
		if err != nil {
			return
		}
		_ = session.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Embeds:     msg.Embeds,
				Components: msg.Components,
			},
		})
		return
	}

	if customID == "gacha_build_respec" {
		_, err := Default.RespecBuild(ctx, guildID, userID)
		if err != nil {
			errText := friendly(err)
			_ = session.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{Content: errText, Flags: discordgo.MessageFlagsEphemeral},
			})
			return
		}

		msg, err := Default.RenderBuildOverview(ctx, guildID, userID)
		if err != nil {
			return
		}
		_ = session.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Content:    locale.Text("gacha.builds.respec_success"),
				Embeds:     msg.Embeds,
				Components: msg.Components,
			},
		})
		return
	}

	if customID == "gacha_build_info" {
		profile, err := Default.GetPlayerBuildProfile(ctx, guildID, userID)
		if err != nil {
			return
		}

		infoEmbed := &discordgo.MessageEmbed{
			Title:       locale.Text("gacha.builds.info_title"),
			Description: locale.Text("gacha.builds.info_desc"),
			Color:       0x1abc9c,
			Fields: []*discordgo.MessageEmbedField{
				{
					Name:   fmt.Sprintf("🎲 Marcos de Rolls (%d rolls)", profile.TotalRolls),
					Value:  "• 25 rolls: +1 BP\n• 100 rolls: +1 BP\n• 250 rolls: +1 BP\n• 500 rolls: +1 BP\n• 1.000 rolls: +1 BP *(Máx: 5 BP)*",
					Inline: true,
				},
				{
					Name:   fmt.Sprintf("💍 Marcos de Casamento (%d no harém)", profile.TotalClaims),
					Value:  "• 5 claims: +1 BP\n• 20 claims: +1 BP\n• 50 claims: +1 BP\n• 100 claims: +1 BP\n• 200 claims: +1 BP *(Máx: 5 BP)*",
					Inline: true,
				},
				{
					Name:   fmt.Sprintf("🔑 Marcos de Chaves (Max: %d, Soma: %d)", profile.MaxKey, profile.TotalKeys),
					Value:  "• Chave Nível 2+: +1 BP\n• Chave Nível 4+: +1 BP\n• 10 Chaves Totais: +1 BP *(Máx: 3 BP)*",
					Inline: true,
				},
				{
					Name:   fmt.Sprintf("📖 Grimórios da Loja (%d comprados)", profile.TomesBought),
					Value:  "• Grimório Arcano na Loja Astral (5.000 🪙): +1 BP cada *(Máx: 3 BP)*",
					Inline: false,
				},
			},
			Footer: &discordgo.MessageEmbedFooter{
				Text: fmt.Sprintf("Limite Máximo Global: %d Pontos de Build.", MaxBuildPoints),
			},
		}

		_ = session.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Embeds: []*discordgo.MessageEmbed{infoEmbed},
				Flags:  discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}
}

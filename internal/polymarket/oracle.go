package polymarket

import (
	"bot/internal/database"
	"bot/pkg/config"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

var (
	oracleTicker *time.Ticker
	oracleDone   chan struct{}
)

// StartOracle starts the background ticker worker that syncs active markets
// and automatically resolves payouts with winner mentions.
func StartOracle(s *discordgo.Session) {
	if oracleTicker != nil {
		return
	}

	oracleTicker = time.NewTicker(2 * time.Minute)
	oracleDone = make(chan struct{})

	log.Println("[PolymarketOracle] Background sync and resolution worker started (interval: 2m)")

	go func() {
		for {
			select {
			case <-oracleDone:
				return
			case <-oracleTicker.C:
				runOracleSyncCycle(s)
			}
		}
	}()
}

// StopOracle terminates the oracle worker gracefully
func StopOracle() {
	if oracleTicker != nil {
		oracleTicker.Stop()
		close(oracleDone)
		oracleTicker = nil
	}
}

// runOracleSyncCycle queries all active markets from PostgreSQL and checks Polymarket
func runOracleSyncCycle(s *discordgo.Session) {
	markets, err := database.GetActivePolymarketMarketsDB()
	if err != nil {
		log.Printf("[PolymarketOracle] Error loading active markets: %v", err)
		return
	}

	if len(markets) == 0 {
		return
	}

	client := GetClient()

	for _, m := range markets {
		syncSingleMarket(s, client, m)
	}
}

func syncSingleMarket(s *discordgo.Session, client *PolymarketClient, m *database.DBPolymarketMarket) {
	// Query current state from Polymarket
	gammaMarket, err := client.GetMarketByID(m.PolymarketID)
	if err != nil {
		// Fallback to slug
		gammaMarket, err = client.GetMarketBySlug(m.Slug)
		if err != nil {
			log.Printf("[PolymarketOracle] Warning: failed to fetch market %s (%s): %v", m.ID, m.Slug, err)
			return
		}
	}

	yesPrice, noPrice, _ := gammaMarket.GetPrices()

	// Check if market resolved
	if gammaMarket.Closed {
		winner := gammaMarket.GetWinningOutcome()
		if winner == "Yes" || winner == "No" {
			// Resolve and payout 100 EC per share
			payouts, err := database.ResolvePolymarketMarketDB(m.ID, winner)
			if err != nil {
				log.Printf("[PolymarketOracle] Error resolving market %s: %v", m.ID, err)
				return
			}

			m.Status = "resolved"
			m.Winner = winner
			m.YesPrice = yesPrice
			m.NoPrice = noPrice

			settings, _ := database.GetGuildPolymarketSettings(m.GuildID)
			if settings == nil {
				settings = &database.DBPolymarketSettings{HouseEdge: 0.03}
			}

			// Update Discord Embed
			embed := CreateMarketEmbed(m, settings)
			components := CreateMarketComponents(m, settings)

			if m.ChannelID != "" && m.MessageID != "" {
				_, _ = s.ChannelMessageEditComplex(&discordgo.MessageEdit{
					Channel:    m.ChannelID,
					ID:         m.MessageID,
					Embeds:     &[]*discordgo.MessageEmbed{embed},
					Components: &components,
				})

				// Send winner announcement with mentions!
				announceWinners(s, m, winner, payouts)
			}
			return
		} else if winner == "cancelled" {
			// Cancel and refund all investments
			refunds, err := database.CancelAndRefundPolymarketMarketDB(m.ID)
			if err != nil {
				log.Printf("[PolymarketOracle] Error cancelling market %s: %v", m.ID, err)
				return
			}

			m.Status = "cancelled"
			m.Winner = "cancelled"

			settings, _ := database.GetGuildPolymarketSettings(m.GuildID)
			if settings == nil {
				settings = &database.DBPolymarketSettings{HouseEdge: 0.03}
			}

			embed := CreateMarketEmbed(m, settings)
			components := CreateMarketComponents(m, settings)

			if m.ChannelID != "" && m.MessageID != "" {
				_, _ = s.ChannelMessageEditComplex(&discordgo.MessageEdit{
					Channel:    m.ChannelID,
					ID:         m.MessageID,
					Embeds:     &[]*discordgo.MessageEmbed{embed},
					Components: &components,
				})

				announcement := fmt.Sprintf("⚠️ **MERCADO CANCELADO NO POLYMARKET**\nO evento **\"%s\"** foi cancelado oficialmente. Todas as apostas de %d participantes foram reembolsadas integralmente!", m.Question, len(refunds))
				_, _ = s.ChannelMessageSend(m.ChannelID, announcement)
			}
			return
		}
	}

	// Update prices if changed significantly
	if mathAbs(m.YesPrice-yesPrice) > 0.005 || mathAbs(m.NoPrice-noPrice) > 0.005 {
		_ = database.UpdatePolymarketPricesDB(m.ID, yesPrice, noPrice, "open")
		m.YesPrice = yesPrice
		m.NoPrice = noPrice

		settings, _ := database.GetGuildPolymarketSettings(m.GuildID)
		if settings == nil {
			settings = &database.DBPolymarketSettings{HouseEdge: 0.03}
		}

		embed := CreateMarketEmbed(m, settings)
		components := CreateMarketComponents(m, settings)

		if m.ChannelID != "" && m.MessageID != "" {
			_, _ = s.ChannelMessageEditComplex(&discordgo.MessageEdit{
				Channel:    m.ChannelID,
				ID:         m.MessageID,
				Embeds:     &[]*discordgo.MessageEmbed{embed},
				Components: &components,
			})
		}
	}
}

func announceWinners(s *discordgo.Session, m *database.DBPolymarketMarket, winner string, payouts map[string]int64) {
	if len(payouts) == 0 {
		msg := fmt.Sprintf("🏆 **MERCADO RESOLVIDO NO POLYMARKET!**\nO evento **\"%s\"** foi concluído com resultado: **%s**.\nNenhum membro do servidor possuía ações vencedoras.", m.Question, strings.ToUpper(winner))
		_, _ = s.ChannelMessageSend(m.ChannelID, msg)
		return
	}

	var winnerMentions []string
	var totalPaid int64

	for userID, amount := range payouts {
		winnerMentions = append(winnerMentions, fmt.Sprintf("<@%s> (+%d %s)", userID, amount, config.Bot.CurrencySymbol))
		totalPaid += amount
	}

	announcement := fmt.Sprintf("🏆 **MERCADO POLYMARKET RESOLVIDO!**\n\n"+
		"📌 **Evento:** %s\n"+
		"🎯 **Resultado Oficial:** **%s**\n\n"+
		"🎉 **Parabéns aos Vencedores (100 EC por ação):**\n%s\n\n"+
		"💰 **Total distribuído:** **%d %s**!",
		m.Question,
		strings.ToUpper(winner),
		strings.Join(winnerMentions, ", "),
		totalPaid,
		config.Bot.CurrencySymbol,
	)

	// If text too long for single message, split or truncate gracefully
	if len(announcement) > 1950 {
		announcement = announcement[:1940] + "...\n*(e outros vencedores!)*"
	}

	_, _ = s.ChannelMessageSend(m.ChannelID, announcement)
}

func mathAbs(a float64) float64 {
	if a < 0 {
		return -a
	}
	return a
}

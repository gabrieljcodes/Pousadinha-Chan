package stockmarket

import (
	"encoding/json"
	"estudocoin/internal/database"
	"log"
	"os"
	"time"

	"github.com/bwmarrin/discordgo"
)

var Companies []Company

func LoadCompanies() error {
	file, err := os.ReadFile("internal/stockmarket/companies.json")
	if err != nil {
		return err
	}
	return json.Unmarshal(file, &Companies)
}

func Start(s *discordgo.Session) {
	if err := LoadCompanies(); err != nil {
		log.Println("Error loading companies:", err)
		return
	}

	go marketLoop(s)
}

func marketLoop(s *discordgo.Session) {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	// Initial check on startup
	checkMarket(s)

	for range ticker.C {
		checkMarket(s)
	}
}

func checkMarket(s *discordgo.Session) {
	log.Println("Checking stock market...")
	for _, company := range Companies {
		data, err := GetStockPrice(company.Ticker)
		if err != nil {
			log.Printf("Error fetching price for %s: %v", company.Ticker, err)
			continue
		}

		oldPrice, err := database.GetStockPriceDB(company.Ticker)
		if err != nil {
			log.Printf("Error getting old price for %s: %v", company.Ticker, err)
			continue
		}

		// Update price in DB
		err = database.SetStockPriceDB(company.Ticker, data.Price)
		if err != nil {
			log.Printf("Error updating price for %s: %v", company.Ticker, err)
			continue
		}

		// Calculate logic
		if oldPrice == 0 {
			// First run or new stock, no payout
			continue
		}

		if data.Price > oldPrice {
			diff := data.Price - oldPrice
			
			// Distribute rewards
			investments, err := database.GetAllInvestmentsByTicker(company.Ticker)
			if err != nil {
				log.Printf("Error getting investments for %s: %v", company.Ticker, err)
				continue
			}

			for _, inv := range investments {
				// Payout = Shares * PriceDiff
				payout := int(inv.Shares * diff)
				if payout > 0 {
					err := database.AddCoins(inv.UserID, payout)
					if err != nil {
						log.Printf("Failed to pay dividends to %s: %v", inv.UserID, err)
					}
					// Optional: Notify user? Maybe too spammy.
				}
			}
			log.Printf("Distributed dividends for %s (Growth: %.2f)", company.Ticker, diff)
		}
	}
}

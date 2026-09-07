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
		file, err = os.ReadFile("companies.json")
		if err != nil {
			return err
		}
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

		// Update price in DB (store real price)
		err = database.SetStockPriceDB(company.Ticker, data.Price)
		if err != nil {
			log.Printf("Error updating price for %s: %v", company.Ticker, err)
			continue
		}
	}
	log.Println("Stock market prices updated successfully.")
}

package polymarket

import (
	"estudocoin/internal/database"
	"strings"
	"testing"
)

func TestCalculateShareCost(t *testing.T) {
	// Price 0.65 (65 EC per share), 10 shares, 3% house edge
	// RawCost = 65 * 10 = 650 EC
	// Fee = 650 * 0.03 = 19.5 -> 20 EC
	// TotalCost = 670 EC
	// PotentialPayout = 10 * 100 = 1000 EC
	// PotentialProfit = 1000 - 670 = 330 EC
	cost := CalculateShareCost(0.65, 10, 0.03)

	if cost.Shares != 10 {
		t.Errorf("Expected 10 shares, got %d", cost.Shares)
	}
	if cost.RawCost != 650 {
		t.Errorf("Expected raw cost 650, got %d", cost.RawCost)
	}
	if cost.Fee != 20 {
		t.Errorf("Expected fee 20, got %d", cost.Fee)
	}
	if cost.TotalCost != 670 {
		t.Errorf("Expected total cost 670, got %d", cost.TotalCost)
	}
	if cost.PotentialPayout != 1000 {
		t.Errorf("Expected potential payout 1000, got %d", cost.PotentialPayout)
	}
	if cost.PotentialProfit != 330 {
		t.Errorf("Expected profit 330, got %d", cost.PotentialProfit)
	}
}

func TestCalculateSharesFromBudget(t *testing.T) {
	// Budget 1000 EC, Price 0.40, Fee 3%
	// Effective cost per share ~ 41.2 EC -> ~24 shares
	cost := CalculateSharesFromBudget(0.40, 1000, 0.03)

	if cost.TotalCost > 1000 {
		t.Errorf("Cost %d exceeded budget 1000", cost.TotalCost)
	}
	if cost.Shares < 20 || cost.Shares > 25 {
		t.Errorf("Unexpected share count %d for 1000 budget at 0.40", cost.Shares)
	}
}

func TestFormatProbabilityBar(t *testing.T) {
	bar := FormatProbabilityBar(0.70, 0.30)

	if !strings.Contains(bar, "70%") || !strings.Contains(bar, "30%") {
		t.Errorf("Bar missing percentage text: %s", bar)
	}
	if !strings.Contains(bar, "█") || !strings.Contains(bar, "░") {
		t.Errorf("Bar missing visual blocks: %s", bar)
	}
}

func TestExtractSlug(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"xi-jinping-out-before-2027", "xi-jinping-out-before-2027"},
		{"https://polymarket.com/event/fed-rate-cuts-in-2025", "fed-rate-cuts-in-2025"},
		{"polymarket.com/market/bitcoin-100k?tab=overview", "bitcoin-100k"},
		{"559651", "559651"},
		{"https://gamma-api.polymarket.com/events/super-bowl-2025/", "super-bowl-2025"},
	}

	for _, tt := range tests {
		got := ExtractSlug(tt.input)
		if got != tt.expected {
			t.Errorf("ExtractSlug(%q) = %q, expected %q", tt.input, got, tt.expected)
		}
	}
}

func TestGammaMarketOutcomeDetection(t *testing.T) {
	// Active market
	m1 := &GammaMarket{
		OutcomePrices: `["0.65", "0.35"]`,
		Closed:        false,
	}
	if w := m1.GetWinningOutcome(); w != "" {
		t.Errorf("Expected empty winner for open market, got %q", w)
	}

	// Resolved to Yes
	mYes := &GammaMarket{
		OutcomePrices: `["0.999", "0.001"]`,
		Closed:        true,
	}
	if w := mYes.GetWinningOutcome(); w != "Yes" {
		t.Errorf("Expected Yes winner, got %q", w)
	}

	// Resolved to No
	mNo := &GammaMarket{
		OutcomePrices: `["0.002", "0.998"]`,
		Closed:        true,
	}
	if w := mNo.GetWinningOutcome(); w != "No" {
		t.Errorf("Expected No winner, got %q", w)
	}

	// Cancelled / Tie
	mCancel := &GammaMarket{
		OutcomePrices: `["0.50", "0.50"]`,
		Closed:        true,
	}
	if w := mCancel.GetWinningOutcome(); w != "cancelled" {
		t.Errorf("Expected cancelled, got %q", w)
	}
}

func TestCreateMarketEmbedAndComponents(t *testing.T) {
	dbMarket := &database.DBPolymarketMarket{
		ID:           "poly_12345",
		PolymarketID: "12345",
		Question:     "Will Bitcoin hit 100k?",
		Description:  "Resolves Yes if BTC hits 100k",
		Category:     "Crypto",
		YesPrice:     0.70,
		NoPrice:      0.30,
		Status:       "open",
	}
	settings := &database.DBPolymarketSettings{
		HouseEdge: 0.03,
	}

	embed := CreateMarketEmbed(dbMarket, settings)
	if embed == nil {
		t.Fatal("Expected non-nil embed")
	}
	if !strings.Contains(embed.Description, "Will Bitcoin hit 100k?") {
		t.Errorf("Embed description missing question: %s", embed.Description)
	}

	comps := CreateMarketComponents(dbMarket, settings)
	if len(comps) == 0 {
		t.Fatal("Expected components row")
	}
}

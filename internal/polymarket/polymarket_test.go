package polymarket

import (
	"bot/internal/database"
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
		{"https://polymarket.com/pt/event/brazil-presidential-election", "brazil-presidential-election"},
		{"https://polymarket.com/es/event/brazil-presidential-election?tid=123", "brazil-presidential-election"},
		{"polymarket.com/market/bitcoin-100k?tab=overview", "bitcoin-100k"},
		{"559651", "559651"},
		{"https://gamma-api.polymarket.com/events/super-bowl-2025/", "super-bowl-2025"},
		{"https://gamma-api.polymarket.com/markets?id=601819", "601819"},
	}

	for _, tt := range tests {
		got := ExtractSlug(tt.input)
		if got != tt.expected {
			t.Errorf("ExtractSlug(%q) = %q, expected %q", tt.input, got, tt.expected)
		}
	}
}

func TestMatchCandidate(t *testing.T) {
	m1 := &GammaMarket{
		GroupItemTitle: "Luiz Inácio Lula da Silva",
		Question:       "Will Luiz Inácio Lula da Silva win the 2026 Brazilian presidential election?",
	}
	m2 := &GammaMarket{
		GroupItemTitle: "Flávio Bolsonaro",
		Question:       "Will Flávio Bolsonaro win the 2026 Brazilian presidential election?",
	}
	m3 := &GammaMarket{
		GroupItemTitle: "Tarcisio de Freitas",
		Question:       "Will Tarcisio de Freitas win the 2026 Brazilian presidential election?",
	}

	if !MatchCandidate(m1, "lula") {
		t.Errorf("Expected match for 'lula' on Lula market")
	}
	if !MatchCandidate(m1, "LULA") {
		t.Errorf("Expected case-insensitive match for 'LULA'")
	}
	if !MatchCandidate(m1, "inacio") {
		t.Errorf("Expected accent-insensitive match for 'inacio' on 'Luiz Inácio'")
	}
	if MatchCandidate(m1, "bolsonaro") {
		t.Errorf("Did not expect match for 'bolsonaro' on Lula market")
	}
	if !MatchCandidate(m2, "flavio") {
		t.Errorf("Expected match for 'flavio' on Flávio Bolsonaro")
	}
	if !MatchCandidate(m3, "tarcisio") {
		t.Errorf("Expected match for 'tarcisio' on Tarcisio de Freitas")
	}
}

func TestSortMarketsByRelevance(t *testing.T) {
	tarcisio := &GammaMarket{
		ID:             "601818",
		GroupItemTitle: "Tarcisio de Freitas",
		OutcomePrices:  `["0.0005", "0.9995"]`,
		VolumeNum:      14000000,
		Active:         true,
	}
	lula := &GammaMarket{
		ID:             "601819",
		GroupItemTitle: "Luiz Inácio Lula da Silva",
		OutcomePrices:  `["0.53", "0.47"]`,
		VolumeNum:      10000000,
		Active:         true,
	}
	bolsonaro := &GammaMarket{
		ID:             "601826",
		GroupItemTitle: "Flávio Bolsonaro",
		OutcomePrices:  `["0.45", "0.55"]`,
		VolumeNum:      9000000,
		Active:         true,
	}
	inactive := &GammaMarket{
		ID:             "601840",
		GroupItemTitle: "Person R",
		OutcomePrices:  `["0.0", "1.0"]`,
		VolumeNum:      0,
		Active:         false,
	}

	raw := []*GammaMarket{tarcisio, inactive, bolsonaro, lula}
	sorted := SortMarketsByRelevance(raw)

	if sorted[0].ID != "601819" {
		t.Errorf("Expected Lula (53%%) to be 1st, got %s (%s)", sorted[0].GroupItemTitle, sorted[0].ID)
	}
	if sorted[1].ID != "601826" {
		t.Errorf("Expected Bolsonaro (45%%) to be 2nd, got %s (%s)", sorted[1].GroupItemTitle, sorted[1].ID)
	}
	if sorted[2].ID != "601818" {
		t.Errorf("Expected Tarcísio to be 3rd, got %s (%s)", sorted[2].GroupItemTitle, sorted[2].ID)
	}
	if sorted[3].ID != "601840" {
		t.Errorf("Expected inactive market to be last, got %s (%s)", sorted[3].GroupItemTitle, sorted[3].ID)
	}
}

func TestLiveEventResolution(t *testing.T) {
	client := GetClient()

	// 1. Multi-candidate event without filter -> should return event with sorted candidates
	m, event, err := client.ResolveImportQuery("brazil-presidential-election", "")
	if err != nil {
		t.Logf("Polymarket API unreachable or failed: %v (skipping live test)", err)
		return
	}
	if m != nil {
		t.Errorf("Expected nil market for multi-candidate event without filter, got %v", m)
	}
	if event == nil || len(event.Markets) == 0 {
		t.Fatalf("Expected non-nil event with markets")
	}

	// Verify that Lula is #1 in sorted markets
	top := event.Markets[0]
	if !strings.Contains(top.Question, "Lula") {
		t.Errorf("Expected top sorted candidate to be Lula, got %s", top.Question)
	}

	// 2. Multi-candidate event with candidate "lula" -> should return Lula market directly
	mLula, _, err := client.ResolveImportQuery("https://polymarket.com/pt/event/brazil-presidential-election", "lula")
	if err != nil {
		t.Fatalf("Failed to resolve lula: %v", err)
	}
	if mLula == nil || !strings.Contains(mLula.Question, "Lula") {
		t.Errorf("Expected Lula market, got %v", mLula)
	}
	if mLula.ID != "601819" {
		t.Errorf("Expected Lula market ID 601819, got %s", mLula.ID)
	}

	// 3. Multi-candidate event with candidate "flavio" -> should return Flavio Bolsonaro market
	mFlavio, _, err := client.ResolveImportQuery("brazil-presidential-election", "flavio")
	if err != nil {
		t.Fatalf("Failed to resolve flavio: %v", err)
	}
	if mFlavio == nil || !strings.Contains(mFlavio.Question, "Flávio") {
		t.Errorf("Expected Flavio Bolsonaro market, got %v", mFlavio)
	}
	if mFlavio.ID != "601826" {
		t.Errorf("Expected Flavio Bolsonaro market ID 601826, got %s", mFlavio.ID)
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

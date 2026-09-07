package stockmarket

import (
	"math"
	"testing"
)

func TestLoadCompanies(t *testing.T) {
	// Temporarily adjust working directory or relative path
	err := LoadCompanies()
	if err != nil {
		t.Fatalf("LoadCompanies failed: %v", err)
	}

	if len(Companies) == 0 {
		t.Fatal("Expected companies to be loaded, got 0")
	}

	foundNVDA := false
	foundBTC := false
	foundGOLD := false
	for _, c := range Companies {
		if c.Ticker == "NVDA" {
			foundNVDA = true
		}
		if c.Ticker == "BTC" {
			foundBTC = true
		}
		if c.Ticker == "GOLD" {
			foundGOLD = true
		}
	}

	if !foundNVDA || !foundBTC || !foundGOLD {
		t.Errorf("Expected to find NVDA, BTC, and GOLD in companies list")
	}
}

func TestGetStockPriceYahooLive(t *testing.T) {
	// Test standard stock ticker
	resp, err := GetStockPrice("AAPL")
	if err != nil {
		t.Fatalf("GetStockPrice(AAPL) failed: %v", err)
	}
	if resp.Ticker != "AAPL" {
		t.Errorf("Expected ticker AAPL, got %s", resp.Ticker)
	}
	if resp.Price <= 0 {
		t.Errorf("Expected price > 0, got %.2f", resp.Price)
	}
	if resp.Name == "" {
		t.Errorf("Expected non-empty company name")
	}

	// Test crypto alias (BTC -> BTC-USD)
	btcResp, err := GetStockPrice("BTC")
	if err != nil {
		t.Fatalf("GetStockPrice(BTC) failed: %v", err)
	}
	if btcResp.Price < 1000 {
		t.Errorf("Expected BTC price > 1000, got %.2f", btcResp.Price)
	}

	// Test futures alias (GOLD -> GC=F)
	goldResp, err := GetStockPrice("GOLD")
	if err != nil {
		t.Fatalf("GetStockPrice(GOLD) failed: %v", err)
	}
	if goldResp.Price < 500 {
		t.Errorf("Expected GOLD price > 500, got %.2f", goldResp.Price)
	}
}

func TestStockCacheTTL(t *testing.T) {
	// Clear cache for test
	stockCacheMu.Lock()
	stockCache = make(map[string]cachedStock)
	stockCacheMu.Unlock()

	// First fetch
	resp1, err := GetStockPrice("MSFT")
	if err != nil {
		t.Fatalf("First fetch failed: %v", err)
	}

	// Verify cached
	stockCacheMu.RLock()
	cached, found := stockCache["MSFT"]
	stockCacheMu.RUnlock()

	if !found {
		t.Fatal("Expected MSFT to be in cache")
	}
	if cached.data.Price != resp1.Price {
		t.Errorf("Cached price mismatch: got %.2f, expected %.2f", cached.data.Price, resp1.Price)
	}

	// Second fetch should use cache directly
	resp2, err := GetStockPrice("MSFT")
	if err != nil {
		t.Fatalf("Second fetch failed: %v", err)
	}
	if resp2.Price != resp1.Price {
		t.Errorf("Expected cached price to match: %.2f vs %.2f", resp2.Price, resp1.Price)
	}
}

func TestStockMathAndRounding(t *testing.T) {
	price := 123.456
	shares := 2.5

	// Payout calculation with Rounding vs Truncation
	rawPayout := shares * price // 308.64
	roundedPayout := int(math.Round(rawPayout))
	truncatedPayout := int(rawPayout)

	if roundedPayout != 309 {
		t.Errorf("Expected rounded payout to be 309, got %d", roundedPayout)
	}
	if truncatedPayout != 308 {
		t.Errorf("Expected truncated payout to be 308, got %d", truncatedPayout)
	}

	// Profit / Loss calculation
	costBasis := 250.0
	currentVal := rawPayout
	pnl := currentVal - costBasis
	pnlPct := (pnl / costBasis) * 100.0

	if pnl <= 0 || pnlPct <= 0 {
		t.Errorf("Expected positive PnL and return percentage")
	}
}

func TestStockPnLEmoji(t *testing.T) {
	cases := []struct {
		val      float64
		cost     float64
		expected string
	}{
		{val: 200.0, cost: 100.0, expected: "🟢"},
		{val: 50.0, cost: 100.0, expected: "🔴"},
		{val: 100.0, cost: 100.0, expected: "⚪"},
	}

	for _, c := range cases {
		pnl := c.val - c.cost
		emoji := ""
		if pnl > 0.5 {
			emoji = "🟢"
		} else if pnl < -0.5 {
			emoji = "🔴"
		} else {
			emoji = "⚪"
		}

		if emoji != c.expected {
			t.Errorf("For val=%.1f cost=%.1f expected emoji %s, got %s", c.val, c.cost, c.expected, emoji)
		}
	}
}

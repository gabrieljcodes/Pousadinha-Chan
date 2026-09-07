package crypto

import (
	"math"
	"testing"
)

func TestAvailableCryptos(t *testing.T) {
	if len(AvailableCryptos) == 0 {
		t.Fatal("Expected AvailableCryptos to have items")
	}

	foundBTC := false
	foundETH := false
	foundDOGE := false
	foundPEPE := false

	for _, c := range AvailableCryptos {
		if c.Symbol == "BTC" && c.Type == "major" {
			foundBTC = true
		}
		if c.Symbol == "ETH" && c.Type == "major" {
			foundETH = true
		}
		if c.Symbol == "DOGE" && c.Type == "meme" {
			foundDOGE = true
		}
		if c.Symbol == "PEPE" && c.Type == "meme" {
			foundPEPE = true
		}
	}

	if !foundBTC || !foundETH || !foundDOGE || !foundPEPE {
		t.Errorf("Expected BTC, ETH, DOGE, and PEPE in AvailableCryptos")
	}
}

func TestGetCryptoLookups(t *testing.T) {
	btc := GetCryptoBySymbol("BTC")
	if btc == nil || btc.ID != "bitcoin" {
		t.Errorf("Expected symbol BTC to map to bitcoin, got %v", btc)
	}

	eth := GetCryptoByID("ethereum")
	if eth == nil || eth.Symbol != "ETH" {
		t.Errorf("Expected id ethereum to map to ETH, got %v", eth)
	}

	invalid := GetCryptoBySymbol("NONEXISTENT123")
	if invalid != nil {
		t.Errorf("Expected nil for invalid symbol, got %v", invalid)
	}
}

func TestCryptoPricesLiveAndCache(t *testing.T) {
	// First fetch
	prices, err := GetCryptoPrices()
	if err != nil {
		t.Fatalf("GetCryptoPrices failed: %v", err)
	}

	if len(prices) == 0 {
		t.Fatal("Expected prices map to not be empty")
	}

	btcPrice, ok := prices["bitcoin"]
	if !ok || btcPrice <= 0 {
		t.Errorf("Expected positive bitcoin price, got %f", btcPrice)
	}

	// Second fetch should use cache without hitting CoinGecko API
	cachedPrices, err := GetCryptoPrices()
	if err != nil {
		t.Fatalf("Cached GetCryptoPrices failed: %v", err)
	}

	if cachedPrices["bitcoin"] != btcPrice {
		t.Errorf("Cached price mismatch: got %f, expected %f", cachedPrices["bitcoin"], btcPrice)
	}
}

func TestGetSingleCryptoPrice(t *testing.T) {
	price, err := GetSingleCryptoPrice("bitcoin")
	if err != nil {
		t.Fatalf("GetSingleCryptoPrice failed: %v", err)
	}
	if price <= 0 {
		t.Errorf("Expected bitcoin price > 0, got %f", price)
	}
}

func TestCryptoMathAndRounding(t *testing.T) {
	price := 65432.10
	coins := 0.0054321

	rawPayout := coins * price // 355.4336
	roundedPayout := int(math.Round(rawPayout))
	truncatedPayout := int(rawPayout)

	if roundedPayout != 355 {
		t.Errorf("Expected rounded payout to be 355, got %d", roundedPayout)
	}
	if truncatedPayout != 355 {
		t.Errorf("Expected truncated payout to be 355, got %d", truncatedPayout)
	}

	// Test profit / loss
	costBasis := 300.0
	currentVal := rawPayout
	pnl := currentVal - costBasis
	pnlPct := (pnl / costBasis) * 100.0

	if pnl <= 0 || pnlPct <= 0 {
		t.Errorf("Expected positive PnL and return percentage")
	}
}

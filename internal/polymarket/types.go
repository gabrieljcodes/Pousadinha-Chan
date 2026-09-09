package polymarket

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// GammaMarket represents the JSON structure returned by the Polymarket Gamma API
type GammaMarket struct {
	ID               string `json:"id"`
	Question         string `json:"question"`
	ConditionID      string `json:"conditionId"`
	Slug             string `json:"slug"`
	ResolutionSource string `json:"resolutionSource"`
	EndDate          string `json:"endDate"`
	Liquidity        string `json:"liquidity"`
	StartDate        string `json:"startDate"`
	Image            string `json:"image"`
	Icon             string `json:"icon"`
	Description      string `json:"description"`
	Outcomes         string `json:"outcomes"`      // e.g. "[\"Yes\", \"No\"]"
	OutcomePrices    string `json:"outcomePrices"` // e.g. "[\"0.65\", \"0.35\"]"
	Volume           string  `json:"volume"`
	VolumeNum        float64 `json:"volumeNum"`
	GroupItemTitle   string  `json:"groupItemTitle"`
	Active           bool    `json:"active"`
	Closed           bool    `json:"closed"`
}

// GammaEvent represents an event container returned by the Polymarket Gamma API
type GammaEvent struct {
	ID          string         `json:"id"`
	Ticker      string         `json:"ticker"`
	Slug        string         `json:"slug"`
	Title       string         `json:"title"`
	Description string         `json:"description"`
	Image       string         `json:"image"`
	Icon        string         `json:"icon"`
	Active      bool           `json:"active"`
	Closed      bool           `json:"closed"`
	Markets     []*GammaMarket `json:"markets"`
}

// GetPrices parses the OutcomePrices JSON string array into (yesPrice, noPrice)
func (m *GammaMarket) GetPrices() (float64, float64, error) {
	if m.OutcomePrices == "" {
		return 0.50, 0.50, nil
	}

	var priceStrings []string
	if err := json.Unmarshal([]byte(m.OutcomePrices), &priceStrings); err != nil {
		return 0.50, 0.50, fmt.Errorf("failed to unmarshal outcomePrices: %w", err)
	}

	if len(priceStrings) < 2 {
		return 0.50, 0.50, fmt.Errorf("insufficient outcome prices: got %d", len(priceStrings))
	}

	yesPrice, err := strconv.ParseFloat(priceStrings[0], 64)
	if err != nil {
		yesPrice = 0.50
	}
	noPrice, err := strconv.ParseFloat(priceStrings[1], 64)
	if err != nil {
		noPrice = 0.50
	}

	// Clamp to 0.01 - 0.99 for safety
	if yesPrice < 0.01 {
		yesPrice = 0.01
	} else if yesPrice > 0.99 {
		yesPrice = 0.99
	}

	if noPrice < 0.01 {
		noPrice = 0.01
	} else if noPrice > 0.99 {
		noPrice = 0.99
	}

	return yesPrice, noPrice, nil
}

// GetWinningOutcome determines if the market resolved to "Yes", "No", or "cancelled"
func (m *GammaMarket) GetWinningOutcome() string {
	if !m.Closed {
		return ""
	}

	yes, no, err := m.GetPrices()
	if err != nil {
		return ""
	}

	// In Polymarket, winning outcome price approaches 1.0 (>= 0.90)
	if yes >= 0.90 {
		return "Yes"
	}
	if no >= 0.90 {
		return "No"
	}

	// If closed and both are equal or 0, might be cancelled / 50-50
	if math.Abs(yes-no) < 0.05 {
		return "cancelled"
	}

	return ""
}

// ParseEndDate parses the ISO endDate string to time.Time
func (m *GammaMarket) ParseEndDate() *time.Time {
	if m.EndDate == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, m.EndDate)
	if err != nil {
		// Try date format YYYY-MM-DD
		t2, err2 := time.Parse("2006-01-02", m.EndDate)
		if err2 == nil {
			return &t2
		}
		return nil
	}
	return &t
}

// FormatVolume formats the volume string into human readable USD (e.g. $13.5M)
func (m *GammaMarket) FormatVolume() string {
	vol, err := strconv.ParseFloat(m.Volume, 64)
	if err != nil || vol <= 0 {
		return "$0"
	}

	if vol >= 1_000_000 {
		return fmt.Sprintf("$%.2fM", vol/1_000_000)
	}
	if vol >= 1_000 {
		return fmt.Sprintf("$%.1fK", vol/1_000)
	}
	return fmt.Sprintf("$%.0f", vol)
}

// ShareCost represents the breakdown of buying shares
type ShareCost struct {
	Outcome         string  `json:"outcome"`
	Shares          int64   `json:"shares"`
	PricePerShare   float64 `json:"price_per_share"` // in EC (e.g. 65.00)
	RawCost         int64   `json:"raw_cost"`
	Fee             int64   `json:"fee"`
	TotalCost       int64   `json:"total_cost"`
	PotentialPayout int64   `json:"potential_payout"` // Shares * 100
	PotentialProfit int64   `json:"potential_profit"` // PotentialPayout - TotalCost
}

// ExtractSlug extracts slug or ID from user input (raw ID, slug, or full URL)
func ExtractSlug(input string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return ""
	}

	// Check if it's an API URL with ?id= or ?slug=
	if strings.Contains(input, "gamma-api.polymarket.com") {
		if u, err := url.Parse(input); err == nil {
			if id := u.Query().Get("id"); id != "" {
				return id
			}
			if slug := u.Query().Get("slug"); slug != "" {
				return slug
			}
		}
	}

	// Strip URL query parameters and hash fragments
	if idx := strings.Index(input, "?"); idx != -1 {
		input = input[:idx]
	}
	if idx := strings.Index(input, "#"); idx != -1 {
		input = input[:idx]
	}
	input = strings.TrimSuffix(input, "/")

	// If URL contains /event/ or /market/ (with optional language prefix like /pt/, /es/, /de/, etc.)
	for _, marker := range []string{"/event/", "/events/", "/market/", "/markets/"} {
		if idx := strings.Index(input, marker); idx != -1 {
			slugPart := input[idx+len(marker):]
			parts := strings.Split(slugPart, "/")
			if len(parts) > 0 && parts[0] != "" {
				return parts[0]
			}
		}
	}

	// Strip protocol and domain prefixes if any remain
	input = strings.TrimPrefix(input, "https://")
	input = strings.TrimPrefix(input, "http://")
	input = strings.TrimPrefix(input, "www.")
	input = strings.TrimPrefix(input, "polymarket.com/")

	// If path like "slug/market-id", take slug
	parts := strings.Split(input, "/")
	if len(parts) > 0 {
		return parts[0]
	}
	return input
}

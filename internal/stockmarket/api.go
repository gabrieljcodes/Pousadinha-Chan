package stockmarket

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

var (
	stockCache   = make(map[string]cachedStock)
	stockCacheMu sync.RWMutex
	cacheTTL     = 60 * time.Second

	// Ticker symbol mapping for Yahoo Finance
	tickerAliases = map[string]string{
		"BTC":  "BTC-USD",
		"GOLD": "GC=F",
	}

	httpClient = &http.Client{
		Timeout: 10 * time.Second,
	}
)

type cachedStock struct {
	data      StockResponse
	expiresAt time.Time
}

type yahooChartResponse struct {
	Chart struct {
		Result []struct {
			Meta struct {
				Currency                   string  `json:"currency"`
				Symbol                     string  `json:"symbol"`
				RegularMarketPrice         float64 `json:"regularMarketPrice"`
				ChartPreviousClose         float64 `json:"chartPreviousClose"`
				PreviousClose              float64 `json:"previousClose"`
				RegularMarketChangePercent float64 `json:"regularMarketChangePercent"`
				ShortName                  string  `json:"shortName"`
			} `json:"meta"`
		} `json:"result"`
		Error *struct {
			Code        string `json:"code"`
			Description string `json:"description"`
		} `json:"error"`
	} `json:"chart"`
}

// GetStockPrice fetches the current stock price using Yahoo Finance v8 API with caching
func GetStockPrice(ticker string) (*StockResponse, error) {
	upperTicker := strings.ToUpper(strings.TrimSpace(ticker))

	// Check cache
	stockCacheMu.RLock()
	cached, found := stockCache[upperTicker]
	stockCacheMu.RUnlock()

	if found && time.Now().Before(cached.expiresAt) {
		res := cached.data
		return &res, nil
	}

	// Resolve Yahoo symbol
	yahooSymbol := upperTicker
	if alias, ok := tickerAliases[upperTicker]; ok {
		yahooSymbol = alias
	}

	url := fmt.Sprintf("https://query1.finance.yahoo.com/v8/finance/chart/%s?interval=1d", yahooSymbol)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		if found {
			res := cached.data
			return &res, nil
		}
		return nil, err
	}

	// Yahoo Finance requires a browser-like User-Agent
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		if found {
			res := cached.data
			return &res, nil
		}
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if found {
			res := cached.data
			return &res, nil
		}
		return nil, fmt.Errorf("Yahoo Finance API returned status: %d", resp.StatusCode)
	}

	var chartResp yahooChartResponse
	if err := json.NewDecoder(resp.Body).Decode(&chartResp); err != nil {
		if found {
			res := cached.data
			return &res, nil
		}
		return nil, fmt.Errorf("failed to decode Yahoo Finance response: %w", err)
	}

	if chartResp.Chart.Error != nil {
		if found {
			res := cached.data
			return &res, nil
		}
		return nil, fmt.Errorf("Yahoo Finance error: %s - %s", chartResp.Chart.Error.Code, chartResp.Chart.Error.Description)
	}

	if len(chartResp.Chart.Result) == 0 {
		if found {
			res := cached.data
			return &res, nil
		}
		return nil, fmt.Errorf("no stock data returned for %s", ticker)
	}

	meta := chartResp.Chart.Result[0].Meta
	price := meta.RegularMarketPrice
	prevClose := meta.ChartPreviousClose
	if prevClose == 0 {
		prevClose = meta.PreviousClose
	}

	changeAmount := 0.0
	changePct := 0.0
	if prevClose > 0 {
		changeAmount = price - prevClose
		changePct = (changeAmount / prevClose) * 100.0
	} else if meta.RegularMarketChangePercent != 0 {
		changePct = meta.RegularMarketChangePercent
	}

	name := meta.ShortName
	if name == "" {
		for _, c := range Companies {
			if c.Ticker == upperTicker {
				name = c.Name
				break
			}
		}
	}

	stockData := StockResponse{
		Ticker:           upperTicker,
		Name:             name,
		Price:            price,
		ChangeAmount:     changeAmount,
		ChangePercentage: changePct,
	}

	// Update cache
	stockCacheMu.Lock()
	stockCache[upperTicker] = cachedStock{
		data:      stockData,
		expiresAt: time.Now().Add(cacheTTL),
	}
	stockCacheMu.Unlock()

	return &stockData, nil
}

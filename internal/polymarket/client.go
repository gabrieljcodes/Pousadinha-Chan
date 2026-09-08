package polymarket

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	defaultBaseURL = "https://gamma-api.polymarket.com"
	defaultTimeout = 10 * time.Second
	defaultCacheTTL = 60 * time.Second
)

type cacheEntry struct {
	data      interface{}
	expiresAt time.Time
}

type PolymarketClient struct {
	httpClient *http.Client
	baseURL    string
	cache      map[string]*cacheEntry
	cacheMu    sync.RWMutex
	cacheTTL   time.Duration
}

var (
	defaultClient *PolymarketClient
	clientOnce    sync.Once
)

// GetClient returns a singleton PolymarketClient
func GetClient() *PolymarketClient {
	clientOnce.Do(func() {
		defaultClient = NewClient(defaultBaseURL, defaultTimeout, defaultCacheTTL)
	})
	return defaultClient
}

// NewClient initializes a new PolymarketClient
func NewClient(baseURL string, timeout, cacheTTL time.Duration) *PolymarketClient {
	return &PolymarketClient{
		httpClient: &http.Client{Timeout: timeout},
		baseURL:    strings.TrimSuffix(baseURL, "/"),
		cache:      make(map[string]*cacheEntry),
		cacheTTL:   cacheTTL,
	}
}

// GetMarketByID fetches a single market by its numerical ID
func (c *PolymarketClient) GetMarketByID(id string) (*GammaMarket, error) {
	cacheKey := "market_id:" + id
	if cached, ok := c.getFromCache(cacheKey); ok {
		return cached.(*GammaMarket), nil
	}

	endpoint := fmt.Sprintf("%s/markets?id=%s", c.baseURL, url.QueryEscape(id))
	var markets []*GammaMarket
	if err := c.fetchJSON(endpoint, &markets); err != nil {
		return nil, err
	}

	if len(markets) == 0 {
		return nil, fmt.Errorf("market not found with ID: %s", id)
	}

	market := markets[0]
	c.setInCache(cacheKey, market)
	return market, nil
}

// GetMarketBySlug fetches a market by slug
func (c *PolymarketClient) GetMarketBySlug(slug string) (*GammaMarket, error) {
	cacheKey := "market_slug:" + slug
	if cached, ok := c.getFromCache(cacheKey); ok {
		return cached.(*GammaMarket), nil
	}

	// 1. Try markets?slug=
	endpoint := fmt.Sprintf("%s/markets?slug=%s", c.baseURL, url.QueryEscape(slug))
	var markets []*GammaMarket
	_ = c.fetchJSON(endpoint, &markets)

	if len(markets) > 0 {
		m := markets[0]
		c.setInCache(cacheKey, m)
		return m, nil
	}

	// 2. Try events?slug= and grab primary market
	type gammaEventResponse struct {
		ID      string         `json:"id"`
		Markets []*GammaMarket `json:"markets"`
	}
	eventEndpoint := fmt.Sprintf("%s/events?slug=%s", c.baseURL, url.QueryEscape(slug))
	var events []*gammaEventResponse
	if err := c.fetchJSON(eventEndpoint, &events); err == nil && len(events) > 0 && len(events[0].Markets) > 0 {
		m := events[0].Markets[0]
		c.setInCache(cacheKey, m)
		return m, nil
	}

	return nil, fmt.Errorf("market not found for slug: %s", slug)
}

// GetMarketBySlugOrID resolves a query (numerical ID, slug, or full URL) to a market
func (c *PolymarketClient) GetMarketBySlugOrID(query string) (*GammaMarket, error) {
	clean := ExtractSlug(query)
	if clean == "" {
		return nil, fmt.Errorf("invalid or empty market query")
	}

	// Try numeric ID first
	if market, err := c.GetMarketByID(clean); err == nil && market != nil {
		return market, nil
	}

	// Fallback to slug
	return c.GetMarketBySlug(clean)
}

// GetTrendingMarkets fetches the top active markets sorted by volume
func (c *PolymarketClient) GetTrendingMarkets(limit int) ([]*GammaMarket, error) {
	if limit <= 0 || limit > 20 {
		limit = 5
	}
	cacheKey := fmt.Sprintf("trending:%d", limit)
	if cached, ok := c.getFromCache(cacheKey); ok {
		return cached.([]*GammaMarket), nil
	}

	endpoint := fmt.Sprintf("%s/markets?limit=%d&active=true&closed=false&order=volumeNum&ascending=false", c.baseURL, limit)
	var markets []*GammaMarket
	if err := c.fetchJSON(endpoint, &markets); err != nil {
		return nil, err
	}

	c.setInCache(cacheKey, markets)
	return markets, nil
}

// SearchMarkets searches markets containing the query string
func (c *PolymarketClient) SearchMarkets(query string, limit int) ([]*GammaMarket, error) {
	if limit <= 0 || limit > 15 {
		limit = 5
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return c.GetTrendingMarkets(limit)
	}

	cacheKey := fmt.Sprintf("search:%s:%d", query, limit)
	if cached, ok := c.getFromCache(cacheKey); ok {
		return cached.([]*GammaMarket), nil
	}

	// Fetch top volume markets and filter
	endpoint := fmt.Sprintf("%s/markets?limit=50&active=true&closed=false&order=volumeNum&ascending=false", c.baseURL)
	var allMarkets []*GammaMarket
	if err := c.fetchJSON(endpoint, &allMarkets); err != nil {
		return nil, err
	}

	qLower := strings.ToLower(query)
	var matched []*GammaMarket
	for _, m := range allMarkets {
		if strings.Contains(strings.ToLower(m.Question), qLower) ||
			strings.Contains(strings.ToLower(m.Slug), qLower) ||
			strings.Contains(strings.ToLower(m.Description), qLower) {
			matched = append(matched, m)
			if len(matched) >= limit {
				break
			}
		}
	}

	c.setInCache(cacheKey, matched)
	return matched, nil
}

func (c *PolymarketClient) fetchJSON(endpoint string, target interface{}) error {
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "Pousadinha-Chan-Bot/1.0")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("api responded with status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	return json.Unmarshal(body, target)
}

func (c *PolymarketClient) getFromCache(key string) (interface{}, bool) {
	c.cacheMu.RLock()
	defer c.cacheMu.RUnlock()

	entry, found := c.cache[key]
	if !found || time.Now().After(entry.expiresAt) {
		return nil, false
	}
	return entry.data, true
}

func (c *PolymarketClient) setInCache(key string, data interface{}) {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()

	c.cache[key] = &cacheEntry{
		data:      data,
		expiresAt: time.Now().Add(c.cacheTTL),
	}
}

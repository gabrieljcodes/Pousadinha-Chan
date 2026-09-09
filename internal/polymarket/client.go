package polymarket

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
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

// GetEventBySlug fetches an event by slug from Gamma API
func (c *PolymarketClient) GetEventBySlug(slug string) (*GammaEvent, error) {
	cacheKey := "event_slug:" + slug
	if cached, ok := c.getFromCache(cacheKey); ok {
		return cached.(*GammaEvent), nil
	}

	endpoint := fmt.Sprintf("%s/events?slug=%s", c.baseURL, url.QueryEscape(slug))
	var events []*GammaEvent
	if err := c.fetchJSON(endpoint, &events); err != nil {
		return nil, err
	}

	if len(events) == 0 {
		return nil, fmt.Errorf("evento não encontrado para o slug: %s", slug)
	}

	event := events[0]
	c.setInCache(cacheKey, event)
	return event, nil
}

// NormalizeText converts string to lowercase and strips accents/diacritics
func NormalizeText(s string) string {
	s = strings.ToLower(s)
	replacements := map[rune]rune{
		'á': 'a', 'à': 'a', 'ã': 'a', 'â': 'a', 'ä': 'a',
		'é': 'e', 'è': 'e', 'ê': 'e', 'ë': 'e',
		'í': 'i', 'ì': 'i', 'î': 'i', 'ï': 'i',
		'ó': 'o', 'ò': 'o', 'õ': 'o', 'ô': 'o', 'ö': 'o',
		'ú': 'u', 'ù': 'u', 'û': 'u', 'ü': 'u',
		'ç': 'c', 'ñ': 'n',
	}
	var b strings.Builder
	for _, r := range s {
		if rep, ok := replacements[r]; ok {
			b.WriteRune(rep)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// MatchCandidate checks if a market matches a candidate query in GroupItemTitle or Question
func MatchCandidate(m *GammaMarket, filter string) bool {
	if filter == "" {
		return true
	}
	normFilter := strings.TrimSpace(NormalizeText(filter))
	if normFilter == "" {
		return true
	}

	normTitle := NormalizeText(m.GroupItemTitle)
	normQuestion := NormalizeText(m.Question)

	return strings.Contains(normTitle, normFilter) || strings.Contains(normQuestion, normFilter)
}

// SortMarketsByRelevance orders markets by active status, then highest probability (yesPrice), then volume
func SortMarketsByRelevance(markets []*GammaMarket) []*GammaMarket {
	sorted := make([]*GammaMarket, len(markets))
	copy(sorted, markets)

	sort.SliceStable(sorted, func(i, j int) bool {
		// Active before inactive
		if sorted[i].Active != sorted[j].Active {
			return sorted[i].Active
		}
		// Not closed before closed
		if sorted[i].Closed != sorted[j].Closed {
			return !sorted[i].Closed
		}

		p1, _, _ := sorted[i].GetPrices()
		p2, _, _ := sorted[j].GetPrices()

		// If probability difference is >= 1%, sort by probability
		if math.Abs(p1-p2) >= 0.01 {
			return p1 > p2
		}

		// Otherwise sort by volume
		return sorted[i].VolumeNum > sorted[j].VolumeNum
	})

	return sorted
}

// ResolveImportQuery resolves a query (numerical ID, slug, or full URL) and optional candidate filter.
// Returns:
// - (market, nil, nil) if an exact market was found or candidate matched
// - (nil, event, nil) if the event has multiple active markets and no candidate was specified
// - (nil, nil, err) if nothing was found or error occurred
func (c *PolymarketClient) ResolveImportQuery(query string, candidateFilter string) (*GammaMarket, *GammaEvent, error) {
	clean := ExtractSlug(query)
	if clean == "" {
		return nil, nil, fmt.Errorf("consulta vazia ou inválida")
	}

	// 1. Try numeric ID first
	if _, err := strconv.ParseInt(clean, 10, 64); err == nil {
		if market, err := c.GetMarketByID(clean); err == nil && market != nil {
			return market, nil, nil
		}
	}

	// 2. Try directly as market slug
	endpoint := fmt.Sprintf("%s/markets?slug=%s", c.baseURL, url.QueryEscape(clean))
	var markets []*GammaMarket
	if err := c.fetchJSON(endpoint, &markets); err == nil && len(markets) > 0 {
		return markets[0], nil, nil
	}

	// 3. Try as event slug
	event, err := c.GetEventBySlug(clean)
	if err != nil {
		return nil, nil, fmt.Errorf("mercado ou evento não encontrado para: %s", clean)
	}

	if len(event.Markets) == 0 {
		return nil, nil, fmt.Errorf("o evento '%s' não possui mercados disponíveis", event.Title)
	}

	// Filter active markets
	var activeMarkets []*GammaMarket
	for _, m := range event.Markets {
		if m.Active && !m.Closed {
			activeMarkets = append(activeMarkets, m)
		}
	}
	if len(activeMarkets) == 0 {
		activeMarkets = event.Markets
	}

	// If candidate filter provided, search for match
	if candidateFilter != "" {
		for _, m := range activeMarkets {
			if MatchCandidate(m, candidateFilter) {
				return m, nil, nil
			}
		}
		// Also search in all markets of the event
		for _, m := range event.Markets {
			if MatchCandidate(m, candidateFilter) {
				return m, nil, nil
			}
		}
		return nil, event, fmt.Errorf("candidato '%s' não encontrado no evento '%s'", candidateFilter, event.Title)
	}

	// If only 1 market exists, return it directly
	if len(activeMarkets) == 1 {
		return activeMarkets[0], nil, nil
	}

	// Multiple markets exist and no candidate specified: sort by relevance and return event
	event.Markets = SortMarketsByRelevance(activeMarkets)
	return nil, event, nil
}

// GetMarketBySlug fetches a market by slug
func (c *PolymarketClient) GetMarketBySlug(slug string) (*GammaMarket, error) {
	m, event, err := c.ResolveImportQuery(slug, "")
	if err != nil {
		return nil, err
	}
	if m != nil {
		return m, nil
	}
	if event != nil && len(event.Markets) > 0 {
		return event.Markets[0], nil
	}
	return nil, fmt.Errorf("mercado não encontrado para o slug: %s", slug)
}

// GetMarketBySlugOrID resolves a query (numerical ID, slug, or full URL) to a market
func (c *PolymarketClient) GetMarketBySlugOrID(query string) (*GammaMarket, error) {
	return c.GetMarketBySlug(query)
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

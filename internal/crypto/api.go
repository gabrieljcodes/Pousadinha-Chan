package crypto

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

const CoinGeckoBaseURL = "https://api.coingecko.com/api/v3"

var (
	cryptoCache   = make(map[string]float64)
	cryptoCacheMu sync.RWMutex
	lastFetchTime time.Time
	cacheTTL      = 60 * time.Second

	httpClient = &http.Client{
		Timeout: 10 * time.Second,
	}
)

// GetCryptoPrices fetches the current prices of all cryptocurrencies with 60s TTL caching
func GetCryptoPrices() (map[string]float64, error) {
	cryptoCacheMu.RLock()
	isFresh := time.Since(lastFetchTime) < cacheTTL && len(cryptoCache) > 0
	if isFresh {
		result := make(map[string]float64, len(cryptoCache))
		for k, v := range cryptoCache {
			result[k] = v
		}
		cryptoCacheMu.RUnlock()
		return result, nil
	}
	cryptoCacheMu.RUnlock()

	// Build ID list
	var ids []string
	for _, c := range AvailableCryptos {
		ids = append(ids, c.ID)
	}
	idList := strings.Join(ids, ",")

	url := fmt.Sprintf("%s/simple/price?ids=%s&vs_currencies=usd", CoinGeckoBaseURL, idList)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return getFallbackPrices(err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return getFallbackPrices(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return getFallbackPrices(fmt.Errorf("CoinGecko API returned status: %d", resp.StatusCode))
	}

	var data CryptoResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return getFallbackPrices(err)
	}

	// Convert to simpler map: cryptoID -> price
	prices := make(map[string]float64)
	for id, priceData := range data {
		if usdPrice, ok := priceData["usd"]; ok {
			prices[id] = usdPrice
		}
	}

	// Update cache
	cryptoCacheMu.Lock()
	for k, v := range prices {
		cryptoCache[k] = v
	}
	lastFetchTime = time.Now()
	cryptoCacheMu.Unlock()

	return prices, nil
}

func getFallbackPrices(origErr error) (map[string]float64, error) {
	cryptoCacheMu.RLock()
	defer cryptoCacheMu.RUnlock()
	if len(cryptoCache) > 0 {
		result := make(map[string]float64, len(cryptoCache))
		for k, v := range cryptoCache {
			result[k] = v
		}
		return result, nil
	}
	return nil, origErr
}

// GetSingleCryptoPrice fetches the price of a single cryptocurrency using the cache
func GetSingleCryptoPrice(cryptoID string) (float64, error) {
	prices, err := GetCryptoPrices()
	if err != nil {
		return 0, err
	}

	if price, ok := prices[cryptoID]; ok && price > 0 {
		return price, nil
	}

	// If not found in bulk, try direct request
	url := fmt.Sprintf("%s/simple/price?ids=%s&vs_currencies=usd", CoinGeckoBaseURL, cryptoID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")

	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("CoinGecko API returned status: %d", resp.StatusCode)
	}

	var data CryptoResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return 0, err
	}

	if priceData, ok := data[cryptoID]; ok {
		if usdPrice, ok := priceData["usd"]; ok {
			cryptoCacheMu.Lock()
			cryptoCache[cryptoID] = usdPrice
			cryptoCacheMu.Unlock()
			return usdPrice, nil
		}
	}

	return 0, fmt.Errorf("price not found for %s", cryptoID)
}

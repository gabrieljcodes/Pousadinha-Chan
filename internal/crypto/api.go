package crypto

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const CoinGeckoBaseURL = "https://api.coingecko.com/api/v3"

// GetCryptoPrices busca os preços atuais de todas as criptomoedas
func GetCryptoPrices() (map[string]float64, error) {
	// Construir lista de IDs
	var ids []string
	for _, c := range AvailableCryptos {
		ids = append(ids, c.ID)
	}
	idList := strings.Join(ids, ",")

	url := fmt.Sprintf("%s/simple/price?ids=%s&vs_currencies=usd", CoinGeckoBaseURL, idList)
	
	client := http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status: %d", resp.StatusCode)
	}

	var data CryptoResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	// Converter para map mais simples: cryptoID -> price
	prices := make(map[string]float64)
	for id, priceData := range data {
		if usdPrice, ok := priceData["usd"]; ok {
			prices[id] = usdPrice
		}
	}

	return prices, nil
}

// GetSingleCryptoPrice busca o preço de uma única criptomoeda
func GetSingleCryptoPrice(cryptoID string) (float64, error) {
	url := fmt.Sprintf("%s/simple/price?ids=%s&vs_currencies=usd", CoinGeckoBaseURL, cryptoID)
	
	client := http.Client{
		Timeout: 5 * time.Second,
	}

	resp, err := client.Get(url)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("API returned status: %d", resp.StatusCode)
	}

	var data CryptoResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return 0, err
	}

	if priceData, ok := data[cryptoID]; ok {
		if usdPrice, ok := priceData["usd"]; ok {
			return usdPrice, nil
		}
	}

	return 0, fmt.Errorf("price not found for %s", cryptoID)
}

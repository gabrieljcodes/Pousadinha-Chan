package stockmarket

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const BaseURL = "https://stockprices.dev/api/stocks/"

func GetStockPrice(ticker string) (*StockResponse, error) {
	client := http.Client{
		Timeout: 5 * time.Second,
	}

	resp, err := client.Get(BaseURL + ticker)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status: %d", resp.StatusCode)
	}

	var data StockResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	return &data, nil
}

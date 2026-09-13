package webhook

import (
	"bot/internal/database"
	"bot/internal/locale"
	"bot/pkg/config"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Payload struct {
	Event     string    `json:"event"`
	Content   string    `json:"content"` // Compatible with Discord Webhooks
	FromID    string    `json:"from_id,omitempty"`
	ToID      string    `json:"to_id,omitempty"`
	Amount    int       `json:"amount,omitempty"`
	Ticker    string    `json:"ticker,omitempty"`
	Symbol    string    `json:"symbol,omitempty"`
	Shares    float64   `json:"shares,omitempty"`
	Coins     float64   `json:"coins,omitempty"`
	Price     float64   `json:"price,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// ValidateWebhookURL validates that the URL is a valid public HTTP/HTTPS endpoint and protects against SSRF
func ValidateWebhookURL(rawURL string) error {
	rawURL = strings.TrimSpace(rawURL)
	if len(rawURL) > 2048 {
		return errors.New(locale.Text("webhook.sender.url_exceeds_maximum_allowed_length_of_characters"))
	}
	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil {
		return fmt.Errorf(locale.Text("webhook.sender.invalid_url_format"), err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New(locale.Text("webhook.sender.url_scheme_must_be_http_or_https"))
	}

	host := parsed.Hostname()
	if host == "" {
		return errors.New(locale.Text("webhook.sender.url_hostname_cannot_be_empty"))
	}

	// Verify hostname does not resolve to private/loopback/cloud-metadata IPs
	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf(locale.Text("webhook.sender.could_not_resolve_hostname"), err)
	}
	if len(ips) == 0 {
		return errors.New(locale.Text("webhook.sender.no_ip_address_found_for_host"))
	}

	for _, ip := range ips {
		if isDisallowedIP(ip) {
			return errors.New(locale.Text("webhook.sender.webhook_url_targets_an_unauthorized_local_or"))
		}
	}
	return nil
}

var allowLocalIPsForTesting = false

func isDisallowedIP(ip net.IP) bool {
	if allowLocalIPsForTesting {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return true
	}
	// AWS / GCP / Azure metadata endpoint
	if ip.Equal(net.ParseIP("169.254.169.254")) {
		return true
	}
	return false
}

func dispatchWebhook(targetURL string, p Payload) {
	if targetURL == "" {
		return
	}
	go func(u string, payload Payload) {
		jsonBytes, err := json.Marshal(payload)
		if err != nil {
			return
		}

		client := http.Client{
			Timeout: 5 * time.Second,
		}

		resp, err := client.Post(u, "application/json", bytes.NewBuffer(jsonBytes))
		if err != nil {
			log.Printf("[WEBHOOK ERROR] Failed to send webhook: %v", err)
			return
		}
		defer resp.Body.Close()
	}(targetURL, p)
}

// SendTransferNotification sends a webhook notification when coins are received
func SendTransferNotification(fromID, toID string, amount int) {
	url, err := database.GetWebhook(toID)
	if err != nil || url == "" {
		return
	}

	content := locale.Text("webhook.sender.transfer_received_you_received_from.formatted", locale.Data{"Amount": amount, "CurrencyName": config.Bot.CurrencyName, "FromID": fromID})
	payload := Payload{
		Event:     "transfer_received",
		Content:   content,
		FromID:    fromID,
		ToID:      toID,
		Amount:    amount,
		Timestamp: time.Now(),
	}

	dispatchWebhook(url, payload)
}

// SendStockNotification sends a webhook notification for stock trading
func SendStockNotification(userID string, isBuy bool, ticker string, shares float64, amount int, price float64) {
	url, err := database.GetWebhook(userID)
	if err != nil || url == "" {
		return
	}

	var event, content string
	if isBuy {
		event = "stock_buy"
		content = locale.Text("webhook.sender.stock_purchase_you_bought_shares_of_for.formatted", locale.Data{"Shares": shares, "Ticker": ticker, "Amount": amount, "CurrencyName": config.Bot.CurrencyName, "Price": price})
	} else {
		event = "stock_sell"
		content = locale.Text("webhook.sender.stock_sale_you_sold_shares_of_for.formatted", locale.Data{"Shares": shares, "Ticker": ticker, "Amount": amount, "CurrencyName": config.Bot.CurrencyName, "Price": price})
	}

	payload := Payload{
		Event:     event,
		Content:   content,
		Ticker:    ticker,
		Shares:    shares,
		Amount:    amount,
		Price:     price,
		Timestamp: time.Now(),
	}

	dispatchWebhook(url, payload)
}

// SendCryptoNotification sends a webhook notification for crypto trading
func SendCryptoNotification(userID string, isBuy bool, symbol string, coins float64, amount int, price float64) {
	url, err := database.GetWebhook(userID)
	if err != nil || url == "" {
		return
	}

	var event, content string
	if isBuy {
		event = "crypto_buy"
		content = locale.Text("webhook.sender.crypto_purchase_you_bought_for_at_coin.formatted", locale.Data{"Coins": coins, "Symbol": symbol, "Amount": amount, "CurrencyName": config.Bot.CurrencyName, "Price": price})
	} else {
		event = "crypto_sell"
		content = locale.Text("webhook.sender.crypto_sale_you_sold_for_at_coin.formatted", locale.Data{"Coins": coins, "Symbol": symbol, "Amount": amount, "CurrencyName": config.Bot.CurrencyName, "Price": price})
	}

	payload := Payload{
		Event:     event,
		Content:   content,
		Symbol:    symbol,
		Coins:     coins,
		Amount:    amount,
		Price:     price,
		Timestamp: time.Now(),
	}

	dispatchWebhook(url, payload)
}

// SendGenericNotification sends a text message notification
func SendGenericNotification(userID string, message string) {
	url, err := database.GetWebhook(userID)
	if err != nil || url == "" {
		return
	}

	payload := Payload{
		Event:     "notification",
		Content:   message,
		Timestamp: time.Now(),
	}

	dispatchWebhook(url, payload)
}

// TestWebhook sends a test payload and verifies the remote endpoint responds with a successful status
func TestWebhook(targetURL string) error {
	if err := ValidateWebhookURL(targetURL); err != nil {
		return err
	}

	payload := Payload{
		Event:     "test",
		Content:   locale.Text("webhook.sender.test_notification_from_pousadinha_chan_your_webhook"),
		Timestamp: time.Now(),
	}

	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	client := http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(targetURL, "application/json", bytes.NewBuffer(jsonBytes))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf(locale.Text("webhook.sender.remote_endpoint_returned_status"), resp.StatusCode, http.StatusText(resp.StatusCode))
	}
	return nil
}

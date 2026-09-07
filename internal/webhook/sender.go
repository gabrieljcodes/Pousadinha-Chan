package webhook

import (
	"bytes"
	"encoding/json"
	"estudocoin/internal/database"
	"estudocoin/pkg/config"
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
		return fmt.Errorf("URL exceeds maximum allowed length of 2048 characters")
	}
	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL format: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("URL scheme must be http or https")
	}

	host := parsed.Hostname()
	if host == "" {
		return fmt.Errorf("URL hostname cannot be empty")
	}

	// Verify hostname does not resolve to private/loopback/cloud-metadata IPs
	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("could not resolve hostname: %w", err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("no IP address found for host")
	}

	for _, ip := range ips {
		if isDisallowedIP(ip) {
			return fmt.Errorf("webhook URL targets an unauthorized local or private IP address")
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

	content := fmt.Sprintf("💰 **Transfer Received!** You received **%d %s** from <@%s>.", amount, config.Bot.CurrencyName, fromID)
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
		content = fmt.Sprintf("📈 **Stock Purchase**\nYou bought **%.4f** shares of **%s** for **%d %s** (at $%.2f/share).",
			shares, ticker, amount, config.Bot.CurrencyName, price)
	} else {
		event = "stock_sell"
		content = fmt.Sprintf("📉 **Stock Sale**\nYou sold **%.4f** shares of **%s** for **%d %s** (at $%.2f/share).",
			shares, ticker, amount, config.Bot.CurrencyName, price)
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
		content = fmt.Sprintf("🪙 **Crypto Purchase**\nYou bought **%.8f %s** for **%d %s** (at $%.6f/coin).",
			coins, symbol, amount, config.Bot.CurrencyName, price)
	} else {
		event = "crypto_sell"
		content = fmt.Sprintf("💰 **Crypto Sale**\nYou sold **%.8f %s** for **%d %s** (at $%.6f/coin).",
			coins, symbol, amount, config.Bot.CurrencyName, price)
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
		Content:   "🔔 **Test Notification** from Pousadinha-Chan! Your webhook is working properly.",
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
		return fmt.Errorf("remote endpoint returned status %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	}
	return nil
}

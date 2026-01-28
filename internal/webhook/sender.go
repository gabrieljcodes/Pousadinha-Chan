package webhook

import (
	"bytes"
	"encoding/json"
	"estudocoin/internal/database"
	"log"
	"net/http"
	"time"
)

type Payload struct {
	Event     string    `json:"event"`
	FromID    string    `json:"from_id"`
	ToID      string    `json:"to_id"`
	Amount    int       `json:"amount"`
	Timestamp time.Time `json:"timestamp"`
}

func SendTransferNotification(fromID, toID string, amount int) {
	// Look up webhook URL for the recipient
	url, err := database.GetWebhook(toID)
	if err != nil || url == "" {
		return // No webhook configured
	}

	payload := Payload{
		Event:     "transfer_received",
		FromID:    fromID,
		ToID:      toID,
		Amount:    amount,
		Timestamp: time.Now(),
	}

	// Send asynchronously
	go func(targetURL string, p Payload) {
		jsonBytes, _ := json.Marshal(p)
		
		client := http.Client{
			Timeout: 5 * time.Second,
		}

		resp, err := client.Post(targetURL, "application/json", bytes.NewBuffer(jsonBytes))
		if err != nil {
			log.Printf("Failed to trigger webhook for user %s: %v", toID, err)
			return
		}
		defer resp.Body.Close()
	}(url, payload)
}

func TestWebhook(url string) error {
	payload := Payload{
		Event:     "test",
		Timestamp: time.Now(),
	}
	jsonBytes, _ := json.Marshal(payload)
	client := http.Client{Timeout: 5 * time.Second}
	_, err := client.Post(url, "application/json", bytes.NewBuffer(jsonBytes))
	return err
}

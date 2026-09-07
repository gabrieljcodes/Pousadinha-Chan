package webhook

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestValidateWebhookURL_SSRF(t *testing.T) {
	blockedURLs := []string{
		"http://127.0.0.1:8080/hook",
		"http://127.0.0.2:9000",
		"http://localhost:8080",
		"http://localhost",
		"http://10.0.0.1/hook",
		"http://10.255.255.255/hook",
		"http://172.16.0.1/hook",
		"http://172.31.255.255/hook",
		"http://192.168.1.1/hook",
		"http://169.254.169.254/latest/meta-data/",
		"http://0.0.0.0:8080",
		"ftp://example.com/hook",
		"file:///etc/passwd",
		"not-a-url",
		"",
	}

	for _, u := range blockedURLs {
		err := ValidateWebhookURL(u)
		if err == nil {
			t.Errorf("Expected URL to be blocked by SSRF/validation, but passed: %s", u)
		}
	}
}

func TestValidateWebhookURL_Valid(t *testing.T) {
	// Public well-known domains that resolve to public IPs
	validURLs := []string{
		"https://discord.com/api/webhooks/123456/abcdef",
		"https://api.github.com/webhook",
	}

	for _, u := range validURLs {
		err := ValidateWebhookURL(u)
		if err != nil {
			t.Errorf("Expected valid URL %s to pass, got error: %v", u, err)
		}
	}
}

func TestPayloadDiscordCompatibility(t *testing.T) {
	p := Payload{
		Event:     "test",
		Content:   "This is a discord compatible test notification",
		Amount:    100,
		Timestamp: time.Now(),
	}

	bytes, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("Failed to marshal payload: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(bytes, &m); err != nil {
		t.Fatalf("Failed to unmarshal payload json: %v", err)
	}

	if content, ok := m["content"].(string); !ok || content == "" {
		t.Errorf("Expected content field to be present and non-empty for Discord compatibility, got %v", m["content"])
	}
	if event, ok := m["event"].(string); !ok || event != "test" {
		t.Errorf("Expected event to be 'test', got %v", m["event"])
	}
}

func TestTestWebhook(t *testing.T) {
	allowLocalIPsForTesting = true
	defer func() { allowLocalIPsForTesting = false }()

	// Success server
	successServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer successServer.Close()

	if err := TestWebhook(successServer.URL); err != nil {
		t.Errorf("Expected TestWebhook to succeed for 200 OK, got: %v", err)
	}

	// Failure server (400 Bad Request)
	failServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"cannot send empty message"}`))
	}))
	defer failServer.Close()

	err := TestWebhook(failServer.URL)
	if err == nil {
		t.Errorf("Expected TestWebhook to fail on 400 Bad Request, but got nil")
	} else if !strings.Contains(err.Error(), "status 400") {
		t.Errorf("Expected error to mention status 400, got: %v", err)
	}
}

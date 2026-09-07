package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCORSMiddleware_Options(t *testing.T) {
	handlerCalled := false
	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
	})

	cors := CORSMiddleware(dummyHandler)

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/me", nil)
	rec := httptest.NewRecorder()

	cors.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("Expected 204 No Content for OPTIONS, got %d", rec.Code)
	}

	if handlerCalled {
		t.Errorf("Downstream handler should not be called on OPTIONS preflight")
	}

	if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("Expected Access-Control-Allow-Origin: *")
	}

	if rec.Header().Get("Access-Control-Allow-Methods") == "" {
		t.Errorf("Expected Access-Control-Allow-Methods to be set")
	}

	if rec.Header().Get("Access-Control-Allow-Headers") == "" {
		t.Errorf("Expected Access-Control-Allow-Headers to be set")
	}
}

func TestCORSMiddleware_PassThrough(t *testing.T) {
	handlerCalled := false
	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	})

	cors := CORSMiddleware(dummyHandler)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	rec := httptest.NewRecorder()

	cors.ServeHTTP(rec, req)

	if !handlerCalled {
		t.Errorf("Expected downstream handler to be called for GET request")
	}

	if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("Expected Access-Control-Allow-Origin: *")
	}
}

func TestWriteJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	payload := map[string]string{"message": "success"}

	writeJSON(rec, http.StatusCreated, payload)

	if rec.Code != http.StatusCreated {
		t.Errorf("Expected status 201, got %d", rec.Code)
	}

	contentType := rec.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected Content-Type application/json, got %s", contentType)
	}

	var result map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode JSON: %v", err)
	}

	if result["message"] != "success" {
		t.Errorf("Expected message 'success', got '%s'", result["message"])
	}
}

func TestAuthMiddleware_MissingKey(t *testing.T) {
	dummyHandler := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}

	auth := AuthMiddleware(dummyHandler)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	rec := httptest.NewRecorder()

	auth(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", rec.Code)
	}

	if rec.Header().Get("Content-Type") != "application/json" {
		t.Errorf("Expected Content-Type application/json")
	}

	var errResp ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&errResp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if errResp.Error != "Missing API Key" {
		t.Errorf("Expected 'Missing API Key', got '%s'", errResp.Error)
	}
}

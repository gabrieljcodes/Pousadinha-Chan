package database

import "testing"

func TestBotMetadataNilDB(t *testing.T) {
	// With empty DB or empty key
	val, err := GetBotMetadata("")
	if err != nil || val != "" {
		t.Fatalf("expected empty result without error for empty key, got val=%q, err=%v", val, err)
	}
}

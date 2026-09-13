package database

import (
	"database/sql"
	"fmt"
)

// GetBotMetadata retrieves a key from bot_metadata table.
// Returns empty string if not found.
func GetBotMetadata(key string) (string, error) {
	if DB == nil || key == "" {
		return "", nil
	}
	query := `SELECT value FROM bot_metadata WHERE key = $1`
	var val string
	err := DB.QueryRow(query, key).Scan(&val)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("query bot_metadata: %w", err)
	}
	return val, nil
}

// SetBotMetadata inserts or updates a key in bot_metadata table.
func SetBotMetadata(key, value string) error {
	if DB == nil {
		return fmt.Errorf("database not initialized")
	}
	query := `
		INSERT INTO bot_metadata (key, value, updated_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (key) DO UPDATE SET
			value = EXCLUDED.value,
			updated_at = NOW()
	`
	if _, err := DB.Exec(query, key, value); err != nil {
		return fmt.Errorf("set bot_metadata: %w", err)
	}
	return nil
}

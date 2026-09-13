package database

import (
	"database/sql"
	"fmt"
	"sync"
	"time"
)

type DBGuildSettings struct {
	GuildID   string    `json:"guild_id"`
	Language  string    `json:"language"`
	UpdatedAt time.Time `json:"updated_at"`
}

var (
	guildLangCache   = make(map[string]string)
	guildLangCacheMu sync.RWMutex
)

// GetGuildLanguageCached retrieves the language setting for a guild from cache or database.
// Returns "auto" if not set or on error.
func GetGuildLanguageCached(guildID string) string {
	if guildID == "" {
		return "auto"
	}
	guildLangCacheMu.RLock()
	if lang, ok := guildLangCache[guildID]; ok {
		guildLangCacheMu.RUnlock()
		return lang
	}
	guildLangCacheMu.RUnlock()

	lang, err := GetGuildLanguage(guildID)
	if err != nil || lang == "" {
		lang = "auto"
	}

	guildLangCacheMu.Lock()
	guildLangCache[guildID] = lang
	guildLangCacheMu.Unlock()

	return lang
}

// GetGuildLanguage retrieves the language configured for the guild from the database.
func GetGuildLanguage(guildID string) (string, error) {
	if DB == nil || guildID == "" {
		return "auto", nil
	}
	query := `SELECT language FROM guild_settings WHERE guild_id = $1`
	var lang string
	err := DB.QueryRow(query, guildID).Scan(&lang)
	if err == sql.ErrNoRows {
		return "auto", nil
	}
	if err != nil {
		return "auto", fmt.Errorf("query guild_settings: %w", err)
	}
	return lang, nil
}

// SetGuildLanguage updates or inserts the guild's language setting.
func SetGuildLanguage(guildID, language string) error {
	if DB == nil {
		return fmt.Errorf("database not initialized")
	}
	if language == "" {
		language = "auto"
	}
	query := `
		INSERT INTO guild_settings (guild_id, language, updated_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (guild_id) DO UPDATE SET
			language = EXCLUDED.language,
			updated_at = NOW()
	`
	if _, err := DB.Exec(query, guildID, language); err != nil {
		return fmt.Errorf("set guild language: %w", err)
	}

	guildLangCacheMu.Lock()
	guildLangCache[guildID] = language
	guildLangCacheMu.Unlock()

	return nil
}

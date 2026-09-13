package database

import (
	"testing"
)

func TestGuildLanguageCache(t *testing.T) {
	// Empty guild ID returns "auto"
	if lang := GetGuildLanguageCached(""); lang != "auto" {
		t.Fatalf("expected 'auto' for empty guild ID, got %q", lang)
	}

	// Manually populate cache to verify caching logic without DB dependency
	guildLangCacheMu.Lock()
	guildLangCache["test_guild_1"] = "pt-BR"
	guildLangCacheMu.Unlock()

	if lang := GetGuildLanguageCached("test_guild_1"); lang != "pt-BR" {
		t.Fatalf("expected 'pt-BR' from cache, got %q", lang)
	}

	guildLangCacheMu.Lock()
	delete(guildLangCache, "test_guild_1")
	guildLangCacheMu.Unlock()
}

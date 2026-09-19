package gacha

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestCustomImageScheduleDefaults(t *testing.T) {
	store := &Store{}
	def := store.DefaultSchedule()
	if !def.CustomImageAutoApprove {
		t.Fatalf("expected DefaultSchedule CustomImageAutoApprove to be true")
	}
}

func TestCustomImageIntegrationIfConfigured(t *testing.T) {
	dsn := os.Getenv("GACHA_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("skipping integration test because GACHA_TEST_DATABASE_URL is not set")
	}

	if strings.Contains(dsn, "?") {
		dsn += "&statement_cache_capacity=0&default_query_exec_mode=exec"
	} else {
		dsn += "?statement_cache_capacity=0&default_query_exec_mode=exec"
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("failed connecting to test db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	store := &Store{DB: db}
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	guildID := "guild-custom-test"
	userID := "user-custom-1"
	otherUser := "user-custom-2"
	charID := int64(99901)

	// Clean up potential leftover test data
	_, _ = db.Exec(`DELETE FROM gacha_guild_character_images WHERE guild_id=$1`, guildID)
	_, _ = db.Exec(`DELETE FROM gacha_assets WHERE submitted_by IN ($1, $2)`, userID, otherUser)
	_, _ = db.Exec(`DELETE FROM gacha_characters WHERE id=$1`, charID)

	// Seed test character
	_, err = db.Exec(`
		INSERT INTO gacha_characters(id, name, favourites, enabled)
		VALUES($1, 'Custom Test Char', 100, true)
		ON CONFLICT(id) DO NOTHING
	`, charID)
	if err != nil {
		t.Fatalf("failed to insert test character: %v", err)
	}

	// Test 1: Auto-approve guild schedule setting toggle
	if err := store.SetGuildAutoApprove(ctx, guildID, false); err != nil {
		t.Fatalf("failed to set auto-approve to false: %v", err)
	}
	sch := store.GuildSchedule(ctx, guildID)
	if sch.CustomImageAutoApprove {
		t.Fatalf("expected auto-approve to be false")
	}

	if err := store.SetGuildAutoApprove(ctx, guildID, true); err != nil {
		t.Fatalf("failed to set auto-approve to true: %v", err)
	}
	sch = store.GuildSchedule(ctx, guildID)
	if !sch.CustomImageAutoApprove {
		t.Fatalf("expected auto-approve to be true")
	}

	// Test 2: User quota count
	count, err := store.CountUserCustomImages(ctx, userID)
	if err != nil {
		t.Fatalf("failed to count custom images: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 custom images initially, got %d", count)
	}

	// Seed 20 dummy assets for userID
	for i := 1; i <= 20; i++ {
		_, err := db.Exec(`
			INSERT INTO gacha_assets(
				character_id, provider, external_id, source_url, sha256, path, media_type, status, submitted_by, guild_id
			) VALUES (
				$1, 'user_custom', $2, 'https://example.com', $3, $4, 'image/png', 'approved', $5, $6
			)
		`, charID, "test-ext-"+string(rune(i)), "sha-"+string(rune(i)), "path-"+string(rune(i))+".png", userID, guildID)
		if err != nil {
			t.Fatalf("failed seeding dummy asset %d: %v", i, err)
		}
	}

	count, err = store.CountUserCustomImages(ctx, userID)
	if err != nil {
		t.Fatalf("failed counting seeded assets: %v", err)
	}
	if count != 20 {
		t.Fatalf("expected 20 custom images, got %d", count)
	}

	// Test 3: Submitting 21st image should fail with quota exceeded
	dummyReader := strings.NewReader("dummy content")
	_, err = store.SubmitCustomImage(ctx, guildID, userID, "TestUser#0001", charID, "", dummyReader, true)
	if err == nil {
		t.Fatalf("expected quota exceeded error when submitting 21st image")
	}
	if !strings.Contains(err.Error(), "limit of 20") {
		t.Fatalf("expected quota exceeded message, got: %v", err)
	}

	// Test 4: List custom images
	list, err := store.ListUserCustomImages(ctx, userID)
	if err != nil {
		t.Fatalf("failed listing custom images: %v", err)
	}
	if len(list) != 20 {
		t.Fatalf("expected list of 20 items, got %d", len(list))
	}

	// Test 5: Remove custom image frees quota
	toRemove := list[0].ID
	if err := store.RemoveCustomImage(ctx, userID, toRemove, false); err != nil {
		t.Fatalf("failed to remove custom image %d: %v", toRemove, err)
	}

	count, err = store.CountUserCustomImages(ctx, userID)
	if err != nil {
		t.Fatalf("failed to count after removal: %v", err)
	}
	if count != 19 {
		t.Fatalf("expected 19 custom images after removal, got %d", count)
	}

	// Test 6: Setting active character image
	remainingAssetID := list[1].ID
	// Make sure remaining asset is approved
	_, _ = db.Exec(`UPDATE gacha_assets SET status='approved' WHERE id=$1`, remainingAssetID)

	// User sets active image
	updatedCard, err := store.SetCharacterActiveImage(ctx, guildID, userID, charID, 1, false)
	if err != nil {
		t.Fatalf("failed setting active character image: %v", err)
	}
	if updatedCard == nil || updatedCard.ID != charID {
		t.Fatalf("expected card for charID %d, got %v", charID, updatedCard)
	}

	// Claim character by otherUser
	_, err = db.Exec(`INSERT INTO users(id, balance) VALUES($1, 100) ON CONFLICT DO NOTHING`, otherUser)
	if err != nil {
		t.Fatalf("failed inserting otherUser: %v", err)
	}
	_, err = db.Exec(`INSERT INTO gacha_collection(guild_id, user_id, character_id) VALUES($1, $2, $3) ON CONFLICT(guild_id, character_id) DO UPDATE SET user_id=EXCLUDED.user_id`, guildID, otherUser, charID)
	if err != nil {
		t.Fatalf("failed claiming character: %v", err)
	}

	// Now userID (non-owner, non-admin) should be denied changing active image
	_, err = store.SetCharacterActiveImage(ctx, guildID, userID, charID, 1, false)
	if err == nil {
		t.Fatalf("expected error when non-owner tries to change active image of claimed character")
	}

	// But otherUser (owner) can change it
	_, err = store.SetCharacterActiveImage(ctx, guildID, otherUser, charID, 1, false)
	if err != nil {
		t.Fatalf("expected owner to succeed setting active image: %v", err)
	}

	// And admin can change it
	_, err = store.SetCharacterActiveImage(ctx, guildID, userID, charID, 1, true)
	if err != nil {
		t.Fatalf("expected admin to succeed setting active image: %v", err)
	}
}

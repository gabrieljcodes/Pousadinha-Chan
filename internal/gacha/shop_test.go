package gacha

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestShopCatalog(t *testing.T) {
	store := &Store{}
	catalog := store.GetShopCatalog()
	if len(catalog) != 7 {
		t.Fatalf("expected 7 shop items, got %d", len(catalog))
	}

	tome, ok := store.FindShopItem(ItemArcaneTome)
	if !ok {
		t.Fatalf("expected Arcane Tome in catalog")
	}
	if tome.Price != 5000 {
		t.Fatalf("expected Arcane Tome price to be 5000, got %d", tome.Price)
	}

	shield, ok := store.FindShopItem(ItemSnipeShield)
	if !ok {
		t.Fatalf("expected Snipe Shield in catalog")
	}
	if shield.Price != 10000 {
		t.Fatalf("expected Snipe Shield price to be 10000 (10k), got %d", shield.Price)
	}
	if !shield.Consumable {
		t.Fatalf("expected Snipe Shield to be consumable")
	}

	// Verify all items have non-empty localized names and descriptions without panicking
	for _, it := range catalog {
		if it.Name() == "" {
			t.Fatalf("expected item %s to have a localized name", it.ID)
		}
		if it.Description() == "" {
			t.Fatalf("expected item %s to have a localized description", it.ID)
		}
		if it.Price <= 0 {
			t.Fatalf("expected item %s to have a positive price, got %d", it.ID, it.Price)
		}
	}
}

func TestSnipeShieldExpiryCalculation(t *testing.T) {
	schedule := ResetSchedule{
		ResetMinute:  0,
		RollsPerHour: 10,
		ClaimHours:   3,
	}

	now := time.Date(2026, 9, 16, 5, 20, 0, 0, time.UTC)
	claimWin := schedule.ClaimWindow(now)
	// Current claim window: 03:00 to 06:00 UTC. NextReset = 06:00 UTC.
	expectedFirstReset := time.Date(2026, 9, 16, 6, 0, 0, 0, time.UTC)
	if !claimWin.NextReset.Equal(expectedFirstReset) {
		t.Fatalf("expected first reset at %v, got %v", expectedFirstReset, claimWin.NextReset)
	}

	claimInterval := time.Duration(schedule.ClaimHours) * time.Hour
	// 3 resets = claimWin.NextReset + 2 * claimInterval (covers 1st reset at 06:00, 2nd at 09:00, 3rd at 12:00)
	shieldExpiry := claimWin.NextReset.Add(2 * claimInterval)
	expectedExpiry := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	if !shieldExpiry.Equal(expectedExpiry) {
		t.Fatalf("expected 3 resets expiry at %v, got %v", expectedExpiry, shieldExpiry)
	}
}

func TestShopPrefixParsing(t *testing.T) {
	tests := []struct {
		input       string
		expectMatch bool
		expectedCmd string
		expectedArg string
		expectedQty int
	}{
		{"!shop", true, "shop", "", 0},
		{"!store", true, "shop", "", 0},
		{"!inv", true, "inventory", "", 0},
		{"!inventory", true, "inventory", "", 0},
		{"!bag", true, "inventory", "", 0},
		{"!buy lootbox", true, "buy", "lootbox", 1},
		{"!buy lootbox 5", true, "buy", "lootbox", 5},
		{"!buy snipe_shield", true, "buy", "snipe_shield", 1},
		{"!use claim_reset", true, "use", "claim_reset", 0},
		{"!use roll_reset", true, "use", "roll_reset", 0},
		{"!rr", true, "use", "roll_reset", 0},
		{"!rc", true, "use", "claim_reset", 0},
		{"!rt", true, "use", "claim_reset", 0},
		{"!shield", true, "use", "snipe_shield", 0},
		{"!battery", true, "use", "gem_battery", 0},
		{"!flare", true, "use", "wish_flare", 0},
		{"!open", true, "open", "lootbox", 1},
		{"!open 3", true, "open", "lootbox", 3},
		{"!chest", true, "open", "lootbox", 1},
	}

	for _, tt := range tests {
		cmd, ok := parsePrefixCommand(tt.input)
		if ok != tt.expectMatch {
			t.Fatalf("input %q: expected match=%v, got %v", tt.input, tt.expectMatch, ok)
		}
		if ok {
			if cmd.SubCmd != tt.expectedCmd {
				t.Fatalf("input %q: expected subCmd=%s, got %s", tt.input, tt.expectedCmd, cmd.SubCmd)
			}
			if tt.expectedArg != "" && cmd.Arg1 != tt.expectedArg {
				t.Fatalf("input %q: expected arg1=%s, got %s", tt.input, tt.expectedArg, cmd.Arg1)
			}
			if tt.expectedQty > 0 && cmd.Arg2 != tt.expectedQty {
				t.Fatalf("input %q: expected arg2=%d, got %d", tt.input, tt.expectedQty, cmd.Arg2)
			}
		}
	}
}

func TestNormalizeShopAction(t *testing.T) {
	if normalizeAction("store") != "shop" {
		t.Fatalf("expected store -> shop")
	}
	if normalizeAction("inv") != "inventory" {
		t.Fatalf("expected inv -> inventory")
	}
	if normalizeAction("bag") != "inventory" {
		t.Fatalf("expected bag -> inventory")
	}
	if normalizeAction("chest") != "open" {
		t.Fatalf("expected chest -> open")
	}
}

func TestShopIntegrationIfConfigured(t *testing.T) {
	dsn := os.Getenv("GACHA_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("skipping integration test because GACHA_TEST_DATABASE_URL is not set")
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

	guildID := "guild-shop-test"
	rollerID := "user-roller-1"
	sniperID := "user-sniper-2"

	for _, u := range []string{rollerID, sniperID} {
		_, _ = db.Exec(`INSERT INTO users(id, balance) VALUES($1, 50000) ON CONFLICT(id) DO UPDATE SET balance=50000`, u)
		_, _ = db.Exec(`INSERT INTO guild_members(guild_id, user_id, balance) VALUES($1, $2, 50000) ON CONFLICT(guild_id, user_id) DO UPDATE SET balance=50000`, guildID, u)
		_, _ = db.Exec(`INSERT INTO gacha_players(guild_id, user_id) VALUES($1, $2) ON CONFLICT DO NOTHING`, guildID, u)
	}

	// 1. Buy Snipe Shield for 10,000 coins
	buyRes, err := store.BuyItem(ctx, guildID, rollerID, ItemSnipeShield, 1)
	if err != nil {
		t.Fatalf("failed buying Snipe Shield: %v", err)
	}
	if buyRes.TotalCost != 10000 {
		t.Fatalf("expected cost 10000, got %d", buyRes.TotalCost)
	}
	if buyRes.NewBalance != 40000 {
		t.Fatalf("expected balance 40000, got %d", buyRes.NewBalance)
	}

	// 2. Use Snipe Shield
	useRes, err := store.UseItem(ctx, guildID, rollerID, ItemSnipeShield)
	if err != nil {
		t.Fatalf("failed using Snipe Shield: %v", err)
	}
	if useRes.Message == "" {
		t.Fatalf("expected non-empty activation message")
	}

	var shieldUntil time.Time
	err = db.QueryRowContext(ctx, `SELECT snipe_shield_until FROM gacha_players WHERE guild_id=$1 AND user_id=$2`, guildID, rollerID).Scan(&shieldUntil)
	if err != nil || !shieldUntil.After(time.Now()) {
		t.Fatalf("expected snipe_shield_until to be active in future, got %v (err: %v)", shieldUntil, err)
	}

	// 3. Create a wished roll
	charID := int64(999902)
	_, _ = db.Exec(`INSERT INTO gacha_characters(id, name, favourites, enabled) VALUES($1, 'Wish Target Char', 10, true) ON CONFLICT(id) DO NOTHING`, charID)
	rollID := "roll-wish-shield-1"
	_, err = db.Exec(`
		INSERT INTO gacha_rolls(id, guild_id, channel_id, user_id, character_id, expires_at, request_id, wish_spawn, created_at)
		VALUES($1, $2, $3, $4, $5, now() + interval '45 seconds', $6, true, now())
	`, rollID, guildID, "channel-shop-test", rollerID, charID, "req-shop-1")
	if err != nil {
		t.Fatalf("failed inserting roll: %v", err)
	}

	// Ensure sniper can claim
	_, _ = db.Exec(`UPDATE gacha_players SET claim_after = now() - interval '1 hour' WHERE guild_id=$1 AND user_id=$2`, guildID, sniperID)
	_, _ = db.Exec(`UPDATE gacha_players SET claim_after = now() - interval '1 hour' WHERE guild_id=$1 AND user_id=$2`, guildID, rollerID)

	// 4. Sniper attempts to claim within 15 seconds -> must be rejected by Snipe Shield
	err = store.Claim(ctx, guildID, "channel-shop-test", sniperID, rollID)
	if err == nil {
		t.Fatalf("expected sniper claim to be blocked by Snipe Shield, but claim succeeded")
	}
	var uErr userError
	if !errors.As(err, &uErr) {
		t.Fatalf("expected userError from Snipe Shield protection, got %T: %v", err, err)
	}

	// 5. Roller attempts to claim within 15 seconds -> must succeed!
	err = store.Claim(ctx, guildID, "channel-shop-test", rollerID, rollID)
	if err != nil {
		t.Fatalf("expected roller claim to succeed with Snipe Shield, got: %v", err)
	}
}

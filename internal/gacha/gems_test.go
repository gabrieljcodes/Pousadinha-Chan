package gacha

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestRollGem_KeyGuarantee(t *testing.T) {
	for i := 0; i < 100; i++ {
		gem, spawned := RollGem(true)
		if !spawned {
			t.Fatalf("expected key roll to always guarantee gem spawn on iteration %d", i)
		}
		if gem.Value <= 0 {
			t.Fatalf("expected positive gem value, got %d", gem.Value)
		}
	}
}

func TestRollGem_NonKeyDistribution(t *testing.T) {
	total := 1000
	spawnedCount := 0
	for i := 0; i < total; i++ {
		if _, spawned := RollGem(false); spawned {
			spawnedCount++
		}
	}
	// Expected ~40% (between 30% and 50% for 1000 trials)
	rate := float64(spawnedCount) / float64(total)
	if rate < 0.30 || rate > 0.50 {
		t.Fatalf("expected non-key gem spawn rate around 40%%, got %.2f%% (%d/%d)", rate*100, spawnedCount, total)
	}
}

func TestGemProperties(t *testing.T) {
	peridot, ok := FindGem(GemPeridot)
	if !ok || peridot.PowerCost != 0 || peridot.Value != 50 {
		t.Fatalf("peridot gem properties mismatch: %+v", peridot)
	}

	prismatic, ok := FindGem(GemPrismatic)
	if !ok || prismatic.PowerCost != -25 || prismatic.Value != 1200 {
		t.Fatalf("prismatic gem properties mismatch: %+v", prismatic)
	}

	ruby, ok := FindGem(GemRuby)
	if !ok || ruby.PowerCost != 25 || ruby.Value != 450 {
		t.Fatalf("ruby gem properties mismatch: %+v", ruby)
	}

	emerald, ok := FindGem(GemEmerald)
	if !ok || emerald.PowerCost != 25 || emerald.Value != 175 {
		t.Fatalf("emerald gem properties mismatch: %+v", emerald)
	}

	topaz, ok := FindGem(GemTopaz)
	if !ok || topaz.PowerCost != 25 || topaz.Value != 100 {
		t.Fatalf("topaz gem properties mismatch: %+v", topaz)
	}

	diamond, ok := FindGem(GemDiamond)
	if !ok || diamond.PowerCost != 25 || diamond.Value != 700 {
		t.Fatalf("diamond gem properties mismatch: %+v", diamond)
	}
}

func testGemsIntegration(t *testing.T, store *Store) {
	db := store.DB
	ctx := context.Background()

	guildID := "guild-gem-test"
	channelID := "channel-gem-test"
	userOwner := "user-gem-owner"
	userClicker1 := "user-gem-clicker-1"
	userClicker2 := "user-gem-clicker-2"
	rollID := "roll-gem-1"

	// Create users and guild_members
	for _, u := range []string{userOwner, userClicker1, userClicker2} {
		_, err := db.Exec(`INSERT INTO users(id, balance) VALUES($1, 0) ON CONFLICT(id) DO NOTHING`, u)
		if err != nil {
			t.Fatalf("failed inserting user: %v", err)
		}
		_, err = db.Exec(`INSERT INTO guild_members(guild_id, user_id, balance) VALUES($1, $2, 0) ON CONFLICT(guild_id, user_id) DO NOTHING`, guildID, u)
		if err != nil {
			t.Fatalf("failed inserting guild_member: %v", err)
		}
	}

	// Insert character
	charID := int64(999901)
	_, _ = db.Exec(`INSERT INTO gacha_characters(id, name, favourites, enabled) VALUES($1, 'Gem Test Char', 10, true) ON CONFLICT(id) DO NOTHING`, charID)

	// Insert roll with Ruby (value 450, cost 25)
	ruby, _ := FindGem(GemRuby)
	_, err := db.Exec(`
		INSERT INTO gacha_rolls(id, guild_id, channel_id, user_id, character_id, expires_at, request_id, gem_type, gem_value, gem_power_cost)
		VALUES($1, $2, $3, $4, $5, now() + interval '45 seconds', $6, $7, $8, $9)
	`, rollID, guildID, channelID, userOwner, charID, "req-gem-1", ruby.Type, ruby.Value, ruby.PowerCost)
	if err != nil {
		t.Fatalf("failed inserting roll: %v", err)
	}

	if _, err := store.ClaimGemAtomic(ctx, guildID, "other-channel", rollID, userClicker1); err == nil {
		t.Fatal("gem could be claimed from another channel")
	}
	if err := store.Claim(ctx, guildID, channelID, userClicker1, rollID); err != ErrClaim {
		t.Fatalf("gem roll accepted character claim: %v", err)
	}
	// Test concurrent claiming by clicker1 and clicker2
	var successCount int64
	var alreadyClaimedCount int64
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		u := fmt.Sprintf("user-concur-%d", i)
		go func(uid string) {
			defer wg.Done()
			res, err := store.ClaimGemAtomic(ctx, guildID, channelID, rollID, uid)
			if err != nil {
				t.Errorf("unexpected error in ClaimGemAtomic: %v", err)
				return
			}
			if res.AlreadyClaimed {
				atomic.AddInt64(&alreadyClaimedCount, 1)
			} else if res.ClaimedBy == uid {
				atomic.AddInt64(&successCount, 1)
			}
		}(u)
	}
	wg.Wait()

	if successCount != 1 {
		t.Fatalf("expected exactly 1 successful claim, got %d", successCount)
	}
	if alreadyClaimedCount != 9 {
		t.Fatalf("expected 9 already claimed rejections, got %d", alreadyClaimedCount)
	}

	// Verify roll record in DB
	var claimedBy string
	var gemVal int
	err = db.QueryRow(`SELECT gem_claimed_by, gem_value FROM gacha_rolls WHERE id=$1`, rollID).Scan(&claimedBy, &gemVal)
	if err != nil || claimedBy == "" || gemVal != 450 {
		t.Fatalf("gacha_rolls row state mismatch: claimedBy=%s, gemVal=%d, err=%v", claimedBy, gemVal, err)
	}

	// Test power deduction, power exhaustion, cost-0 gem, and restorative gem
	user := "user-power-test"
	_, _ = db.Exec(`INSERT INTO users(id, balance) VALUES($1, 0) ON CONFLICT(id) DO NOTHING`, user)
	_, _ = db.Exec(`INSERT INTO guild_members(guild_id, user_id, balance) VALUES($1, $2, 0) ON CONFLICT(guild_id, user_id) DO NOTHING`, guildID, user)

	charID2 := int64(999902)
	_, _ = db.Exec(`INSERT INTO gacha_characters(id, name, favourites, enabled) VALUES($1, 'Power Test Char', 10, true) ON CONFLICT(id) DO NOTHING`, charID2)

	// Claim 4 standard gems (each costs 25 power, starting from 100%)
	for i := 1; i <= 4; i++ {
		rID := fmt.Sprintf("roll-power-%d", i)
		_, err := db.Exec(`
			INSERT INTO gacha_rolls(id, guild_id, channel_id, user_id, character_id, expires_at, request_id, gem_type, gem_value, gem_power_cost)
			VALUES($1, $2, $3, $4, $5, now() + interval '45 seconds', $6, $7, $8, $9)
		`, rID, guildID, channelID, user, charID2, fmt.Sprintf("req-power-%d", i), ruby.Type, ruby.Value, ruby.PowerCost)
		if err != nil {
			t.Fatalf("failed inserting roll %d: %v", i, err)
		}

		res, err := store.ClaimGemAtomic(ctx, guildID, channelID, rID, user)
		if err != nil {
			t.Fatalf("claim %d failed: %v", i, err)
		}
		expectedPower := 100 - (i * 25)
		if res.RemainingPower != expectedPower {
			t.Fatalf("claim %d: expected remaining power %d, got %d", i, expectedPower, res.RemainingPower)
		}
	}

	// 5th standard gem: user now has 0% power, should be rejected!
	rollID5 := "roll-power-5"
	_, err = db.Exec(`
		INSERT INTO gacha_rolls(id, guild_id, channel_id, user_id, character_id, expires_at, request_id, gem_type, gem_value, gem_power_cost)
		VALUES($1, $2, $3, $4, $5, now() + interval '45 seconds', $6, $7, $8, $9)
	`, rollID5, guildID, channelID, user, charID2, "req-power-5", ruby.Type, ruby.Value, ruby.PowerCost)
	if err != nil {
		t.Fatalf("failed inserting roll 5: %v", err)
	}

	res5, err := store.ClaimGemAtomic(ctx, guildID, channelID, rollID5, user)
	if err != nil {
		t.Fatalf("claim 5 error: %v", err)
	}
	if !res5.InsufficientPower {
		t.Fatalf("expected claim 5 to fail with InsufficientPower, got: %+v", res5)
	}

	// 6th gem: Peridot (costs 0 power!) -> should SUCCEED even with 0% power!
	peridot, _ := FindGem(GemPeridot)
	rollID6 := "roll-power-6"
	_, err = db.Exec(`
		INSERT INTO gacha_rolls(id, guild_id, channel_id, user_id, character_id, expires_at, request_id, gem_type, gem_value, gem_power_cost)
		VALUES($1, $2, $3, $4, $5, now() + interval '45 seconds', $6, $7, $8, $9)
	`, rollID6, guildID, channelID, user, charID2, "req-power-6", peridot.Type, peridot.Value, peridot.PowerCost)
	if err != nil {
		t.Fatalf("failed inserting roll 6: %v", err)
	}

	res6, err := store.ClaimGemAtomic(ctx, guildID, channelID, rollID6, user)
	if err != nil {
		t.Fatalf("claim 6 error: %v", err)
	}
	if res6.InsufficientPower || res6.RemainingPower != 0 {
		t.Fatalf("expected Peridot (cost 0) to succeed with 0 remaining power, got: %+v", res6)
	}

	// 7th gem: Prismatic (restores 25 power!) -> should SUCCEED and bring power to 25%!
	prismatic, _ := FindGem(GemPrismatic)
	rollID7 := "roll-power-7"
	_, err = db.Exec(`
		INSERT INTO gacha_rolls(id, guild_id, channel_id, user_id, character_id, expires_at, request_id, gem_type, gem_value, gem_power_cost)
		VALUES($1, $2, $3, $4, $5, now() + interval '45 seconds', $6, $7, $8, $9)
	`, rollID7, guildID, channelID, user, charID2, "req-power-7", prismatic.Type, prismatic.Value, prismatic.PowerCost)
	if err != nil {
		t.Fatalf("failed inserting roll 7: %v", err)
	}

	res7, err := store.ClaimGemAtomic(ctx, guildID, channelID, rollID7, user)
	if err != nil {
		t.Fatalf("claim 7 error: %v", err)
	}
	if res7.RemainingPower != 25 {
		t.Fatalf("expected Prismatic to restore power to 25%%, got: %d%%", res7.RemainingPower)
	}

	// Hourly reset test: simulate window expiring (move window_start back by 2 hours)
	_, err = db.Exec(`UPDATE gacha_players SET window_start = now() - interval '2 hours' WHERE guild_id=$1 AND user_id=$2`, guildID, user)
	if err != nil {
		t.Fatalf("failed updating window_start: %v", err)
	}

	effectivePower, err := store.GetEffectiveGemPower(ctx, guildID, user, time.Now())
	if err != nil {
		t.Fatalf("GetEffectiveGemPower error: %v", err)
	}
	if effectivePower != MaxGemPower {
		t.Fatalf("expected power to reset to 100%%, got %d%%", effectivePower)
	}
}

func TestGemsIntegrationIfConfigured(t *testing.T) {
	dsn := os.Getenv("GACHA_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("skipping integration test because GACHA_TEST_DATABASE_URL is not set")
	}
}

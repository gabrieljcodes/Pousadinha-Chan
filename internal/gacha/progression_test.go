package gacha

import (
	"context"
	"fmt"
	"math"
	"sync"
	"testing"
)

func TestProgressionValue(t *testing.T) {
	for _, v := range []struct{ likes, claimed, keys, want int64 }{
		{-10, -1, -3, 10}, {666, 0, 0, 10}, {10000, 0, 0, 150}, {10000, 1000, 0, 165},
		{10000, 0, 1, 153}, {10000, 0, 10, 195}, {10000, 1000, 10, 214},
		{0, 0, 5, 11}, {0, 0, 10, 13}, {1000, 10000, 0, 30}, {2000000, 0, 0, 30000},
	} {
		got, e := CalculateValue(v.likes, v.claimed, v.keys)
		if e != nil || got != v.want {
			t.Fatal(v, got, e)
		}
	}
	if _, e := CalculateValue(math.MaxInt64, math.MaxInt64, math.MaxInt64); e == nil {
		t.Fatal("overflow silently capped or wrapped")
	}
	last := int64(0)
	for keys := int64(0); keys < 1000; keys++ {
		got, e := CalculateValue(10000, 1000, keys)
		if e != nil || got < last {
			t.Fatal("key bonus decreased")
		}
		last = got
	}
}
func testProgression(t *testing.T, s *Store) {
	ctx := context.Background()
	db := s.DB
	must := func(e error) {
		t.Helper()
		if e != nil {
			t.Fatal(e)
		}
	}
	exec := func(query string, args ...any) { t.Helper(); _, e := db.ExecContext(ctx, query, args...); must(e) }
	// An isolated pool makes the real RNG deterministic without replacing production code.
	exec(`UPDATE gacha_characters SET enabled=false`)
	cid, e := s.Import(ctx, Character{Provider: "fixture", ExternalID: "progression", Name: "Keys character", Gender: "Female", Favourites: 10000, Works: []Work{{ExternalID: "progression", Kind: "anime", Title: "Progression"}}})
	must(e)
	exec(`UPDATE gacha_characters SET enabled=true WHERE id=$1`, cid)
	exec(`INSERT INTO gacha_assets(character_id,provider,external_id,source_url,sha256,path,media_type,status) VALUES($1,'fixture','progression','https://example.com','progression','progression.png','image/png','approved')`, cid)
	acquire := func(guild, user string) {
		t.Helper()
		tx, e := db.BeginTx(ctx, nil)
		must(e)
		defer tx.Rollback()
		must(ensurePlayer(ctx, tx, guild, user))
		_, e = tx.ExecContext(ctx, `INSERT INTO gacha_collection(guild_id,user_id,character_id) VALUES($1,$2,$3)`, guild, user, cid)
		must(e)
		must(tx.Commit())
	}
	keys := func(guild string) int64 {
		t.Helper()
		var n int64
		must(db.QueryRow(`SELECT keys FROM gacha_collection WHERE guild_id=$1 AND character_id=$2`, guild, cid).Scan(&n))
		return n
	}
	local := *s
	local.Config.RollsPerHour = 100
	acquire("keys", "key-owner")
	roll, e := local.Roll(ctx, "keys", "channel", "key-owner", "key-roll-1")
	must(e)
	if !roll.KeyEarned || roll.Card.Keys != 1 || roll.Card.Value != 153 {
		t.Fatal("first key", roll)
	}
	repeat, e := local.Roll(ctx, "keys", "channel", "key-owner", "key-roll-1")
	must(e)
	if !repeat.KeyEarned || keys("keys") != 1 {
		t.Fatal("replay duplicated reward")
	}
	other, e := local.Roll(ctx, "keys", "channel", "stranger", "key-stranger")
	must(e)
	if other.KeyEarned || keys("keys") != 1 {
		t.Fatal("non-owner earned key")
	}
	other, e = local.Roll(ctx, "unowned-keys", "channel", "key-owner", "key-other-guild")
	must(e)
	if other.KeyEarned || other.Card.Keys != 0 || other.Card.Value != 150 {
		t.Fatal("guild progression leaked")
	}
	// Concurrent duplicate deliveries award exactly one key.
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for n := 0; n < 2; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := local.Roll(ctx, "keys", "channel", "key-owner", "key-duplicate")
			results <- e
		}()
	}
	wg.Wait()
	close(results)
	for e := range results {
		must(e)
	}
	if keys("keys") != 2 {
		t.Fatal("concurrent replay reward")
	}
	// Different rolls each earn one key, without lost updates.
	results = make(chan error, 2)
	for n := 0; n < 2; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_, e := local.Roll(ctx, "keys", "channel", "key-owner", fmt.Sprintf("key-concurrent-%d", n))
			results <- e
		}(n)
	}
	wg.Wait()
	close(results)
	for e := range results {
		must(e)
	}
	if keys("keys") != 4 {
		t.Fatal("lost key increment")
	}
	must(local.CancelDelivery(ctx, "key-roll-1"))
	must(local.CancelDelivery(ctx, "key-roll-1"))
	if keys("keys") != 3 {
		t.Fatal("refund not idempotent")
	}
	repeat, e = local.Roll(ctx, "keys", "channel", "key-owner", "key-roll-1")
	must(e)
	if repeat.KeyEarned {
		t.Fatal("revoked reward displayed as earned")
	}
	for n := 0; n < 7; n++ {
		_, e = local.Roll(ctx, "keys", "channel", "key-owner", fmt.Sprintf("milestone-%d", n))
		must(e)
	}
	if keys("keys") != 10 {
		t.Fatal("milestone missing")
	}
	c := Card{ID: cid}
	must(priceCards(ctx, db, "keys", &c))
	if c.Value != 195 || c.Keys != 10 {
		t.Fatal("milestone price", c)
	}
	count, total, e := local.HaremSummary(ctx, "keys", "key-owner")
	must(e)
	if count != 1 || total != c.Value {
		t.Fatal("summary pricing mismatch", total, c.Value)
	}
	ranks, e := local.Rankings(ctx, "keys", 1)
	must(e)
	if len(ranks) != 1 || ranks[0].Value != c.Value {
		t.Fatal("ranking price mismatch")
	}
	cards, e := local.Cards(ctx, "keys", "key-owner", "", 1)
	must(e)
	if len(cards) != 1 || cards[0].Value != c.Value {
		t.Fatal("harem card mismatch")
	}
	// Bulk population changes affect unowned characters too, without historical claim inflation.
	exec(`INSERT INTO gacha_characters(name) SELECT 'Population fixture' FROM generate_series(1,99)`)
	exec(`INSERT INTO gacha_collection(guild_id,user_id,character_id) SELECT 'keys','key-owner',id FROM gacha_characters WHERE name='Population fixture'`)
	var unownedID int64
	must(db.QueryRow(`INSERT INTO gacha_characters(name,favourites) VALUES('Unowned value',10000) RETURNING id`).Scan(&unownedID))
	unowned := Card{ID: unownedID}
	must(priceCards(ctx, db, "keys", &unowned))
	if unowned.Claimed != 100 || unowned.Value != 151 {
		t.Fatal("server bonus on unowned character", unowned)
	}
	exec(`DELETE FROM gacha_collection WHERE guild_id='keys' AND character_id<>$1`, cid)
	must(priceCards(ctx, db, "keys", &unowned))
	if unowned.Value != 150 {
		t.Fatal("divorce should reduce current population bonus")
	}

	// Both sides retain their own keys through a trade and a trade back.
	partner, e := s.Import(ctx, Character{Provider: "fixture", ExternalID: "key-partner", Name: "Key partner", Favourites: 100})
	must(e)
	tx, e := db.BeginTx(ctx, nil)
	must(e)
	must(ensurePlayer(ctx, tx, "keys", "key-partner"))
	_, e = tx.ExecContext(ctx, `INSERT INTO gacha_collection(guild_id,user_id,character_id,keys) VALUES('keys','key-partner',$1,3)`, partner)
	must(e)
	must(tx.Commit())
	trade, e := local.CreateAction(ctx, "keys", "channel", "key-owner", "key-partner", "key-trade", "trade", cid, partner)
	must(e)
	_, e = local.ResolveAction(ctx, "keys", "channel", "key-partner", trade.ID, "accept")
	must(e)
	var partnerKeys int64
	must(db.QueryRow(`SELECT keys FROM gacha_collection WHERE guild_id='keys' AND character_id=$1`, partner).Scan(&partnerKeys))
	if keys("keys") != 10 || partnerKeys != 3 {
		t.Fatal("trade lost character keys")
	}
	trade, e = local.CreateAction(ctx, "keys", "channel", "key-partner", "key-owner", "key-trade-back", "trade", cid, partner)
	must(e)
	_, e = local.ResolveAction(ctx, "keys", "channel", "key-owner", trade.ID, "accept")
	must(e)
	// Keys follow gifts and compensation follows their lineage across that transfer.
	a, e := local.CreateAction(ctx, "keys", "channel", "key-owner", "key-recipient", "keys-gift", "gift", cid, 0)
	must(e)
	_, e = local.ResolveAction(ctx, "keys", "channel", "key-recipient", a.ID, "accept")
	must(e)
	if keys("keys") != 10 {
		t.Fatal("gift dropped keys")
	}
	must(local.CancelDelivery(ctx, "milestone-0"))
	if keys("keys") != 9 {
		t.Fatal("reward reversal did not follow gift")
	}
	// Divorce quote includes the earned bonus and is frozen against further keys.
	a, e = local.CreateAction(ctx, "keys", "channel", "key-recipient", "", "keys-divorce", "divorce", cid, 0)
	must(e)
	if a.Payout != 177 {
		t.Fatal("divorce omitted keys", a.Payout)
	}
	earned, e := local.Roll(ctx, "keys", "channel", "key-recipient", "before-divorce-key")
	must(e)
	if !earned.KeyEarned {
		t.Fatal("new owner did not earn key")
	}
	_, e = local.ResolveAction(ctx, "keys", "channel", "key-recipient", a.ID, "accept")
	must(e)
	var wallet int64
	must(db.QueryRow(`SELECT balance FROM guild_members WHERE guild_id='keys' AND user_id='key-recipient'`).Scan(&wallet))
	if wallet != 177 {
		t.Fatal("quote was not frozen", wallet)
	}
	acquire("keys", "key-recipient")
	if keys("keys") != 0 {
		t.Fatal("divorce kept keys")
	}
	_, e = local.Roll(ctx, "keys", "channel", "key-recipient", "new-lineage-key")
	must(e)
	must(local.CancelDelivery(ctx, "before-divorce-key"))
	if keys("keys") != 1 {
		t.Fatal("old reward refund touched new ownership cycle")
	}
	// Unlimited gameplay values are accepted by the payout constraint.
	exec(`UPDATE gacha_characters SET favourites=2000000 WHERE id=$1`, cid)
	a, e = local.CreateAction(ctx, "keys", "channel", "key-recipient", "", "uncapped-price", "divorce", cid, 0)
	must(e)
	if a.Payout <= 25000 {
		t.Fatal("old cap remains", a.Payout)
	}
	// Only pre-upgrade pending divorce quotes are canceled by a repeated migration.
	old, e := local.CreateAction(ctx, "keys", "channel", "key-recipient", "", "old-quote", "divorce", cid, 0)
	must(e)
	exec(`UPDATE gacha_actions SET pricing_version=1 WHERE id=$1`, old.ID)
	must(local.Migrate(ctx))
	var status string
	must(db.QueryRow(`SELECT status FROM gacha_actions WHERE id=$1`, old.ID).Scan(&status))
	if status != "cancelled" {
		t.Fatal("legacy quote not retired")
	}
	must(db.QueryRow(`SELECT status FROM gacha_actions WHERE id=$1`, a.ID).Scan(&status))
	if status != "pending" {
		t.Fatal("new quote canceled on restart")
	}
	for _, v := range []struct{ likes, claimed, keys int64 }{{0, 0, 0}, {667, 100, 9}, {10000, 1000, 10}, {2000000, 10000, 100}, {math.MaxInt32, 1000000, 10000}} {
		expected, e := CalculateValue(v.likes, v.claimed, v.keys)
		must(e)
		var actual int64
		must(db.QueryRow(`SELECT gacha_character_value($1,$2,$3)::bigint`, v.likes, v.claimed, v.keys).Scan(&actual))
		if actual != expected {
			t.Fatal("SQL/Go disagreement", v, actual, expected)
		}
	}
}

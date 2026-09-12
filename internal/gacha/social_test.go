package gacha

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
)

func TestCharacterValue(t *testing.T) {
	for _, v := range []struct {
		likes int
		want  int64
	}{{-1, 10}, {0, 10}, {1, 10}, {100, 10}, {10000, 150}, {math.MaxInt32, 32212254}} {
		if got := CharacterValue(v.likes); got != v.want {
			t.Fatalf("likes=%d got=%d want=%d", v.likes, got, v.want)
		}
	}
	last := int64(0)
	for n := 0; n < 10000; n++ {
		value := CharacterValue(n)
		if value < last {
			t.Fatal("value decreased")
		}
		last = value
	}
}
func TestCommandParsing(t *testing.T) {
	for _, v := range []struct {
		args          []string
		action, query string
		page          int
	}{
		{[]string{"MM", "<@123456789012345678>", "2"}, "harem", "<@123456789012345678>", 2},
		{[]string{"gallery", "17", "3"}, "gallery", "17", 3},
		{[]string{"gallery", "17"}, "gallery", "17", 1},
		{[]string{"harem", "2"}, "harem", "", 2},
		{[]string{"wa"}, "wa", "", 1},
		{[]string{"search", "Monkey", "D", "Luffy"}, "search", "Monkey D Luffy", 1},
		{[]string{"tu"}, "status", "", 1},
		{[]string{"topchar", "unclaimed"}, "topchar", "unclaimed", 1},
		{[]string{"topu"}, "topchar_unclaimed", "", 1},
		{[]string{"topw"}, "topchar_waifu", "", 1},
		{[]string{"mmi"}, "harem_visual", "", 1},
		{[]string{"im", "Rem"}, "character", "Rem", 1},
	} {
		a, q, p, e := parseText(v.args)
		if e != nil || a != v.action || q != v.query || p != v.page {
			t.Fatal(v, a, q, p, e)
		}
	}
	for _, q := range []string{"", "bob", "<@&123456789012345678>", "123456789012345678 extra"} {
		if _, e := parseMember(q); e == nil {
			t.Fatalf("accepted member %q", q)
		}
	}
	if _, _, _, e := parseOffer("trade", "<@123456789012345678> 1 -2"); e == nil {
		t.Fatal("negative ID accepted")
	}
	if _, _, _, e := parseText([]string{"harem", "0"}); e == nil {
		t.Fatal("page zero accepted")
	}
}
func testSocial(t *testing.T, s *Store) {
	ctx := context.Background()
	db := s.DB
	must := func(e error) {
		t.Helper()
		if e != nil {
			t.Fatal(e)
		}
	}
	ids := []int64{}
	for n, v := range []struct{ gender, kind string }{{"Female", "anime"}, {"Male", "anime"}, {"Female", "game"}, {"Male", "game"}, {"", "anime"}, {"Female", "manga"}} {
		id, e := s.Import(ctx, Character{Provider: "fixture", ExternalID: fmt.Sprint(10000 + n), Name: fmt.Sprintf("Social %d", n), Gender: v.gender, Favourites: 100, Works: []Work{{ExternalID: fmt.Sprint(10000 + n), Kind: v.kind, Title: v.kind}}})
		must(e)
		ids = append(ids, id)
		_, e = db.Exec(`UPDATE gacha_characters SET enabled=true WHERE id=$1`, id)
		must(e)
		_, e = db.Exec(`INSERT INTO gacha_assets(character_id,provider,external_id,source_url,sha256,path,media_type,status) VALUES($1,'fixture',$2,'https://example.com',$2,$2,'image/png','approved')`, id, fmt.Sprint(10000+n))
		must(e)
	}
	poolStore := *s
	poolStore.Config.RollsPerHour = 100
	for _, pool := range Pools {
		for n := 0; n < 3; n++ {
			roll, e := poolStore.RollPool(ctx, "pool-tests", "channel", "pool-user", fmt.Sprintf("pool-%s-%d", pool.Code, n), pool.Code)
			must(e)
			var matches bool
			e = db.QueryRow(`SELECT ($2='' OR lower(trim(gender))=$2) AND ($3='' OR EXISTS(SELECT 1 FROM gacha_character_works cw JOIN gacha_works w ON w.id=cw.work_id WHERE cw.character_id=c.id AND w.kind=$3)) FROM gacha_characters c WHERE c.id=$1`, roll.Card.ID, pool.Gender, pool.Kind).Scan(&matches)
			must(e)
			if !matches {
				t.Fatal("wrong pool", pool.Code, roll.Card.ID)
			}
		}
	}
	// Empty filters spend nothing; other aliases share the same quota.
	_, e := db.Exec(`UPDATE gacha_characters SET enabled=false WHERE lower(gender)='male'`)
	must(e)
	if _, e = poolStore.RollPool(ctx, "empty-filter", "channel", "pool-user", "empty-filter", "h"); e != ErrEmpty {
		t.Fatal(e)
	}
	var used int
	must(db.QueryRow(`SELECT count(*) FROM gacha_players WHERE guild_id='empty-filter'`).Scan(&used))
	if used != 0 {
		t.Fatal("empty filter changed quota")
	}
	_, e = db.Exec(`UPDATE gacha_characters SET enabled=true WHERE lower(gender)='male'`)
	must(e)
	for n, code := range []string{"wa", "ha", "ma"} {
		_, e = s.RollPool(ctx, "shared-pool", "channel", "pool-user", fmt.Sprint("shared-", n), code)
		if n < 2 {
			must(e)
		} else if e != ErrLimit {
			t.Fatal("aliases bypassed quota", e)
		}
	}
	acquire := func(guild, user string, id int64) {
		t.Helper()
		tx, e := db.BeginTx(ctx, nil)
		must(e)
		defer tx.Rollback()
		must(ensurePlayer(ctx, tx, guild, user))
		_, e = tx.ExecContext(ctx, `INSERT INTO gacha_collection(guild_id,user_id,character_id) VALUES($1,$2,$3)`, guild, user, id)
		must(e)
		must(tx.Commit())
	}
	balance := func(guild, user string) int64 {
		t.Helper()
		var value int64
		must(db.QueryRow(`SELECT balance FROM guild_members WHERE guild_id=$1 AND user_id=$2`, guild, user).Scan(&value))
		return value
	}
	owner := func(guild string, id int64) string {
		t.Helper()
		var user string
		must(db.QueryRow(`SELECT user_id FROM gacha_collection WHERE guild_id=$1 AND character_id=$2`, guild, id).Scan(&user))
		return user
	}
	acquire("social", "social-alice", ids[0])
	acquire("other-social", "social-alice", ids[0])
	a, e := s.CreateAction(ctx, "social", "channel", "social-alice", "", "divorce-1", "divorce", ids[0], 0)
	must(e)
	replay, e := s.CreateAction(ctx, "social", "channel", "social-alice", "", "divorce-1", "divorce", ids[0], 0)
	must(e)
	if a.ID != replay.ID {
		t.Fatal("action request duplicated")
	}
	if _, e = s.ResolveAction(ctx, "social", "channel", "social-bob", a.ID, "accept"); e == nil {
		t.Fatal("outsider confirmed divorce")
	}
	if _, e = s.ResolveAction(ctx, "other-social", "channel", "social-alice", a.ID, "accept"); e == nil {
		t.Fatal("cross-guild divorce")
	}
	if _, e = s.ResolveAction(ctx, "social", "wrong-channel", "social-alice", a.ID, "accept"); e == nil {
		t.Fatal("cross-channel divorce")
	}
	_, e = db.Exec(`UPDATE gacha_characters SET favourites=10000 WHERE id=$1`, ids[0])
	must(e)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for n := 0; n < 2; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := s.ResolveAction(ctx, "social", "channel", "social-alice", a.ID, "accept")
			results <- e
		}()
	}
	wg.Wait()
	close(results)
	for e := range results {
		must(e)
	}
	if balance("social", "social-alice") != 10 || balance("other-social", "social-alice") != 0 {
		t.Fatal("incorrect or duplicated payout")
	}
	var global int64
	must(db.QueryRow(`SELECT balance FROM users WHERE id='social-alice'`).Scan(&global))
	if global != 0 {
		t.Fatal("credited legacy global economy")
	}
	if owner("other-social", ids[0]) != "social-alice" {
		t.Fatal("other guild collection changed")
	}
	// Reacquiring the same character cannot revive an old pending divorce.
	acquire("social", "social-alice", ids[0])
	stale, e := s.CreateAction(ctx, "social", "channel", "social-alice", "", "stale-divorce", "divorce", ids[0], 0)
	must(e)
	acquire("social", "social-bob", ids[1])
	trade, e := s.CreateAction(ctx, "social", "channel", "social-alice", "social-bob", "trade-1", "trade", ids[0], ids[1])
	must(e)
	if _, e = s.ResolveAction(ctx, "social", "channel", "social-alice", trade.ID, "accept"); e == nil {
		t.Fatal("proposer accepted own trade")
	}
	done, e := s.ResolveAction(ctx, "social", "channel", "social-bob", trade.ID, "accept")
	must(e)
	if done.Status != "completed" || owner("social", ids[0]) != "social-bob" || owner("social", ids[1]) != "social-alice" {
		t.Fatal("non-atomic trade")
	}
	gift, e := s.CreateAction(ctx, "social", "channel", "social-bob", "social-alice", "gift-back", "gift", ids[0], 0)
	must(e)
	_, e = s.ResolveAction(ctx, "social", "channel", "social-alice", gift.ID, "accept")
	must(e)
	done, e = s.ResolveAction(ctx, "social", "channel", "social-alice", stale.ID, "accept")
	must(e)
	if done.Status != "stale" || balance("social", "social-alice") != 10 {
		t.Fatal("old ownership offer revived")
	}
	// A trade becomes stale if either character left the promised harem.
	pending, e := s.CreateAction(ctx, "social", "channel", "social-alice", "social-bob", "gift-decline", "gift", ids[0], 0)
	must(e)
	done, e = s.ResolveAction(ctx, "social", "channel", "social-bob", pending.ID, "decline")
	must(e)
	if done.Status != "declined" || owner("social", ids[0]) != "social-alice" {
		t.Fatal("decline transferred character")
	}
	pending, e = s.CreateAction(ctx, "social", "channel", "social-alice", "", "expire-divorce", "divorce", ids[0], 0)
	must(e)
	_, e = db.Exec(`UPDATE gacha_actions SET expires_at=now()-interval '1 second' WHERE id=$1`, pending.ID)
	must(e)
	done, e = s.ResolveAction(ctx, "social", "channel", "social-alice", pending.ID, "accept")
	must(e)
	if done.Status != "expired" {
		t.Fatal("expired action completed")
	}
	// Competing trade acceptance and divorce may only consume ownership once.
	acquire("social", "social-bob", ids[2])
	trade, e = s.CreateAction(ctx, "social", "channel", "social-alice", "social-bob", "race-trade", "trade", ids[0], ids[2])
	must(e)
	divorce, e := s.CreateAction(ctx, "social", "channel", "social-alice", "", "race-divorce", "divorce", ids[0], 0)
	must(e)
	statuses := make(chan string, 2)
	results = make(chan error, 2)
	for _, v := range []struct {
		a    Action
		user string
	}{{trade, "social-bob"}, {divorce, "social-alice"}} {
		wg.Add(1)
		go func(a Action, user string) {
			defer wg.Done()
			result, e := s.ResolveAction(ctx, "social", "channel", user, a.ID, "accept")
			results <- e
			statuses <- result.Status
		}(v.a, v.user)
	}
	wg.Wait()
	close(results)
	close(statuses)
	for e := range results {
		must(e)
	}
	completed := 0
	for status := range statuses {
		if status == "completed" {
			completed++
		}
	}
	if completed != 1 {
		t.Fatal("ownership consumed twice", completed)
	}

	// A failed wallet write must not release the character or complete the receipt.
	acquire("overflow", "overflow-user", ids[3])
	_, e = db.Exec(`UPDATE guild_members SET balance=9223372036854775807 WHERE guild_id='overflow'`)
	must(e)
	blocked, e := s.CreateAction(ctx, "overflow", "channel", "overflow-user", "", "overflow-divorce", "divorce", ids[3], 0)
	must(e)
	if _, e = s.ResolveAction(ctx, "overflow", "channel", "overflow-user", blocked.ID, "accept"); e == nil {
		t.Fatal("overflow accepted")
	}
	if owner("overflow", ids[3]) != "overflow-user" {
		t.Fatal("failed payment lost character")
	}
	var blockedStatus string
	must(db.QueryRow(`SELECT status FROM gacha_actions WHERE id=$1`, blocked.ID).Scan(&blockedStatus))
	if blockedStatus != "pending" {
		t.Fatal("failed payment completed action")
	}
	// Cancellation does not move either character, and SQL/Go valuation stay identical.
	pending, e = s.CreateAction(ctx, "social", "channel", "social-alice", "social-bob", "cancel-gift", "gift", ids[1], 0)
	must(e)
	done, e = s.ResolveAction(ctx, "social", "channel", "social-alice", pending.ID, "cancel")
	must(e)
	if done.Status != "cancelled" || owner("social", ids[1]) != "social-alice" {
		t.Fatal("cancel changed ownership")
	}
	for _, likes := range []int{0, 1, 100, 10000, math.MaxInt32} {
		var value int64
		must(db.QueryRow(`SELECT gacha_character_value($1::bigint,0,0)::bigint`, likes).Scan(&value))
		if value != CharacterValue(likes) {
			t.Fatal("SQL/Go pricing disagree", likes, value)
		}
	}
	// English card output displays the same price as the payment formula.
	msg, e := s.Execute(ctx, "social", "channel", "social-alice", "view-test", "character", fmt.Sprint(ids[1]), 1)
	must(e)
	found := false
	for _, field := range msg.Embeds[0].Fields {
		if field.Name == "Character value" && strings.Contains(field.Value, "10") {
			found = true
		}
	}
	if !found {
		t.Fatal("missing value")
	}
	count, value, e := s.HaremSummary(ctx, "other-social", "social-alice")
	must(e)
	if count != 1 || value != 150 {
		t.Fatal(count, value)
	}

	// Test FindCharacter
	card, _, e := s.FindCharacter(ctx, "social", "Social 1")
	must(e)
	if card.ID != ids[1] || card.Owner != "social-alice" {
		t.Fatalf("unexpected find character result: got card %d owner %s, want %d social-alice", card.ID, card.Owner, ids[1])
	}
	unclaimedCard, _, e := s.FindCharacter(ctx, "social", "Social 4")
	must(e)
	if unclaimedCard.Owner != "" {
		t.Fatalf("expected unclaimed character, got owner %s", unclaimedCard.Owner)
	}

	// Test TopCharacters
	topEntries, totalTop, e := s.TopCharacters(ctx, "social", "unclaimed", "female", 1, 10)
	must(e)
	if len(topEntries) == 0 || totalTop == 0 {
		t.Fatalf("expected top characters, got %d (total %d)", len(topEntries), totalTop)
	}

	// Test HaremCardAt
	haremCard, totalHarem, e := s.HaremCardAt(ctx, "social", "social-alice", 0)
	must(e)
	if haremCard.ID != ids[1] || totalHarem != 1 {
		t.Fatalf("unexpected harem card at 0: card %d total %d", haremCard.ID, totalHarem)
	}

	// Test Execute with status (tu)
	tuMsg, e := s.Execute(ctx, "social", "channel", "social-alice", "tu-test", "tu", "", 1)
	must(e)
	if len(tuMsg.Embeds) == 0 {
		t.Fatal("expected embed in status message")
	}

	// Test Execute with visual harem
	haremVisualMsg, e := s.Execute(ctx, "social", "channel", "social-alice", "harem-vis-test", "harem", "-i", 1)
	must(e)
	if len(haremVisualMsg.Embeds) == 0 || haremVisualMsg.Embeds[0].Image == nil {
		t.Fatal("expected image in visual harem embed")
	}

	// Test Execute with topchar
	topMsg, e := s.Execute(ctx, "social", "channel", "social-alice", "topchar-test", "topchar", "unclaimed", 1)
	must(e)
	if len(topMsg.Embeds) == 0 {
		t.Fatal("expected embed in topchar message")
	}

	// Test Execute with topchar_waifu
	topwMsg, e := s.Execute(ctx, "social", "channel", "social-alice", "topw-test", "topchar_waifu", "", 1)
	must(e)
	if len(topwMsg.Embeds) == 0 || !strings.Contains(topwMsg.Embeds[0].Title, "Waifus") {
		t.Fatalf("expected Waifus in title, got %q", topwMsg.Embeds[0].Title)
	}

	// Test Execute with pagination query "all female"
	topwPage2Msg, e := s.Execute(ctx, "social", "channel", "social-alice", "topw-p2-test", "topchar", "all female", 2)
	must(e)
	if len(topwPage2Msg.Embeds) == 0 || !strings.Contains(topwPage2Msg.Embeds[0].Title, "Waifus • Página 2") {
		t.Fatalf("expected Waifus • Página 2 in title, got %q", topwPage2Msg.Embeds[0].Title)
	}
}

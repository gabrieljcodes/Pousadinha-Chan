package gacha

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"

	"github.com/bwmarrin/discordgo"
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
	for _, q := range []string{"", "bob", "<@&123456789012345678>", "123456789012345678 extra"} {
		if _, e := parseMember(q); e == nil {
			t.Fatalf("accepted member %q", q)
		}
	}
	if _, _, _, e := parseOffer("trade", "<@123456789012345678> 1 -2"); e == nil {
		t.Fatal("negative ID accepted")
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
	poolStore := &Store{DB: s.DB, Config: s.Config}
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

	must(s.Wish(ctx, "social", "social-alice", ids[1], false))
	wishlistMessage, e := s.Execute(ctx, "social", "channel", "social-alice", "wishlist-view", "wishes", "", 1)
	must(e)
	if len(wishlistMessage.Embeds) != 1 || !strings.Contains(wishlistMessage.Embeds[0].Description, "Social 1") {
		t.Fatal("wishlist did not render its character")
	}

	// Test HaremCardAt
	haremCard, totalHarem, e := s.HaremCardAt(ctx, "social", "social-alice", 0)
	must(e)
	expectedHarem := int64(1)
	if owner("social", ids[2]) == "social-alice" {
		expectedHarem = 2
	}
	if haremCard.ID != ids[1] || totalHarem != expectedHarem {
		t.Fatalf("unexpected harem card at 0: card %d total %d", haremCard.ID, totalHarem)
	}

	// Test the canonical profile action.
	tuMsg, e := s.Execute(ctx, "social", "channel", "social-alice", "profile-test", "profile", "", 1)
	must(e)
	if len(tuMsg.Embeds) == 0 {
		t.Fatal("expected embed in status message")
	}

	// Test Execute with visual harem
	haremVisualMsg, e := s.Execute(ctx, "social", "channel", "social-alice", "harem-vis-test", "harem_visual", "", 1)
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
	if len(topwPage2Msg.Embeds) == 0 || !strings.Contains(topwPage2Msg.Embeds[0].Title, "Waifus • Page 2") {
		t.Fatalf("expected Waifus • Page 2 in title, got %q", topwPage2Msg.Embeds[0].Title)
	}

	// Test navigationTopChar interactive components and verify NO duplicate CustomIDs exist
	for _, claim := range []string{"all", "unclaimed"} {
		for _, gender := range []string{"all", "female", "male"} {
			for _, page := range []int{1, 2, 5} {
				topComponents := navigationTopChar(claim, gender, page, true)
				if len(topComponents) != 2 {
					t.Fatalf("expected 2 component rows in topchar navigation, got %d", len(topComponents))
				}
				seenIDs := make(map[string]bool)
				for rowIdx, row := range topComponents {
					actionsRow, ok := row.(discordgo.ActionsRow)
					if !ok {
						t.Fatalf("expected ActionsRow at row %d", rowIdx)
					}
					for compIdx, comp := range actionsRow.Components {
						btn, ok := comp.(discordgo.Button)
						if !ok {
							t.Fatalf("expected Button at row %d comp %d", rowIdx, compIdx)
						}
						if seenIDs[btn.CustomID] {
							t.Fatalf("duplicate CustomID %q found in navigationTopChar(claim=%q, gender=%q, page=%d)", btn.CustomID, claim, gender, page)
						}
						seenIDs[btn.CustomID] = true
					}
				}
			}
		}
	}

	// Test navigation harem list toggle
	haremVisualComponents := navigation("harem_visual", "alice", 1, true)
	if len(haremVisualComponents) == 0 {
		t.Fatal("expected components in harem_visual navigation")
	}

	// Test wishlist Execution with numeric ID and name
	wishMsg, e := s.Execute(ctx, "social", "channel", "social-alice", "wish-test", "wish", fmt.Sprintf("%d", ids[1]), 1)
	must(e)
	if !strings.Contains(wishMsg.Content, "added to your wishlist") || !strings.Contains(wishMsg.Content, "Social 1") {
		t.Fatalf("unexpected wishMsg: %q", wishMsg.Content)
	}

	wishByNameMsg, e := s.Execute(ctx, "social", "channel", "social-alice", "wish-name-test", "wish", "Social 2", 1)
	must(e)
	if !strings.Contains(wishByNameMsg.Content, "added to your wishlist") || !strings.Contains(wishByNameMsg.Content, "Social 2") {
		t.Fatalf("unexpected wishByNameMsg: %q", wishByNameMsg.Content)
	}

	wishesListMsg, e := s.Execute(ctx, "social", "channel", "social-alice", "wishes-test", "wishes", "", 1)
	must(e)
	if len(wishesListMsg.Embeds) == 0 || !strings.Contains(wishesListMsg.Embeds[0].Title, "Wishlist") {
		t.Fatalf("unexpected wishes embed title: %v", wishesListMsg.Embeds)
	}
	if !strings.Contains(wishesListMsg.Embeds[0].Description, "Social 2") {
		t.Fatalf("wishlist missing character added by name: %s", wishesListMsg.Embeds[0].Description)
	}

	// Test Wish Drop mechanics (gold embed, mentions, and wish bonus drop)
	s.Config.WishBonusPercent = 100.0
	must(s.Wish(ctx, "social", "social-bob", ids[0], false))
	must(s.Wish(ctx, "social", "social-alice", ids[0], false))

	rollWishMsg, e := s.Execute(ctx, "social", "channel", "social-alice", "wish-roll-test", "wa", "", 1)
	must(e)
	if len(rollWishMsg.Embeds) == 0 {
		t.Fatal("expected roll embed")
	}
	if rollWishMsg.Embeds[0].Color != 0xffd700 {
		t.Fatalf("expected gold embed color 0xffd700 for wish roll, got 0x%x", rollWishMsg.Embeds[0].Color)
	}
	if !strings.Contains(rollWishMsg.Content, "<@social-alice>") || !strings.Contains(rollWishMsg.Content, "<@social-bob>") {
		t.Fatalf("expected wish mentions in content, got %q", rollWishMsg.Content)
	}
	if rollWishMsg.AllowedMentions == nil || len(rollWishMsg.AllowedMentions.Users) < 2 {
		t.Fatalf("expected AllowedMentions with wish users, got %+v", rollWishMsg.AllowedMentions)
	}
	if !strings.Contains(rollWishMsg.Embeds[0].Description, "Wished!") {
		t.Fatalf("expected Wished banner in embed description, got %q", rollWishMsg.Embeds[0].Description)
	}

	// Test unwish by name and numeric ID
	unwishByNameMsg, e := s.Execute(ctx, "social", "channel", "social-alice", "unwish-name-test", "unwish", "Social 2", 1)
	must(e)
	if !strings.Contains(unwishByNameMsg.Content, "removed from your wishlist") || !strings.Contains(unwishByNameMsg.Content, "Social 2") {
		t.Fatalf("unexpected unwishByNameMsg: %q", unwishByNameMsg.Content)
	}

	unwishMsg, e := s.Execute(ctx, "social", "channel", "social-alice", "unwish-test", "unwish", fmt.Sprintf("%d", ids[1]), 1)
	must(e)
	if !strings.Contains(unwishMsg.Content, "removed from your wishlist") {
		t.Fatalf("unexpected unwishMsg: %q", unwishMsg.Content)
	}

	// Test Wishlist 5-slot limit
	limitUser := "wishlist-limit-user"
	for n := 0; n < 5; n++ {
		must(s.Wish(ctx, "social", limitUser, ids[n], false))
	}
	// 6th wish must exceed the limit of 5
	errOverLimit := s.Wish(ctx, "social", limitUser, ids[5], false)
	if errOverLimit == nil || !strings.Contains(errOverLimit.Error(), "5 characters") {
		t.Fatalf("expected wishlist full error with 5 characters, got %v", errOverLimit)
	}
	// Viewing the wishlist should display 5/5
	wishesLimitView, e := s.Execute(ctx, "social", "channel", limitUser, "wishlist-limit-view", "wishes", "", 1)
	must(e)
	if len(wishesLimitView.Embeds) == 0 || wishesLimitView.Embeds[0].Footer == nil || !strings.Contains(wishesLimitView.Embeds[0].Footer.Text, "5/5") {
		t.Fatalf("expected 5/5 in wishlist footer, got: %v", wishesLimitView.Embeds)
	}
	// Remove one wish and add the 6th character
	must(s.Wish(ctx, "social", limitUser, ids[0], true))
	must(s.Wish(ctx, "social", limitUser, ids[5], false))

	// Test SearchCharacters ordering by favourites and including work
	_, e = db.Exec(`UPDATE gacha_characters SET aliases='["SocialAlias"]'::jsonb, favourites=500 WHERE id=$1`, ids[1])
	must(e)
	_, e = db.Exec(`UPDATE gacha_characters SET favourites=1000 WHERE id=$1`, ids[0])
	must(e)

	searchResults, totalSearch, e := s.SearchCharacters(ctx, "social", "Social", 1, 10)
	must(e)
	if totalSearch == 0 || len(searchResults) == 0 {
		t.Fatalf("expected search results, got total %d", totalSearch)
	}
	for idx := 1; idx < len(searchResults); idx++ {
		if searchResults[idx].Favourites > searchResults[idx-1].Favourites {
			t.Fatalf("search results not ordered by favourites descending: %d > %d", searchResults[idx].Favourites, searchResults[idx-1].Favourites)
		}
	}

	// Test Execute search command
	searchMsg, e := s.Execute(ctx, "social", "channel", "social-alice", "search-test", "search", "Social", 1)
	must(e)
	if len(searchMsg.Embeds) == 0 || !strings.Contains(searchMsg.Embeds[0].Description, "Social 0") {
		t.Fatalf("expected search embed with character name and work, got: %v", searchMsg)
	}
	if len(searchMsg.Components) == 0 {
		t.Fatal("expected navigation components in search result")
	}

	// Test search by alias
	searchAliasMsg, e := s.Execute(ctx, "social", "channel", "social-alice", "search-alias-test", "search", "SocialAlias", 1)
	must(e)
	if len(searchAliasMsg.Embeds) == 0 || !strings.Contains(searchAliasMsg.Embeds[0].Description, "Social 1") {
		t.Fatalf("expected search by alias to find character, got: %v", searchAliasMsg)
	}

	// Test SearchCharactersByWork and Execute series command
	workChars, totalWork, primaryWork, e := s.SearchCharactersByWork(ctx, "social", "anime", 1, 10)
	must(e)
	if totalWork == 0 || len(workChars) == 0 {
		t.Fatalf("expected work characters for 'anime', got total %d", totalWork)
	}
	if primaryWork != "anime" {
		t.Fatalf("expected primaryWork 'anime', got %q", primaryWork)
	}

	seriesMsg, e := s.Execute(ctx, "social", "channel", "social-alice", "series-test", "series", "anime", 1)
	must(e)
	if len(seriesMsg.Embeds) == 0 || !strings.Contains(seriesMsg.Embeds[0].Title, "anime") {
		t.Fatalf("expected series embed with title anime, got: %v", seriesMsg)
	}
	if len(seriesMsg.Components) == 0 {
		t.Fatal("expected navigation components in series result")
	}

	nonExistentMsg, e := s.Execute(ctx, "social", "channel", "social-alice", "series-non-existent", "series", "NonExistentSeries123", 1)
	must(e)
	if !strings.Contains(nonExistentMsg.Content, "No anime or work found") {
		t.Fatalf("expected no works found message, got %q", nonExistentMsg.Content)
	}

	// Test divorce using character alias
	divorceAliasMsg, e := s.Execute(ctx, "social", "channel", "social-alice", "divorce-alias-test", "divorce", "SocialAlias", 1)
	must(e)
	if len(divorceAliasMsg.Embeds) == 0 || !strings.Contains(strings.ToLower(divorceAliasMsg.Embeds[0].Title), "divorce") {
		t.Fatalf("expected divorce offer embed, got: %v", divorceAliasMsg)
	}

	// Test gift using character alias
	giftAliasMsg, e := s.Execute(ctx, "social", "channel", "social-alice", "gift-alias-test", "gift", "<@123456789012345678> | SocialAlias", 1)
	must(e)
	if len(giftAliasMsg.Embeds) == 0 || !strings.Contains(strings.ToLower(giftAliasMsg.Embeds[0].Title), "gift") {
		t.Fatalf("expected gift offer embed, got: %v", giftAliasMsg)
	}

	// Test gallery and keys using alias
	galleryMsg, e := s.Execute(ctx, "social", "channel", "social-alice", "gallery-alias-test", "gallery", "SocialAlias", 1)
	must(e)
	if len(galleryMsg.Embeds) == 0 || !strings.Contains(galleryMsg.Embeds[0].Title, "Social 1") {
		t.Fatalf("expected gallery embed for alias, got: %v", galleryMsg)
	}

	keysMsg, e := s.Execute(ctx, "social", "channel", "social-alice", "keys-alias-test", "keys", "SocialAlias", 1)
	must(e)
	if len(keysMsg.Embeds) == 0 || !strings.Contains(keysMsg.Embeds[0].Title, "Social 1") {
		t.Fatalf("expected keys embed for alias, got: %v", keysMsg)
	}

	// --- Character Alias Tests ---
	// 1. Trying to manage alias when not married
	_, e = s.Execute(ctx, "social", "channel", "social-bob", "alias-not-owner", "alias", fmt.Sprintf("%d | SocialAlias", ids[1]), 1)
	if e == nil {
		t.Fatal("expected error when setting alias on unowned character")
	}

	// 2. Trying to manage alias on character with no aliases (Social 0 has empty aliases)
	_, e = s.Execute(ctx, "social", "channel", "social-alice", "alias-no-aliases", "alias", fmt.Sprintf("%d | SomeName", ids[0]), 1)
	if e == nil {
		t.Fatal("expected error when setting alias on character without aliases")
	}

	// 3. Trying to set an alias that is not in the character's alias list
	_, e = s.Execute(ctx, "social", "channel", "social-alice", "alias-invalid-choice", "alias", fmt.Sprintf("%d | NonExistentAlias", ids[1]), 1)
	if e == nil {
		t.Fatal("expected error when setting an alias that is not registered")
	}

	// 4. Listing available aliases
	listAliasMsg, e := s.Execute(ctx, "social", "channel", "social-alice", "alias-list", "alias", fmt.Sprintf("%d", ids[1]), 1)
	must(e)
	if len(listAliasMsg.Embeds) == 0 || !strings.Contains(listAliasMsg.Embeds[0].Description, "SocialAlias") {
		t.Fatalf("expected alias listing embed, got: %v", listAliasMsg)
	}

	// 5. Successfully setting valid alias
	setAliasMsg, e := s.Execute(ctx, "social", "channel", "social-alice", "alias-set", "alias", fmt.Sprintf("%d | SocialAlias", ids[1]), 1)
	must(e)
	if len(setAliasMsg.Embeds) == 0 || !strings.Contains(setAliasMsg.Embeds[0].Description, "SocialAlias") {
		t.Fatalf("expected success embed when setting alias, got: %v", setAliasMsg)
	}

	// 6. Verify priceCards replaces Name with alias
	cardCheck, _, e := s.FindCharacter(ctx, "social", fmt.Sprintf("%d", ids[1]))
	must(e)
	if cardCheck.Name != "SocialAlias" {
		t.Fatalf("expected Card.Name to be SocialAlias, got: %q", cardCheck.Name)
	}
	if cardCheck.OriginalName != "Social 1" {
		t.Fatalf("expected Card.OriginalName to be Social 1, got: %q", cardCheck.OriginalName)
	}

	// 7. Verify harem visual displays alias
	haremCard, _, e = s.HaremCardAt(ctx, "social", "social-alice", 0)
	must(e)
	if haremCard.Name != "SocialAlias" && haremCard.ID == ids[1] {
		t.Fatalf("expected HaremCardAt to display SocialAlias, got: %q", haremCard.Name)
	}

	// 8. Divorce and verify alias persists in server
	divAction, e := s.CreateAction(ctx, "social", "channel", "social-alice", "", "divorce-persist-test", "divorce", ids[1], 0)
	must(e)
	_, e = s.ResolveAction(ctx, "social", "channel", "social-alice", divAction.ID, "accept")
	must(e)
	cardPostDivorce, _, e := s.FindCharacter(ctx, "social", fmt.Sprintf("%d", ids[1]))
	must(e)
	if cardPostDivorce.Name != "SocialAlias" {
		t.Fatalf("expected Card.Name to persist as SocialAlias after divorce, got: %q", cardPostDivorce.Name)
	}

	// 9. Re-claim and test reset to default
	_, e = db.Exec(`INSERT INTO gacha_collection(guild_id,user_id,character_id) VALUES('social','social-alice',$1)`, ids[1])
	must(e)
	resetMsg, e := s.Execute(ctx, "social", "channel", "social-alice", "alias-reset", "alias", fmt.Sprintf("%d | default", ids[1]), 1)
	must(e)
	if len(resetMsg.Embeds) == 0 || !strings.Contains(resetMsg.Embeds[0].Description, "Social 1") {
		t.Fatalf("expected reset embed, got: %v", resetMsg)
	}

	cardReset, _, e := s.FindCharacter(ctx, "social", fmt.Sprintf("%d", ids[1]))
	must(e)
	if cardReset.Name != "Social 1" {
		t.Fatalf("expected Card.Name to be restored to Social 1, got: %q", cardReset.Name)
	}
}


package gacha

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestSplitWishlistInput(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  []string
	}{{"1 2 #3", []string{"1", "2", "#3"}}, {"Emilia; Rem, 17\nAsuka Langley", []string{"Emilia", "Rem", "17", "Asuka Langley"}}, {"Asuka Langley", []string{"Asuka Langley"}}} {
		got, err := SplitWishlistInput(tc.input)
		if err != nil || strings.Join(got, "|") != strings.Join(tc.want, "|") {
			t.Fatalf("%q: %v %v", tc.input, got, err)
		}
	}
	if _, err := SplitWishlistInput(" , ; "); err == nil {
		t.Fatal("empty list accepted")
	}
	if _, err := SplitWishlistInput(strings.Repeat("1,", 51)); err == nil {
		t.Fatal("unbounded list accepted")
	}
}

func testWishlistRegression(t *testing.T, original *Store) {
	ctx := context.Background()
	store := &Store{DB: original.DB, Config: original.Config}
	store.Config.WishlistLimit = 5
	db := store.DB
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	ids := make([]int64, 8)
	for n := range ids {
		id, err := store.Import(ctx, Character{Provider: "wishlist-test", ExternalID: fmt.Sprint(n), Name: fmt.Sprintf("Wishlist regression %d", n), Aliases: []string{fmt.Sprintf("WishAlias%d", n)}})
		must(err)
		ids[n] = id
		_, err = db.Exec(`UPDATE gacha_characters SET enabled=true WHERE id=$1`, id)
		must(err)
		_, err = db.Exec(`INSERT INTO gacha_assets(character_id,provider,external_id,source_url,sha256,path,media_type,status) VALUES($1,'wishlist-test',$2,'',$2,$2||'.png','image/png','approved')`, id, fmt.Sprintf("wish-regression-%d", n))
		must(err)
	}
	// Legacy scalar aliases must not break any name query, including other rows.
	for n, value := range []string{`null`, `"legacy alias"`, `{"alias":"legacy"}`} {
		_, err := db.Exec(`UPDATE gacha_characters SET aliases=$2::jsonb WHERE id=$1`, ids[n], value)
		must(err)
	}
	_, _, err := store.FindCharacter(ctx, "wishlist-regression", "Wishlist regression 3")
	must(err)
	_, _, err = store.SearchCharacters(ctx, "wishlist-regression", "Wishlist regression", 1, 10)
	must(err)
	// Duplicate entries consume no capacity; entries beyond 5 are ignored in order.
	queries := []string{fmt.Sprint(ids[0]), fmt.Sprint(ids[0]), "missing-entry"}
	for _, id := range ids[1:] {
		queries = append(queries, fmt.Sprint(id))
	}
	result, err := store.UpdateWishlist(ctx, "wishlist-regression", "alice", queries, false)
	must(err)
	if len(result.Changed) != 5 || len(result.Unchanged) != 1 || len(result.Missing) != 1 || len(result.Overflow) != 3 || result.Count != 5 {
		t.Fatalf("batch: %+v", result)
	}
	// Removing a hidden character must not go through the public card search.
	_, err = db.Exec(`UPDATE gacha_characters SET enabled=false,auto_publish_blocked=true,archived_at=now() WHERE id=$1`, ids[0])
	must(err)
	_, err = db.Exec(`UPDATE gacha_assets SET status='rejected' WHERE character_id=$1`, ids[0])
	must(err)
	result, err = store.UpdateWishlist(ctx, "wishlist-regression", "alice", []string{"Wishlist regression 0"}, true)
	must(err)
	if len(result.Changed) != 1 || result.Changed[0].ID != ids[0] {
		t.Fatalf("disabled removal: %+v", result)
	}
	result, err = store.UpdateWishlist(ctx, "wishlist-regression", "alice", []string{"WishAlias3"}, true)
	must(err)
	if len(result.Changed) != 1 || result.Changed[0].ID != ids[3] {
		t.Fatalf("alias removal: %+v", result)
	}
	// Ambiguous removal never picks an arbitrary character.
	result, err = store.UpdateWishlist(ctx, "wishlist-regression", "alice", []string{"Wishlist regression"}, true)
	must(err)
	if len(result.Ambiguous) != 1 || len(result.Changed) != 0 {
		t.Fatalf("ambiguous removal: %+v", result)
	}
	// Cross-user and cross-guild removals cannot affect Alice.
	result, err = store.UpdateWishlist(ctx, "other-wishlist-guild", "alice", []string{fmt.Sprint(ids[1])}, true)
	must(err)
	if len(result.Changed) != 0 {
		t.Fatal("cross-guild deletion")
	}
	token, count, err := store.PrepareWishlistClear(ctx, "wishlist-regression", "channel", "alice")
	must(err)
	if count != 3 || token == "" {
		t.Fatal(count, token)
	}
	for _, scope := range []struct{ guild, channel, user string }{{"wishlist-regression", "channel", "bob"}, {"other-wishlist-guild", "channel", "alice"}, {"wishlist-regression", "other-channel", "alice"}} {
		if _, err = store.ResolveWishlistClear(ctx, scope.guild, scope.channel, scope.user, token, true); err == nil {
			t.Fatal("foreign confirmation accepted")
		}
	}
	removed, err := store.ResolveWishlistClear(ctx, "wishlist-regression", "channel", "alice", token, false)
	must(err)
	if removed != 0 {
		t.Fatal("cancel deleted wishes")
	}
	if _, err = store.ResolveWishlistClear(ctx, "wishlist-regression", "channel", "alice", token, true); err == nil {
		t.Fatal("cancelled confirmation replay")
	}
	token, _, err = store.PrepareWishlistClear(ctx, "wishlist-regression", "channel", "alice")
	must(err)
	_, err = db.Exec(`UPDATE gacha_wishlist_confirmations SET expires_at=now()-interval '1 second' WHERE token=$1`, token)
	must(err)
	if _, err = store.ResolveWishlistClear(ctx, "wishlist-regression", "channel", "alice", token, true); err == nil {
		t.Fatal("expired confirmation accepted")
	}
	token, _, err = store.PrepareWishlistClear(ctx, "wishlist-regression", "channel", "alice")
	must(err)
	removed, err = store.ResolveWishlistClear(ctx, "wishlist-regression", "channel", "alice", token, true)
	must(err)
	if removed != 3 {
		t.Fatalf("clear removed %d, want 3", removed)
	}
	must(store.Wish(ctx, "wishlist-regression", "alice", ids[5], false))
	if _, err = store.ResolveWishlistClear(ctx, "wishlist-regression", "channel", "alice", token, true); err == nil {
		t.Fatal("replay accepted after new additions")
	}
	var remaining int
	must(db.QueryRow(`SELECT count(*) FROM gacha_wishes WHERE guild_id='wishlist-regression' AND user_id='alice'`).Scan(&remaining))
	if remaining != 1 {
		t.Fatal("replay deleted new wishes")
	}

	// Resolve a whole batch of canonical names without falling back to aliases.
	names := []string{"Wishlist regression 3", "Wishlist regression 4", "Wishlist regression 5", "Wishlist regression 6", "Wishlist regression 7"}
	result, err = store.UpdateWishlist(ctx, "wishlist-names", "alice", names, false)
	must(err)
	if len(result.Changed) != 5 {
		t.Fatalf("name batch: %+v", result)
	}
	// An exact canonical name takes precedence over another character's alias.
	_, err = db.Exec(`UPDATE gacha_characters SET aliases='["Wishlist regression 3"]'::jsonb WHERE id=$1`, ids[6])
	must(err)
	result, err = store.UpdateWishlist(ctx, "wishlist-exact", "alice", names[:1], false)
	must(err)
	if len(result.Changed) != 1 || result.Changed[0].ID != ids[3] {
		t.Fatalf("canonical precedence: %+v", result)
	}
	// Duplicate canonical names are still ambiguous and require an ID.
	_, err = db.Exec(`UPDATE gacha_characters SET name=$2 WHERE id=$1`, ids[7], names[0])
	must(err)
	result, err = store.UpdateWishlist(ctx, "wishlist-ambiguous", "alice", names[:1], false)
	must(err)
	if len(result.Ambiguous) != 1 || len(result.Changed) != 0 {
		t.Fatalf("duplicate canonical names: %+v", result)
	}
	// Separate batches race for the same five slots.
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for _, id := range ids {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			_, err := store.UpdateWishlist(ctx, "wishlist-regression", "concurrent", []string{fmt.Sprint(id)}, false)
			errs <- err
		}(id)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		must(err)
	}
	must(db.QueryRow(`SELECT count(*) FROM gacha_wishes WHERE guild_id='wishlist-regression' AND user_id='concurrent'`).Scan(&remaining))
	if remaining != 5 {
		t.Fatalf("concurrent batches exceeded limit: %d", remaining)
	}
}

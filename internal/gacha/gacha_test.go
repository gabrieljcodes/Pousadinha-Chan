package gacha

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestAniListPaginationAndErrors(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var req struct{ Variables struct{ ID, Page int } }
		if e := json.NewDecoder(r.Body).Decode(&req); e != nil {
			t.Error(e)
		}
		if req.Variables.ID != 17 {
			t.Error("wrong ID")
		}
		if calls == 1 {
			w.Write([]byte(`{"data":{"Character":{"id":17,"name":{"full":"Example"},"favourites":42,"media":{"pageInfo":{"hasNextPage":true},"edges":[{"characterRole":"MAIN","node":{"id":1,"type":"ANIME","title":{"romaji":"Work"},"genres":["Action"],"studios":{"nodes":[{"name":"Studio"}]}}}]}}}}`))
		} else {
			w.Write([]byte(`{"data":{"Character":{"id":17,"name":{"full":"Example"},"favourites":42,"media":{"pageInfo":{"hasNextPage":false},"edges":[]}}}}`))
		}
	}))
	defer srv.Close()
	a := NewAniList()
	a.Endpoint = srv.URL
	a.Client = srv.Client()
	c, e := a.Character(context.Background(), 17)
	if e != nil || calls != 2 || c.Name != "Example" || len(c.Works) != 1 || c.Works[0].Studios[0] != "Studio" {
		t.Fatalf("%+v %v calls=%d", c, e, calls)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e = a.Character(ctx, 17); e == nil {
		t.Fatal("cancellation ignored")
	}
}
func TestConfig(t *testing.T) {
	t.Setenv("GACHA_ENABLED", "true")
	t.Setenv("GACHA_PUBLIC_URL", "https://pousadinha.com")
	t.Setenv("GACHA_ROLLS_PER_HOUR", "10")
	t.Setenv("GACHA_CLAIM_HOURS", "3")
	if _, e := LoadConfig(); e != nil {
		t.Fatal(e)
	}
	for _, u := range []string{"http://pousadinha.com", "https://user:pass@example.com", "https://example.com/x", "https://example.com?x=1"} {
		t.Setenv("GACHA_PUBLIC_URL", u)
		if _, e := LoadConfig(); e == nil {
			t.Errorf("accepted %s", u)
		}
	}
}
func TestRenderStillAndAnimation(t *testing.T) {
	if _, e := exec.LookPath("ffmpeg"); e != nil {
		t.Skip("ffmpeg required")
	}
	dir := t.TempDir()
	input := filepath.Join(dir, "still.png")
	f, e := os.Create(input)
	if e != nil {
		t.Fatal(e)
	}
	img := image.NewRGBA(image.Rect(0, 0, 80, 120))
	for y := 0; y < 120; y++ {
		for x := 0; x < 80; x++ {
			img.Set(x, y, color.RGBA{200, 60, 40, 255})
		}
	}
	if e = png.Encode(f, img); e != nil {
		t.Fatal(e)
	}
	f.Close()
	output := filepath.Join(dir, "card.png")
	if e = Render(context.Background(), input, output, false); e != nil {
		t.Fatal(e)
	}
	f, _ = os.Open(output)
	render, e := png.Decode(f)
	f.Close()
	if e != nil {
		t.Fatal(e)
	}
	if render.Bounds().Dx() != 420 || render.Bounds().Dy() != 600 {
		t.Fatal(render.Bounds())
	}
	r, g, b, _ := render.At(3, 3).RGBA()
	if r>>8 != 197 || g>>8 != 166 || b>>8 != 107 {
		t.Fatalf("border %d %d %d", r>>8, g>>8, b>>8)
	}
	pal := color.Palette{color.Black, color.White}
	one := image.NewPaletted(image.Rect(0, 0, 40, 60), pal)
	two := image.NewPaletted(one.Rect, pal)
	for i := range two.Pix {
		two.Pix[i] = 1
	}
	input = filepath.Join(dir, "motion.gif")
	f, _ = os.Create(input)
	e = gif.EncodeAll(f, &gif.GIF{Image: []*image.Paletted{one, two}, Delay: []int{20, 20}, LoopCount: 0})
	f.Close()
	if e != nil {
		t.Fatal(e)
	}
	output = filepath.Join(dir, "card.gif")
	if e = Render(context.Background(), input, output, true); e != nil {
		t.Fatal(e)
	}
	f, _ = os.Open(output)
	anim, e := gif.DecodeAll(f)
	f.Close()
	if e != nil {
		t.Fatal(e)
	}
	if len(anim.Image) < 2 || anim.Config.Width != 420 || anim.Config.Height != 600 {
		t.Fatal("animation lost")
	}
}
func TestPostgresGame(t *testing.T) {
	dsn := os.Getenv("GACHA_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set GACHA_TEST_DATABASE_URL to a disposable PostgreSQL database")
	}
	db, e := sql.Open("pgx", dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	ctx := context.Background()
	// Every test run owns a separate schema.
	if !strings.Contains(dsn, "gacha_test") {
		t.Fatal("test database must contain gacha_test in its DSN")
	}
	schema := "gacha_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, e = db.Exec(`CREATE SCHEMA ` + schema); e != nil {
		t.Fatal(e)
	}
	defer db.Exec(`DROP SCHEMA ` + schema + ` CASCADE`)
	u, e := url.Parse(dsn)
	if e != nil {
		t.Fatal(e)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	scoped, e := sql.Open("pgx", u.String())
	if e != nil {
		t.Fatal(e)
	}
	defer scoped.Close()
	if _, e = scoped.Exec(`CREATE TABLE users(id TEXT PRIMARY KEY,balance BIGINT DEFAULT 0)`); e != nil {
		t.Fatal(e)
	}
	db = scoped
	store := &Store{DB: db, Config: Config{RollsPerHour: 2, ClaimHours: 3, MediaDir: t.TempDir(), PublicURL: "https://example.com"}}
	if e = store.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	if e = store.Migrate(ctx); e != nil {
		t.Fatal("migration not idempotent", e)
	}
	character := Character{Provider: "anilist", ExternalID: "1", Name: "Hero", Favourites: 100, Works: []Work{{ExternalID: "1", Kind: "anime", Title: "Work", Genres: []string{"Action"}}}}
	id, e := store.Import(ctx, character)
	if e != nil {
		t.Fatal(e)
	}
	again, e := store.Import(ctx, character)
	if e != nil || again != id {
		t.Fatal("duplicate import", e)
	}
	if _, e = store.Roll(ctx, "guild", "channel", "alice", "empty"); e != ErrEmpty {
		t.Fatal(e)
	}
	var count int
	if e = db.QueryRow(`SELECT count(*) FROM gacha_rolls`).Scan(&count); e != nil || count != 0 {
		t.Fatal("empty roll persisted")
	}
	_, e = db.Exec(`UPDATE gacha_characters SET enabled=true`)
	if e != nil {
		t.Fatal(e)
	}
	_, e = db.Exec(`INSERT INTO gacha_assets(character_id,provider,external_id,source_url,sha256,path,media_type,status) VALUES($1,'test','1','https://example.com','hash','work/hero/photo1/hash.png','image/png','approved')`, id)
	if e != nil {
		t.Fatal(e)
	}
	roll, e := store.Roll(ctx, "guild", "channel", "alice", "r1")
	if e != nil {
		t.Fatal(e)
	}
	repeat, e := store.Roll(ctx, "guild", "channel", "alice", "r1")
	if e != nil || repeat.ID != roll.ID {
		t.Fatal("request replay failed", e)
	}
	if e = store.Claim(ctx, "other-guild", "channel", "eve", roll.ID); e != ErrClaim {
		t.Fatal("cross guild claim", e)
	}
	if e = store.Claim(ctx, "guild", "other-channel", "eve", roll.ID); e != ErrClaim {
		t.Fatal("cross channel claim", e)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, u := range []string{"bob", "carol"} {
		wg.Add(1)
		go func(user string) { defer wg.Done(); results <- store.Claim(ctx, "guild", "channel", user, roll.ID) }(u)
	}
	wg.Wait()
	close(results)
	wins := 0
	for e := range results {
		if e == nil {
			wins++
		} else if e != ErrClaim {
			t.Fatal(e)
		}
	}
	if wins != 1 {
		t.Fatalf("claim winners=%d", wins)
	}
	if _, e = store.Roll(ctx, "guild", "channel", "alice", "r2"); e != nil {
		t.Fatal(e)
	}
	if _, e = store.Roll(ctx, "guild", "channel", "alice", "r3"); e != ErrLimit {
		t.Fatal("roll limit", e)
	}
	if _, e = (&Store{DB: db, Config: store.Config}).Roll(ctx, "guild", "channel", "alice", "r4"); e != ErrLimit {
		t.Fatal("restart reset limits", e)
	}
	var winner string
	db.QueryRow(`SELECT user_id FROM gacha_collection LIMIT 1`).Scan(&winner)
	if e = store.Claim(ctx, "guild", "channel", winner, roll.ID); e != ErrClaim {
		t.Fatal("cooldown not enforced")
	}
	other, e := store.Roll(ctx, "other", "channel", "alice", "r5")
	if e != nil {
		t.Fatal(e)
	}
	if e = store.Claim(ctx, "other", "channel", "alice", other.ID); e != nil {
		t.Fatal("guild isolation", e)
	}
	expired, e := store.Roll(ctx, "expired", "channel", "alice", "r6")
	if e != nil {
		t.Fatal(e)
	}
	db.Exec(`UPDATE gacha_rolls SET expires_at=now()-interval '1 second' WHERE id=$1`, expired.ID)
	if e = store.Claim(ctx, "expired", "channel", "bob", expired.ID); e != ErrClaim {
		t.Fatal("expired claim", e)
	}

	refund, e := store.Roll(ctx, "refund", "channel", "alice", "refund1")
	if e != nil {
		t.Fatal(e)
	}
	if e = store.CancelDelivery(ctx, "refund1"); e != nil {
		t.Fatal(e)
	}
	if e = store.CancelDelivery(ctx, "refund1"); e != nil {
		t.Fatal(e)
	}
	if e = db.QueryRow(`SELECT rolls_used FROM gacha_players WHERE guild_id='refund' AND user_id='alice'`).Scan(&count); e != nil || count != 0 {
		t.Fatal("refund not idempotent", count, e)
	}
	if e = store.Claim(ctx, "refund", "channel", "bob", refund.ID); e != ErrClaim {
		t.Fatal("refunded roll claimable", e)
	}
	if e = store.Wish(ctx, "guild", "alice", id, false); e != nil {
		t.Fatal(e)
	}
	if e = store.Wish(ctx, "guild", "alice", 999999, false); e == nil {
		t.Fatal("nonexistent wish accepted")
	}
	results = make(chan error, 8)
	for n := 0; n < 8; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_, e := store.Roll(ctx, "parallel", "channel", "alice", fmt.Sprintf("parallel-%d", n))
			results <- e
		}(n)
	}
	wg.Wait()
	close(results)
	wins = 0
	for e := range results {
		if e == nil {
			wins++
		} else if e != ErrLimit {
			t.Fatal(e)
		}
	}
	if wins != 2 {
		t.Fatalf("parallel roll quota: %d", wins)
	}
	// Approved assets only, even when a pending asset physically exists on disk.
	path := filepath.Join(store.Config.MediaDir, "work/hero/photo1")
	os.MkdirAll(path, 0750)
	os.WriteFile(filepath.Join(path, "hash.png"), []byte("image"), 0600)
	request := httptest.NewRequest("GET", "/work/hero/photo1/hash.png", nil)
	rec := httptest.NewRecorder()
	store.ServeHTTP(rec, request)
	if rec.Code != 200 {
		t.Fatal(rec.Code)
	}
	db.Exec(`UPDATE gacha_assets SET status='rejected'`)
	rec = httptest.NewRecorder()
	store.ServeHTTP(rec, request)
	if rec.Code != 404 {
		t.Fatal("rejected asset served")
	}
	t.Run("batch", func(t *testing.T) { testBatch(t, store); testBatchCache(t, store) })

}

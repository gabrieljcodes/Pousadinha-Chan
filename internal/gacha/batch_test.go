package gacha

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type fakeBatchProvider struct {
	failPage bool
	calls    []int
}

func (f *fakeBatchProvider) Character(context.Context, int64) (Character, error) {
	panic("injected importer expected")
}
func (f *fakeBatchProvider) CharacterIDs(_ context.Context, page int) ([]int64, bool, error) {
	f.calls = append(f.calls, page)
	if page == 1 {
		return []int64{100, 101}, true, nil
	}
	if f.failPage {
		return nil, false, fmt.Errorf("temporary discovery failure")
	}
	return []int64{101, 102, 103, 104}, false, nil
}
func testBatch(t *testing.T, s *Store) {
	ctx := context.Background()
	p := &fakeBatchProvider{failPage: true}
	counts := map[int64]int{}
	importer := func(ctx context.Context, id int64) (int64, error) {
		counts[id]++
		return s.Import(ctx, Character{Provider: "anilist", ExternalID: fmt.Sprint(id), Name: fmt.Sprintf("Hero %d", id)})
	}
	opt := BatchOptions{Name: "resume", Target: 3}
	if e := s.runBatch(ctx, p, opt, io.Discard, importer); e == nil {
		t.Fatal("discovery failure ignored")
	}
	stats, e := s.BatchStats(ctx, opt.Name)
	if e != nil || stats.Imported != 2 {
		t.Fatal(stats, e)
	}
	p.failPage = false
	if e = s.runBatch(ctx, p, opt, io.Discard, importer); e != nil {
		t.Fatal(e)
	}
	stats, e = s.BatchStats(ctx, opt.Name)
	if e != nil || stats.Imported != 3 || stats.Pending != 2 {
		t.Fatal(stats, e)
	}
	if counts[100] != 1 || counts[101] != 1 || counts[102] != 1 || counts[103] != 0 {
		t.Fatal("resume repeated or over-imported", counts)
	}
	if e = s.runBatch(ctx, p, opt, io.Discard, importer); e != nil {
		t.Fatal(e)
	}
	if counts[100] != 1 {
		t.Fatal("completed job repeated")
	}
	opt.AutoApprove = true
	if e = s.runBatch(ctx, p, opt, io.Discard, importer); e == nil {
		t.Fatal("policy changed on resume")
	}
	// A live lease rejects a second worker without making any provider requests.
	_, e = s.DB.Exec(`INSERT INTO gacha_import_leases(provider,owner,expires_at) VALUES('anilist','other',now()+interval '5 minutes')`)
	if e != nil {
		t.Fatal(e)
	}
	opt.AutoApprove = false
	if e = s.runBatch(ctx, p, opt, io.Discard, importer); e == nil {
		t.Fatal("concurrent worker allowed")
	}
	s.DB.Exec(`DELETE FROM gacha_import_leases`)
	// Failed imports remain retryable; skipped non-anime characters do not count toward target.
	fail := true
	opt = BatchOptions{Name: "failures", Target: 4}
	retryImport := func(ctx context.Context, id int64) (int64, error) {
		if id == 100 {
			return 0, errNotAnime
		}
		if id == 101 && fail {
			return 0, errors.New("download interrupted")
		}
		return importer(ctx, id)
	}
	if e = s.runBatch(ctx, p, opt, io.Discard, retryImport); e == nil {
		t.Fatal("exhausted source reported success")
	}
	stats, _ = s.BatchStats(ctx, opt.Name)
	if stats.Imported != 3 || stats.Skipped != 1 || stats.Failed != 1 {
		t.Fatal(stats)
	}
	fail = false
	opt.RetryFailed = true
	if e = s.runBatch(ctx, p, opt, io.Discard, retryImport); e != nil {
		t.Fatal(e)
	}
	// Approval cannot promote booru assets or overturn a manual rejection/disable.
	if e = os.WriteFile(filepath.Join(s.Config.MediaDir, "portrait.png"), []byte("test portrait"), 0600); e != nil {
		t.Fatal(e)
	}
	var cid, aid int64
	if e = s.DB.QueryRow(`SELECT character_id FROM gacha_character_sources WHERE provider='anilist' AND external_id='100'`).Scan(&cid); e != nil {
		t.Fatal(e)
	}
	e = s.DB.QueryRow(`INSERT INTO gacha_assets(character_id,provider,external_id,source_url,sha256,path,media_type) VALUES($1,'anilist','100','https://anilist.co/character/100','portrait','portrait.png','image/png') RETURNING id`, cid).Scan(&aid)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.publishPortrait(ctx, cid, aid); e != nil {
		t.Fatal(e)
	}
	var enabled bool
	var status, reviewer string
	s.DB.QueryRow(`SELECT enabled FROM gacha_characters WHERE id=$1`, cid).Scan(&enabled)
	s.DB.QueryRow(`SELECT status,reviewed_by FROM gacha_assets WHERE id=$1`, aid).Scan(&status, &reviewer)
	if !enabled || status != "approved" || reviewer != "auto:anilist-portrait:v1" {
		t.Fatal(enabled, status, reviewer)
	}
	s.DB.Exec(`UPDATE gacha_characters SET enabled=false,auto_publish_blocked=true WHERE id=$1`, cid)
	if e = s.publishPortrait(ctx, cid, aid); e != nil {
		t.Fatal(e)
	}
	s.DB.QueryRow(`SELECT enabled FROM gacha_characters WHERE id=$1`, cid).Scan(&enabled)
	if enabled {
		t.Fatal("manual disable lost")
	}
	s.DB.Exec(`UPDATE gacha_assets SET status='rejected' WHERE id=$1`, aid)
	if e = s.publishPortrait(ctx, cid, aid); e == nil {
		t.Fatal("manual rejection lost")
	}
	s.DB.Exec(`UPDATE gacha_assets SET status='pending',provider='gelbooru' WHERE id=$1`, aid)
	if e = s.publishPortrait(ctx, cid, aid); e == nil {
		t.Fatal("booru automatically approved")
	}
}
func TestDiscoveryAndRateHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		fmt.Fprint(w, `{"data":{"Page":{"pageInfo":{"hasNextPage":true},"characters":[{"id":1},{"id":7}]}}}`)
	}))
	defer srv.Close()
	a := NewAniList()
	a.Endpoint = srv.URL
	a.Client = srv.Client()
	ids, more, e := a.CharacterIDs(context.Background(), 1)
	if e != nil || !more || len(ids) != 2 || ids[1] != 7 {
		t.Fatal(ids, more, e)
	}
	if time.Until(a.next) < 59*time.Second {
		t.Fatal("exhausted rate budget not honored")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, e = a.CharacterIDs(ctx, 2); e == nil {
		t.Fatal("wait cannot be canceled")
	}
}

func TestAniListDisabledResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		fmt.Fprint(w, `{"errors":[{"message":"The AniList API has been temporarily disabled due to severe stability issues.","status":403}],"data":null}`)
	}))
	defer srv.Close()
	a := NewAniList()
	a.Endpoint = srv.URL
	a.Client = srv.Client()
	_, _, e := a.CharacterIDs(context.Background(), 1)
	var apiError *AniListHTTPError
	if !errors.As(e, &apiError) || apiError.Status != 403 || apiError.Message == "" {
		t.Fatalf("source explanation lost: %v", e)
	}
}

type cachedBatchProvider struct{ calls int }

func (p *cachedBatchProvider) Character(context.Context, int64) (Character, error) {
	return Character{}, fmt.Errorf("unexpected individual fetch")
}
func (p *cachedBatchProvider) CharacterIDs(context.Context, int) ([]int64, bool, error) {
	return []int64{200, 201}, false, nil
}
func (p *cachedBatchProvider) Characters(_ context.Context, ids []int64) ([]Character, error) {
	p.calls++
	out := []Character{}
	for _, id := range ids {
		out = append(out, Character{Provider: "anilist", ExternalID: fmt.Sprint(id), Name: fmt.Sprint(id)})
	}
	return out, nil
}
func testBatchCache(t *testing.T, s *Store) {
	ctx := context.Background()
	p := &cachedBatchProvider{}
	fail := true
	importer := func(ctx context.Context, id int64) (int64, error) {
		var payload []byte
		if e := s.DB.QueryRowContext(ctx, `SELECT payload FROM gacha_import_items WHERE job='bulk-cache' AND external_id=$1`, id).Scan(&payload); e != nil {
			t.Fatal(e)
		}
		if len(payload) == 0 {
			t.Fatal("metadata not persisted before portrait work")
		}
		if fail && id == 201 {
			return 0, fmt.Errorf("portrait download interrupted")
		}
		var c Character
		if e := json.Unmarshal(payload, &c); e != nil {
			t.Fatal(e)
		}
		return s.Import(ctx, c)
	}
	opt := BatchOptions{Name: "bulk-cache", Target: 2}
	if e := s.runBatch(ctx, p, opt, io.Discard, importer); e == nil {
		t.Fatal("expected incomplete job")
	}
	if p.calls != 1 {
		t.Fatal("not batched", p.calls)
	}
	fail = false
	opt.RetryFailed = true
	if e := s.runBatch(ctx, p, opt, io.Discard, importer); e != nil {
		t.Fatal(e)
	}
	if p.calls != 1 {
		t.Fatal("refetched persisted metadata on resume", p.calls)
	}
	stats, e := s.BatchStats(ctx, opt.Name)
	if e != nil || stats.Imported != 2 {
		t.Fatal(stats, e)
	}
}
func TestCharactersBatchContinuesOnlyOverflow(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Referer") != "https://anilist.co/" || r.Header.Get("Origin") != "https://anilist.co" {
			t.Error("client must send Referer and Origin headers")
		}
		var req struct {
			Variables struct {
				IDs           []int64
				ID, MediaPage int
			}
		}
		if e := json.NewDecoder(r.Body).Decode(&req); e != nil {
			t.Fatal(e)
		}
		if calls == 1 {
			if len(req.Variables.IDs) != 2 || req.Variables.MediaPage != 1 {
				t.Error("invalid batch arguments")
			}
			fmt.Fprint(w, `{"data":{"Page":{"characters":[{"id":17,"name":{"full":"A"},"media":{"pageInfo":{"hasNextPage":true},"edges":[{"node":{"id":1,"type":"ANIME","title":{"romaji":"One"}}}]}},{"id":18,"name":{"full":"B"},"media":{"pageInfo":{"hasNextPage":false},"edges":[]}}]}}}`)
		} else {
			if req.Variables.ID != 17 || req.Variables.MediaPage != 2 {
				t.Error("refetched first page or fetched non-overflowing character")
			}
			fmt.Fprint(w, `{"data":{"Character":{"id":17,"media":{"pageInfo":{"hasNextPage":false},"edges":[{"node":{"id":2,"type":"ANIME","title":{"romaji":"Two"}}}]}}}}`)
		}
	}))
	defer srv.Close()
	a := NewAniList()
	a.Endpoint = srv.URL
	a.Client = srv.Client()
	characters, e := a.Characters(context.Background(), []int64{17, 18})
	if e != nil {
		t.Fatal(e)
	}
	if calls != 2 || len(characters) != 2 || len(characters[0].Works) != 2 || characters[0].Name != "A" {
		t.Fatal(calls, characters)
	}
	if _, e = a.Characters(context.Background(), make([]int64, 51)); e == nil {
		t.Fatal("oversized batch accepted")
	}
}

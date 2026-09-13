package gacha

import (
	"context"
	"os"
	"sync"
	"testing"
)

func testRuntime(t *testing.T, s *Store) {
	ctx := context.Background()
	assertCounts := func() {
		t.Helper()
		var mismatch int
		err := s.DB.QueryRow(`SELECT count(*) FROM (SELECT guild_id,count(*) n FROM gacha_collection GROUP BY guild_id) c FULL JOIN gacha_guild_stats s USING(guild_id) WHERE COALESCE(c.n,0)<>COALESCE(s.claimed,0)`).Scan(&mismatch)
		if err != nil || mismatch != 0 {
			t.Fatalf("derived counts mismatch=%d: %v", mismatch, err)
		}
	}
	assertCounts()
	// Existing ownership must be preserved when installing the derived table.
	if _, err := s.DB.Exec(`DROP TABLE gacha_guild_stats`); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	assertCounts()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`UPDATE gacha_collection SET guild_id='runtime-moved-'||guild_id`); err != nil {
		t.Fatal(err)
	}
	var mismatch int
	if err = tx.QueryRow(`SELECT count(*) FROM (SELECT guild_id,count(*) n FROM gacha_collection GROUP BY guild_id) c FULL JOIN gacha_guild_stats s USING(guild_id) WHERE COALESCE(c.n,0)<>COALESCE(s.claimed,0)`).Scan(&mismatch); err != nil || mismatch != 0 {
		t.Fatalf("move count mismatch: %d %v", mismatch, err)
	}
	if _, err = tx.Exec(`TRUNCATE gacha_collection`); err != nil {
		t.Fatal(err)
	}
	var total int
	if err = tx.QueryRow(`SELECT count(*) FROM gacha_guild_stats`).Scan(&total); err != nil || total != 0 {
		t.Fatalf("truncate counter: %d %v", total, err)
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	assertCounts()

	first, err := s.catalogPools(ctx, true)
	if err != nil || !validPools(first) {
		t.Fatalf("snapshot: %v", err)
	}
	// Rolled-back catalog writes must not invalidate committed snapshots.
	tx, err = s.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`UPDATE gacha_characters SET enabled=false`); err != nil {
		t.Fatal(err)
	}
	tx.Rollback()
	same, err := s.catalogPools(ctx, true)
	if err != nil || same != first {
		t.Fatalf("rollback changed revision: %v", err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			snapshot, e := s.catalogPools(ctx, false)
			if e != nil || snapshot != first {
				t.Errorf("concurrent cache read: %v", e)
			}
		}()
	}
	wg.Wait()

	if address := os.Getenv("GACHA_TEST_VALKEY_URL"); address != "" {
		a := &Store{DB: s.DB, Config: s.Config}
		if err := a.ConnectPoolCache(address); err != nil {
			t.Fatal(err)
		}
		defer a.ClosePoolCache()
		shared, err := a.catalogPools(ctx, true)
		if err != nil {
			t.Fatal(err)
		}
		key := "gacha:pools:v1:" + shared.Namespace
		defer a.runtime.client.Do(ctx, a.runtime.client.B().Del().Key(key).Build())
		if err := a.runtime.client.Do(ctx, a.runtime.client.B().Get().Key(key).Build()).Error(); err != nil {
			t.Fatalf("shared snapshot absent: %v", err)
		}
		b := &Store{DB: s.DB, Config: s.Config}
		if err := b.ConnectPoolCache(address); err != nil {
			t.Fatal(err)
		}
		defer b.ClosePoolCache()
		loaded, err := b.catalogPools(ctx, true)
		if err != nil || loaded.Revision != shared.Revision || !validPools(loaded) {
			t.Fatalf("shared snapshot invalid: %v", err)
		}
		if stats := b.PoolCacheStats(); stats.SharedHits != 1 || stats.Builds != 0 {
			t.Fatalf("shared snapshot not reused: %+v", stats)
		}
		// Corrupted shared data must rebuild from PostgreSQL.
		if err := a.runtime.client.Do(ctx, a.runtime.client.B().Set().Key(key).Value(`{"revision":-1}`).Build()).Error(); err != nil {
			t.Fatal(err)
		}
		b.runtime.snapshot.Store(nil)
		if _, err = b.catalogPools(ctx, true); err != nil {
			t.Fatalf("corrupt cache blocked rolls: %v", err)
		}
		// A failed cache connection cannot prevent a cold PostgreSQL rebuild.
		b.runtime.client.Close()
		b.runtime.snapshot.Store(nil)
		if _, err = b.catalogPools(ctx, true); err != nil {
			t.Fatalf("cache outage blocked rebuild: %v", err)
		}
	}
}

func BenchmarkSampleMillion(b *testing.B) {
	ids := make([]int64, 1_000_000)
	for i := range ids {
		ids[i] = int64(i + 1)
	}
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			id, err := sampleID(ids)
			if err != nil || id < 1 || id > 1_000_000 {
				b.Fatal("invalid sample")
			}
		}
	})
}

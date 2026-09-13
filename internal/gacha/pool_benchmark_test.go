package gacha

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Opt-in: creates one million synthetic characters in an isolated test schema.
// It measures database work on localhost, not Discord throughput or tail latency.
func BenchmarkPostgresCatalog(b *testing.B) {
	dsn := os.Getenv("GACHA_BENCH_DATABASE_URL")
	if dsn == "" {
		b.Skip("set GACHA_BENCH_DATABASE_URL to a disposable gacha_test database")
	}
	if !strings.Contains(dsn, "gacha_test") {
		b.Fatal("benchmark requires gacha_test database")
	}
	root, err := sql.Open("pgx", dsn)
	if err != nil {
		b.Fatal(err)
	}
	defer root.Close()
	schema := "gacha_bench_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = root.Exec(`CREATE SCHEMA ` + schema); err != nil {
		b.Fatal(err)
	}
	defer root.Exec(`DROP SCHEMA ` + schema + ` CASCADE`)
	u, err := url.Parse(dsn)
	if err != nil {
		b.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := sql.Open("pgx", u.String())
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(15)
	db.SetMaxIdleConns(5)
	ctx := context.Background()
	if _, err = db.Exec(`CREATE TABLE users(id TEXT PRIMARY KEY,balance BIGINT DEFAULT 0);
 CREATE TABLE guild_members(guild_id TEXT NOT NULL,user_id TEXT NOT NULL REFERENCES users(id),balance BIGINT NOT NULL DEFAULT 0 CHECK(balance>=0),updated_at TIMESTAMPTZ DEFAULT now(),PRIMARY KEY(guild_id,user_id))`); err != nil {
		b.Fatal(err)
	}
	s := &Store{DB: db, Config: Config{RollsPerHour: 1_000_000_000, ClaimHours: 3}}
	if err = s.Migrate(ctx); err != nil {
		b.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO gacha_characters(name,gender,enabled) SELECT 'Character '||n,CASE WHEN n%2=0 THEN 'female' ELSE 'male' END,true FROM generate_series(1,1000000) n;
 INSERT INTO gacha_works(provider,external_id,kind,title,source_url) VALUES('benchmark','1','anime','Synthetic work','https://example.com');
 INSERT INTO gacha_character_works(character_id,work_id) SELECT id,1 FROM gacha_characters;
 INSERT INTO gacha_assets(character_id,provider,external_id,source_url,sha256,path,media_type,status) SELECT id,'benchmark',id::text,'https://example.com',id::text,'benchmark/'||id||'.png','image/png','approved' FROM gacha_characters;
 ANALYZE gacha_characters; ANALYZE gacha_assets; ANALYZE gacha_character_works; ANALYZE gacha_works;`); err != nil {
		b.Fatal(err)
	}
	started := time.Now()
	snapshot, err := s.catalogPools(ctx, true)
	if err != nil {
		b.Fatal(err)
	}
	b.Logf("snapshot build: %s, eligible=%d", time.Since(started), len(snapshot.IDs["roll"]))
	if len(snapshot.IDs["roll"]) != 1_000_000 {
		b.Fatal("incomplete snapshot")
	}
	const eligible = `c.enabled AND EXISTS(SELECT 1 FROM gacha_assets a WHERE a.character_id=c.id AND a.status='approved')`
	b.Run("PreviousCountOffset", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			var count int64
			if err := db.QueryRowContext(ctx, `SELECT count(*) FROM gacha_characters c WHERE `+eligible).Scan(&count); err != nil {
				b.Fatal(err)
			}
			id, err := sampleID(snapshot.IDs["roll"])
			if err != nil {
				b.Fatal(err)
			}
			if _, err = scanCard(db.QueryRowContext(ctx, cardSelect+` WHERE `+eligible+` ORDER BY c.id OFFSET $1 LIMIT 1`, (id-1)%count)); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("CachedIDLookup", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			id, err := sampleID(snapshot.IDs["roll"])
			if err != nil {
				b.Fatal(err)
			}
			if _, err = scanCard(db.QueryRowContext(ctx, cardSelect+` WHERE c.id=$1 AND `+eligible, id)); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("RollTransaction", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := s.Roll(ctx, "bench", "channel", "user", uuid.NewString()); err != nil {
				b.Fatal(err)
			}
		}
	})
}

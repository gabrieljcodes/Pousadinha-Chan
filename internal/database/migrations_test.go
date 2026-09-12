package database

import (
	"bot/migrations"
	"database/sql"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func migrationTestDB(t *testing.T) *PostgresDatabase {
	t.Helper()
	dsn := os.Getenv("GACHA_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set GACHA_TEST_DATABASE_URL to a disposable PostgreSQL database")
	}
	u, err := url.Parse(dsn)
	if err != nil || !strings.Contains(strings.TrimPrefix(u.Path, "/"), "gacha_test") {
		t.Fatal("migration tests require a disposable gacha_test database")
	}
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Close() })
	schema := "migration_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = admin.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec("DROP SCHEMA " + schema + " CASCADE"); err != nil {
			t.Error(err)
		}
	})
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := sql.Open("pgx", u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return &PostgresDatabase{db: db}
}

func TestGuildMigrationPreservesEconomy(t *testing.T) {
	p := migrationTestDB(t)
	exec := func(q string) {
		t.Helper()
		if _, err := p.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	exec(`CREATE TABLE users(id TEXT PRIMARY KEY,balance BIGINT,last_daily TIMESTAMPTZ,daily_streak INT,max_daily_streak INT);
 CREATE TABLE stock_investments(user_id TEXT,ticker TEXT,shares NUMERIC,PRIMARY KEY(user_id,ticker));
 INSERT INTO users VALUES ('legacy',9000,now(),5,10);
 INSERT INTO stock_investments VALUES ('legacy','ABC',2.5);`)
	// A missing dependency must roll back the entire migration, including new columns.
	if err := p.migrateGuildEconomy(); err == nil {
		t.Fatal("expected missing crypto table error")
	}
	var exists bool
	if err := p.db.QueryRow(`SELECT to_regclass('guild_members') IS NOT NULL`).Scan(&exists); err != nil || exists {
		t.Fatalf("partial migration persisted: %v, %v", exists, err)
	}
	exec(`CREATE TABLE crypto_investments(user_id TEXT,symbol TEXT,coins NUMERIC,PRIMARY KEY(user_id,symbol));
 INSERT INTO crypto_investments VALUES ('legacy','BTC',0.25);`)
	if err := p.migrateGuildEconomy(); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := p.db.QueryRow(`SELECT count(*) FROM guild_members`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("legacy wallets were allocated: %d, %v", count, err)
	}
	exec(`INSERT INTO guild_members(guild_id,user_id,balance,last_daily,daily_streak,max_daily_streak)
 VALUES ('guild-a','legacy',15,'2026-01-01',1,2),('guild-b','legacy',30,'2026-02-01',2,3);
 INSERT INTO stock_investments VALUES ('legacy','ABC',1,'guild-a'),('legacy','ABC',3,'guild-b');
 INSERT INTO crypto_investments VALUES ('legacy','BTC',1,'guild-a'),('legacy','BTC',2,'guild-b');`)
	fingerprint := func() string {
		t.Helper()
		var s string
		err := p.db.QueryRow(`SELECT jsonb_build_object(
 'wallets',(SELECT jsonb_agg(to_jsonb(g) ORDER BY guild_id,user_id) FROM guild_members g),
 'legacy',(SELECT jsonb_agg(to_jsonb(u) ORDER BY id) FROM users u),
 'stocks',(SELECT jsonb_agg(to_jsonb(s) ORDER BY guild_id,user_id,ticker) FROM stock_investments s),
 'crypto',(SELECT jsonb_agg(to_jsonb(c) ORDER BY guild_id,user_id,symbol) FROM crypto_investments c))::text`).Scan(&s)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	before := fingerprint()
	for i := 0; i < 2; i++ {
		if err := p.migrateGuildEconomy(); err != nil {
			t.Fatal(err)
		}
	}
	if after := fingerprint(); after != before {
		t.Fatal("migration changed existing balances, daily state or investments")
	}
	var secured bool
	if err := p.db.QueryRow(`SELECT relrowsecurity FROM pg_class WHERE oid='guild_members'::regclass`).Scan(&secured); err != nil || !secured {
		t.Fatalf("wallet RLS: %v, %v", secured, err)
	}
}

func TestAssetMigrationPreservesClassification(t *testing.T) {
	p := migrationTestDB(t)
	if _, err := p.db.Exec(`CREATE TABLE gacha_assets(id BIGINT PRIMARY KEY,character_id BIGINT,provider TEXT);
 INSERT INTO gacha_assets VALUES (1,1,'anilist'),(2,1,'gelbooru');`); err != nil {
		t.Fatal(err)
	}
	if _, err := p.db.Exec(migrations.GachaAssetsExtra); err != nil {
		t.Fatal(err)
	}
	var official, extra bool
	if err := p.db.QueryRow(`SELECT a.is_extra,b.is_extra FROM gacha_assets a,gacha_assets b WHERE a.id=1 AND b.id=2`).Scan(&official, &extra); err != nil || official || !extra {
		t.Fatalf("legacy classification: %v %v %v", official, extra, err)
	}
	if _, err := p.db.Exec(`UPDATE gacha_assets SET is_extra=false WHERE id=2;
 INSERT INTO gacha_assets VALUES (3,1,'game-provider',false);`); err != nil {
		t.Fatal(err)
	}
	if _, err := p.db.Exec(migrations.GachaAssetsExtra); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := p.db.QueryRow(`SELECT count(*) FROM gacha_assets WHERE is_extra`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("restart overwrote classification: %d %v", count, err)
	}
}

func TestFreshDatabaseCreatesGuildSchemaWithoutGacha(t *testing.T) {
	p := migrationTestDB(t)
	if err := p.CreateTables(); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"guild_members", "stock_investments", "crypto_investments"} {
		var exists bool
		if err := p.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema=current_schema() AND table_name=$1 AND column_name='guild_id')`, table).Scan(&exists); err != nil || !exists {
			t.Fatalf("missing guild schema for %s: %v", table, err)
		}
	}
	var exists bool
	if err := p.db.QueryRow(`SELECT to_regclass('gacha_players') IS NOT NULL`).Scan(&exists); err != nil || exists {
		t.Fatalf("unexpected gacha dependency: %v %v", exists, err)
	}
}

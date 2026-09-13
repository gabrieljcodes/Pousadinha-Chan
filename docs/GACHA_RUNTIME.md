# Gacha runtime architecture

Go remains the application language. PostgreSQL owns durable state; Valkey is an
optional shared catalog cache. Media storage and delivery are unchanged.

## Roll execution

1. Load an immutable snapshot of eligible character IDs. The bot warms it before
   connecting to Discord. All nine existing gender/media pools are included.
2. Sample uniformly with `crypto/rand`, then fetch the selected card by primary
   key. PostgreSQL validates its current enabled status, approved image and pool
   membership. Names, images, likes, prices and ownership are never taken from
   the shared cache.
3. Preserve the existing transaction: serialize the player's quota, recognize
   duplicate request IDs, persist the roll and award eligible keys atomically.
   Empty pools and failed transactions do not spend rolls. Replayed requests
   still resolve even if rebuilding the pool fails.

The previous selection counted eligible characters and traversed a random SQL
`OFFSET` on every roll. Only a snapshot rebuild now scans the eligible catalog.
The ID sampling step has constant cost; the complete roll still performs indexed
reads and transactional writes in PostgreSQL.

## Catalog consistency and cache failure

Migration `013_gacha_runtime.sql` creates a database-specific namespace and a
transactional revision. Statement triggers invalidate membership after catalog,
asset approval, work-kind or character/work relationship changes, including
changes made through direct SQL. Rolled-back changes do not publish a revision.

Each process checks the revision at most once per second during normal traffic.
An empty pool or repeated rejection of stale candidates forces an earlier check.
One refresh runs at a time per process. Builds use a read-only repeatable-read
transaction so their revision and membership describe the same database snapshot.
Newly eligible characters become available after the next check and rebuild.
Removed/ineligible characters are rejected against current database visibility
when selected, even before the next refresh.

Valkey stores one JSON snapshot per database namespace, with a five-minute TTL.
The payload revision is checked against PostgreSQL. Invalid, obsolete, evicted or
unavailable shared data falls back to a local database build. Cache GET and SET
operations each have a 250 ms timeout; they never hold a player transaction open.
A late cache writer can cause a miss but cannot make an obsolete revision current.

If Valkey cannot connect at startup, the process uses local pools until restart.
An established client can reconnect after transient network loss. PostgreSQL
availability is still required for gameplay. Simultaneous cold starts may build
one snapshot per process: there is no distributed cache lock or Lua dependency.
Full rebuilds can delay requests during large catalog edits; bulk import frequency
and cold-start latency should be measured before adding more replicas.

`Store.PoolCacheStats()` reports local builds, shared hits, cache command errors
and eligible character count. Startup logs include these values after warming.

## Ownership counts and economic consistency

Migration 013 backfills `gacha_guild_stats` from existing ownership while holding
a write-conflicting lock on `gacha_collection`. Statement triggers maintain the
count in the same transaction for inserts, deletes, guild moves and truncation.
Reapplying the migration does not recount or reset existing statistics. Ordinary
keys, trades and gifts within a guild do not update the counter row.

Prices use an indexed count lookup instead of repeatedly counting a guild's
collection. The existing value formula, wallets, ownership, cooldowns, keys and
refund rules remain authoritative in PostgreSQL. Claims are never acknowledged
on the strength of a Valkey lock. No economic operation is queued for eventual
persistence. Both new tables have RLS enabled without public policies, matching
the existing owner/BYPASSRLS service connection model.

The initial backfill blocks ownership writes until its migration commits; deploy
with a migration window appropriate to database size. No character, collection
or wallet rows are deleted or rewritten by this migration.

## Running

Docker Compose includes Valkey 8.1 with a 256 MB eviction limit, no public port,
and no persistence (all contents are rebuildable). The bot defaults to
`redis://valkey:6379` in Compose. Configure `VALKEY_URL` to use another instance;
set it explicitly empty to disable the cache. Outside Docker the variable is
optional and defaults to disabled. Keep Valkey on a trusted private network and
use authenticated/TLS URLs for remote deployments.

The Compose configuration now obtains Discord, catalog and tunnel credentials
from environment variables rather than embedded secret fallbacks.

Migration 013 is embedded and applied by the existing gacha migration runner.
Starting the updated bot applies it before enabling gameplay. This change does
not restart or migrate an already running production deployment by itself.

## Verification and capacity planning

Run integration tests only against a disposable database whose URL contains
`gacha_test`:

```sh
GACHA_TEST_DATABASE_URL='postgres://postgres:test@localhost:55441/gacha_test?sslmode=disable' \
GACHA_TEST_VALKEY_URL='redis://localhost:56379' go test -race ./...

go test ./internal/gacha -run '^$' -bench BenchmarkSampleMillion -benchmem

GACHA_BENCH_DATABASE_URL='postgres://postgres:test@localhost:55441/gacha_test?sslmode=disable' \
go test ./internal/gacha -run '^$' -bench '^BenchmarkPostgresCatalog$' -benchtime=10x
```

The database benchmark creates and removes its own schema with one million
synthetic characters and approved asset records. It compares the old selection,
current ID lookup, and a complete sequential roll transaction. It does not call
Discord or download media. The integration suite covers competing claims, replay,
refunds, keys, trades, pricing, migration reapplication/backfill, counter rollback,
guild moves, concurrent local cache reads, shared reuse and failed cache rebuilds.

This is a foundation, not a claim of 50,000-user capacity. Next measurements should
cover p95/p99 latency and database pool waits under realistic concurrent traffic,
including a crowded guild and cache rebuilds. Then size the PostgreSQL connection
budget across replicas, introduce bounded admission/backpressure, and configure
Discord sharding and REST rate-limit coordination. Add an outbox and a durable
queue for actual background workloads when needed; they are not required to
secure a claim or persist a wallet change.

### Initial local measurement

On an AMD Ryzen 5 5500, PostgreSQL 16 in Docker on localhost, one million
synthetic characters (one approved asset and one anime association each), ten
sequential operations per sub-benchmark produced:

| Operation | Mean time |
| --- | ---: |
| Previous eligible count + random OFFSET + card fetch | 3,910.94 ms |
| Local ID sample + validated card lookup | 0.519 ms |
| Complete new roll transaction | 5.461 ms |

These are diagnostic microbenchmarks with a small sample, not production SLOs.
They exclude Discord, network distance to a hosted database, high concurrency,
large character biographies, guild contention and catalog rebuild latency.

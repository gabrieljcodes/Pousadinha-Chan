# Database migrations

These SQL files are embedded by `migrations.go`; they are not an independently
ordered bootstrap script. Keep their numbers stable so existing release references
remain meaningful. An applied migration is still needed for other installations.

| File | Purpose | Entry point |
| --- | --- | --- |
| 004 | Global character catalog and guild ownership | `gacha.Store.Migrate` |
| 005 | Resumable bulk import jobs and leases | `gacha.Store.Migrate` |
| 006 | Persist imported metadata for retries | `gacha.Store.Migrate` |
| 007 | Extra-image classification; backfill only when adding the column | `gacha.Store.Migrate` |
| 008 | Guild wallets and investment primary keys | `database.CreateTables` |
| 009 | Ownership tokens and trade/divorce/gift actions | `gacha.Store.Migrate` |
| 010 | Keys, valuation and compatibility with older divorce quotes | `gacha.Store.Migrate` |
| 011 | Catalog editorial favorites, reversible archive and primary portraits | `gacha.Store.Migrate` |

`database.CreateTables` initializes the non-gacha schema first. The bot then calls
`gacha.Store.Migrate` when gacha is enabled. The administrative `gacha migrate`
command requires the base database (including `users`) to exist. Gacha migrations
run together in a transaction under an advisory lock; they currently use guarded,
repeatable SQL rather than a migration-history table.

## Removed deployment-specific code

The unused `001`–`003` scripts duplicated the base schema, constraints, indexes and
RLS setup maintained in `internal/database/postgres.go`. They were not embedded or
executed by the application. Their previous contents remain in Git history.

Migration `008` no longer contains a fixed Discord guild ID, guesses a destination
with `LIMIT 1`, copies global balances into multiple guilds, or restores balances
from `users` on every startup. The guild schema now has a single definition here,
and a failure applying it prevents startup.

## Older global-economy installations

Existing `guild_members` balances are authoritative and are never reconciled with
`users.balance`. Legacy global balances remain in `users`; this migration does not
allocate them to guilds. New guild memberships start with zero balance.

Legacy investments without a guild retain `guild_id = ''` and remain stored but
are not included in real guild portfolios. Before upgrading an older global
economy, an operator must explicitly map its balances and investments to the
intended guilds. No server can be inferred reliably from historical activity.
Back up the database, stop writers and review that mapping before importing it;
never overwrite existing guild balances as part of a schema migration.

The `007` legacy image backfill and `010` old-price quote invalidation are general
upgrade steps, not repairs for a particular deployment. Keep them for installations
upgrading from older releases. Completed actions and ownership are retained.

## Verification

Run `go test ./...`. Set `GACHA_TEST_DATABASE_URL` to a disposable PostgreSQL
database whose name includes `gacha_test` to also run migration and game integration
tests. Tests create isolated schemas and must never target a production database.

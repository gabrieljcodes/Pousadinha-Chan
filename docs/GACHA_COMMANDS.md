# Character gacha commands

The bot uses slash commands only. All command names, options, buttons and messages
are in English through the embedded go-i18n catalogs. Ownership, quotas, keys and
coins remain specific to each Discord server.

## Rolling

Use `/roll` and select a pool. All pools share the hourly quota.

| Pool value | Characters |
| --- | --- |
| `roll` | All eligible characters |
| `wa` / `ha` / `ma` | Female / male / all anime characters |
| `wg` / `hg` / `mg` | Female / male / all game characters |
| `w` / `h` | Female / male characters across all origins |

Only enabled characters with approved images qualify. Selection is uniform;
likes affect value, not probability. Gender filters use the catalog gender field;
anime/game filters use associated works. Empty pools do not spend rolls.

Defaults: 10 rolls per hourly window, one claim every three hours and a 45-second
claim window. Configure `GACHA_ROLLS_PER_HOUR` and `GACHA_CLAIM_HOURS` as needed.
Claim cooldowns survive divorce, gifts and trades. Owned characters can appear in
rolls; rolling your own character earns a key.

## Collection and discovery

| Command | Result |
| --- | --- |
| `/harem [member] [mode] [page]` | Collection with list/photo modes, counts and values |
| `/info character:<name or ID>` | Character details, owner, works and value |
| `/search query:<name or ID> [page]` | Search names, native names and aliases |
| `/profile` | Remaining rolls, claim cooldown and collection stats |
| `/top [gender] [claim] [page]` | Popularity ranking with interactive filters |
| `/harem-ranking [page]` | Server collections ranked by total value |
| `/gallery character:<ID> [page]` | Approved images |
| `/keys character:<ID>` | Keys and the next value milestone |
| `/wishlist action:<action> [character]` | View, add or remove wishes |
| `/alias character:<name or ID> [alias]` | Change or list the display alias of a married character |
| `/offers [action] [id]` | List or respond to pending offers |
| `/help` | Bot command reference |

Use internal catalog IDs, not AniList IDs. Wishlists hold up to 20 characters;
wishes mark rolls without changing their odds. Pagination buttons open a private
copy of a public listing. Harem lists show 10 characters per page ordered by
current value, including keys. The visual mode shows one card at a time.

## Character value and divorce

Values use the character's imported AniList favorites, the server's **currently owned character count**, and that character's keys in this server:

```text
base         = max(10, floor(favorites × 0.015))
server bonus = 1 + currently_claimed / 10,000
key bonus    = 1 + keys × 0.02 + floor(keys / 10) × 0.10
value        = floor(base × server bonus × key bonus)
```

There is **no gameplay value cap**. Monetary amounts still use the existing PostgreSQL BIGINT wallet storage; an out-of-range amount fails and rolls back rather than wrapping or being silently capped. Pricing uses exact numeric/integer arithmetic, not floating-point currency calculations.

| Favorites | Claimed in server | Keys | Value |
| ---: | ---: | ---: | ---: |
| 0 | 0 | 0 | 10 coins |
| 10,000 | 0 | 0 | 150 coins |
| 10,000 | 1,000 | 0 | 165 coins |
| 10,000 | 0 | 10 | 195 coins |
| 10,000 | 1,000 | 10 | 214 coins |
| 2,000,000 | 0 | 0 | 30,000 coins |

Each owned character adds 0.01% to the server multiplier: 100 owned = +1%, 1,000 = +10%, 10,000 = +100%. It applies to **every character**, including unclaimed ones. It is based on current ownership, not historical claim events, so divorcing lowers the population bonus and repeatedly reclaiming a character does not permanently inflate prices. Trades and gifts do not change the population count.

The SQL function `gacha_character_value` is the central implementation used by cards, search/harem lists, rankings and divorce quotes. `CalculateValue` is its exact-integer Go counterpart, checked against it by tests. No AniList requests occur while pricing. Card details expose the base, server bonus, keys and key bonus; status shows the server population bonus.

### Keys

A **new roll of a character you already own in that server grants one key automatically**. Rolling someone else's character or claiming an unowned character does not award a key. All existing pool limits still apply; keys neither grant free rolls nor reset the claim cooldown.

Every key adds +2% to the key multiplier. Every 10 keys adds another +10%:

| Keys | Total key bonus |
| ---: | ---: |
| 1 | +2% |
| 5 | +10% |
| 10 | +30% |
| 20 | +60% |
| 50 | +150% |

There is no gameplay key cap. Integer rounding means a low-value character may need several keys before the displayed whole-coin amount increases. `/keys character:<ID>` show progress to the next milestone. The roll card announces earned keys; harem entries also display key counts.

Keys belong to the character's current claim lineage **within the server**. They follow trades and gifts, but divorce removes them; a new claim begins at zero. There are no retroactive keys for old rolls. Duplicate gateway events and concurrent retries of the same request cannot award extra keys. If Discord explicitly rejects delivery and the roll is refunded, its key is revoked once, even after a trade; a refund from a divorced lineage never removes keys from a new claim. As before, ambiguous delivery timeouts are not automatically refunded.

`/divorce character:<ID>` creates a confirmation showing the payout. **No ownership or wallet changes occur until confirmation.** Only the owner can confirm. A successful divorce removes the character from that server's harem and credits that server's `guild_members.balance` in the same transaction. The legacy `users.balance` is untouched. The payout is frozen at the quote for up to 10 minutes, even if favorites, keys or server population change meanwhile. The quote includes the keys before divorce resets them.

Cancel keeps the character. A duplicate confirmation returns the completed result without paying again. Old offers become invalid when the character is transferred, divorced or reclaimed, even if it later returns to its previous owner. Balance overflow or any SQL error rolls back the release and credit together.

## Trades and gifts

```text
/trade member:@member offer:<your ID> receive:<their ID>

/gift member:@member character:<your ID>
```

The recipient must be a current server member and cannot be a bot or yourself. One-for-one trades and one-character gifts require the recipient's **Accept** button. The recipient can decline; the proposer can cancel. Both characters are revalidated and transferred atomically at acceptance. Trading does not create or destroy coins. Gifts and trades do not reset claim cooldowns.

An offer expires after 10 minutes. Offers do not reserve characters: if either side changes ownership first, acceptance marks the offer stale. This keeps characters usable while preventing old offers from taking newly acquired characters. Up to 10 live offers per proposer are allowed per server.

`/offers` lists the 10 newest live incoming/outgoing offers and their IDs. If the original message is unavailable, use these commands in the original channel:

```text
/offers action:accept id:<offer UUID>
/offers action:decline id:<offer UUID>
/offers action:cancel id:<offer UUID>
```

Existing channel restrictions apply to commands and buttons. Offer responses must use the original channel.

## Persistence and deployment

Migration `009_gacha_social.sql` adds ownership tokens, a persistent action/audit table, constraints and indexes. Migration `010_gacha_progression.sql` adds keys and reward receipts, the exact valuation function, and removes the old 25,000-coin ceiling. Pending divorce quotes from the previous pricing version are cancelled on upgrade; completed payment history is preserved. New quotes survive subsequent restarts. It is applied by `Store.Migrate` when gacha starts, or by the existing administrative `gacha migrate` command. The guild economy schema from the previous release must already exist; this migration does not copy or rebalance legacy wallets.

Completed actions retain the participants, character IDs, ownership snapshots, payout and resolution timestamp. A committed divorce action is the audit record for its wallet credit. Buttons work after a restart because offers are in PostgreSQL. Expired offers are rejected at resolution and excluded from the live offer list without requiring a timer worker. RLS is enabled with no public client policy; the bot uses its existing owner/BYPASSRLS service connection.

Restart/redeploy the bot to register the current slash command catalog. Global commands are replaced in one bulk request, removing obsolete names. No reset of collections, claim timers or balances is needed. The gacha still requires `GACHA_ENABLED=true`, the API and a public media URL.

## Validation

```bash
go test ./...
GACHA_TEST_DATABASE_URL='postgres://postgres:gacha-test@127.0.0.1:55441/gacha_test?sslmode=disable' go test -race ./...
```

Integration tests use a unique disposable schema and cover all roll pools, shared quotas, empty-filter rollback, duplicate confirmations, cross-guild/channel rejection, exact wallet credit, frozen quotes, stale ownership, trade-versus-divorce concurrency, rejected/expired offers, current-value totals and atomic rollback. Progression tests also cover exact SQL/Go valuation, guild bonus isolation, key milestones, duplicate/concurrent rewards, gift lineage, delivery refunds, divorce reset, uncapped quotes and migration replay. They do not alter the live character catalog or real Discord collections. Discord delivery still requires a live bot smoke test after deployment.

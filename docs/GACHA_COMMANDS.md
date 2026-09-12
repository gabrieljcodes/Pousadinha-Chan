# Character gacha commands

The character catalog is shared across the bot. Ownership, roll limits, claim cooldowns, trades and divorce proceeds belong to the current Discord server. All gacha messages and slash command labels are in English.

## Roll pools

| Text / slash shortcut | Pool |
| --- | --- |
| `!gacha roll` / `/gacha action:All characters` | All enabled characters |
| `!wa` / `/wa` | Female anime characters |
| `!ha` / `/ha` | Male anime characters |
| `!ma` / `/ma` | All anime characters |
| `!wg` / `/wg` | Female game characters |
| `!hg` / `/hg` | Male game characters |
| `!mg` / `/mg` | All game characters |
| `!w` / `/w` | Female characters across all origins |
| `!h` / `/h` | Male characters across all origins |

Every shortcut also works as `!gacha wa`, etc., or as an `/gacha action` choice. The prefix is `!`. Female/male filters use the explicit character gender field, case-insensitively. Unknown and other genders remain in unrestricted pools. Anime/game membership uses associated works, rather than the metadata provider name. A character associated with both can occur in either pool, with one entry per roll candidate.

All pools share the same hourly roll quota. Empty pools do not spend rolls and never fall back to a different pool. Only enabled characters with an approved image qualify. Games may remain empty until game works have been imported. Roll chances are uniform within the selected pool; favorites change value, not probability.

The existing defaults remain 10 rolls per hourly window, one claim every three hours, and a 45-second public claim window. `GACHA_ROLLS_PER_HOUR` and `GACHA_CLAIM_HOURS` configure the first two. Claim cooldowns survive divorce, gifts and trades. Owned characters can still appear but cannot be claimed again.

## Dual Prefix Support

Both `!` and `$` can be used interchangeably as prefixes for all bot commands (e.g. `$wa`, `!wa`, `$tu`, `!tu`, `$harem`, `!harem`).

## Collection and discovery

| Command | Result |
| --- | --- |
| `$harem [@member] [page]` / `!harem` / `/harem` | Harem list, total character count and total current value |
| `$harem -i` / `$mmi` / `/harem visual:True` | Visual Harem mode with full character photo card and `[◀]` `[▶]` pagination |
| `$im <name or ID>` / `$char <name or ID>` / `/im` | Character details, source, server owner (`Casado com @user` vs `Livre`) and value |
| `$tu` / `!tu` / `$gacha status` | Mudae-style user status: remaining rolls `<t:...:R>`, claim cooldown `<t:...:R>`, total harem value |
| `$topchar [unclaimed\|claimed] [w\|h]` / `/topchar` | Top characters ranking sorted by favorites with claim and gender filters |
| `$topu` / `$topw` / `$toph` | Shortcuts for top unclaimed, top waifus, and top husbandos |
| `$gacha top [page]` | Server harem ranking by current total value |
| `$gacha search <name or ID>` | Search names, native names and aliases |
| `$gacha gallery <ID> [page]` | Approved images |
| `$gacha wish <ID>` | Add a wish (maximum 20) |
| `$gacha unwish <ID>` | Remove a wish |
| `$gacha wishes` | View your wishlist |
| `$keys <ID>` / `/keys character:<ID>` | Keys, value breakdown and next milestone |
| `$gacha help` | Command reference |

Use the **internal catalog ID** displayed on cards, not the AniList ID. Harem, gallery and ranking include Previous/Next buttons. Clicking a public paginator opens a private copy; subsequent navigation updates that copy. Harem pages contain 10 characters, sorted by their current server value (including keys). Wishlist additions are idempotent, including when the list is already full. Wishes are informational and do not increase roll odds.

The original collection/search/gallery/wishlist commands are retained. Portuguese text aliases (`sortear`, `colecao`, `buscar`, `personagem`, `galeria`, `desejar`, `remover`, `desejos`) continue to work, but responses are English.

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

There is no gameplay key cap. Integer rounding means a low-value character may need several keys before the displayed whole-coin amount increases. `!keys <ID>`, `/keys`, and `/gacha action:Character keys` show progress to the next milestone. The roll card announces earned keys; harem entries also display key counts.

Keys belong to the character's current claim lineage **within the server**. They follow trades and gifts, but divorce removes them; a new claim begins at zero. There are no retroactive keys for old rolls. Duplicate gateway events and concurrent retries of the same request cannot award extra keys. If Discord explicitly rejects delivery and the roll is refunded, its key is revoked once, even after a trade; a refund from a divorced lineage never removes keys from a new claim. As before, ambiguous delivery timeouts are not automatically refunded.

`!divorce <ID>` or `/divorce character:<ID>` creates a confirmation showing the payout. **No ownership or wallet changes occur until confirmation.** Only the owner can confirm. A successful divorce removes the character from that server's harem and credits that server's `guild_members.balance` in the same transaction. The legacy `users.balance` is untouched. The payout is frozen at the quote for up to 10 minutes, even if favorites, keys or server population change meanwhile. The quote includes the keys before divorce resets them.

Cancel keeps the character. A duplicate confirmation returns the completed result without paying again. Old offers become invalid when the character is transferred, divorced or reclaimed, even if it later returns to its previous owner. Balance overflow or any SQL error rolls back the release and credit together.

## Trades and gifts

```text
!trade @member <your character ID> <their character ID>
/trade member:@member character:<your ID> receive:<their ID>

!gift @member <your character ID>
/gift member:@member character:<your ID>
```

The recipient must be a current server member and cannot be a bot or yourself. One-for-one trades and one-character gifts require the recipient's **Accept** button. The recipient can decline; the proposer can cancel. Both characters are revalidated and transferred atomically at acceptance. Trading does not create or destroy coins. Gifts and trades do not reset claim cooldowns.

An offer expires after 10 minutes. Offers do not reserve characters: if either side changes ownership first, acceptance marks the offer stale. This keeps characters usable while preventing old offers from taking newly acquired characters. Up to 10 live offers per proposer are allowed per server.

`!gacha offers` lists the 10 newest live incoming/outgoing offers and their IDs. If the original message is unavailable, use these commands in the original channel:

```text
!gacha accept <offer UUID>
!gacha decline <offer UUID>
!gacha cancel <offer UUID>
```

`/gacha` exposes all main actions through the `action` option. Use `member`, `character`, `receive`, `query` and `page` as applicable. Shortcuts and slash commands execute the same services. Existing channel restrictions apply to gacha commands and buttons.

## Persistence and deployment

Migration `009_gacha_social.sql` adds ownership tokens, a persistent action/audit table, constraints and indexes. Migration `010_gacha_progression.sql` adds keys and reward receipts, the exact valuation function, and removes the old 25,000-coin ceiling. Pending divorce quotes from the previous pricing version are cancelled on upgrade; completed payment history is preserved. New quotes survive subsequent restarts. It is applied by `Store.Migrate` when gacha starts, or by the existing administrative `gacha migrate` command. The guild economy schema from the previous release must already exist; this migration does not copy or rebalance legacy wallets.

Completed actions retain the participants, character IDs, ownership snapshots, payout and resolution timestamp. A committed divorce action is the audit record for its wallet credit. Buttons work after a restart because offers are in PostgreSQL. Expired offers are rejected at resolution and excluded from the live offer list without requiring a timer worker. RLS is enabled with no public client policy; the bot uses its existing owner/BYPASSRLS service connection.

Restart/redeploy the bot to apply the migration and register the updated slash commands. The `/gacha` option names are now `action`, `query`, and `page`; the handler still understands older cached interaction names. No reset of existing collections, claim timers or balances is needed. Commands require the existing `GACHA_ENABLED=true`, API and public media URL configuration.

## Validation

```bash
go test ./...
GACHA_TEST_DATABASE_URL='postgres://postgres:gacha-test@127.0.0.1:55441/gacha_test?sslmode=disable' go test -race ./...
```

Integration tests use a unique disposable schema and cover all roll pools, shared quotas, empty-filter rollback, duplicate confirmations, cross-guild/channel rejection, exact wallet credit, frozen quotes, stale ownership, trade-versus-divorce concurrency, rejected/expired offers, current-value totals and atomic rollback. Progression tests also cover exact SQL/Go valuation, guild bonus isolation, key milestones, duplicate/concurrent rewards, gift lineage, delivery refunds, divorce reset, uncapped quotes and migration replay. They do not alter the live character catalog or real Discord collections. Discord delivery still requires a live bot smoke test after deployment.

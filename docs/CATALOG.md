# Catalog studio

A private character and photo editor, embedded in the Go binary. The UI and API
share the bot's PostgreSQL catalog and media directory; no Node build is needed.

## Run independently of Discord

Set these values in the gitignored `.env`:

```dotenv
DATABASE_URL=postgresql://...
CATALOG_ADMIN_PASSWORD=use-a-long-random-password-at-least-16-characters
CATALOG_ADDR=127.0.0.1:8081
GACHA_MEDIA_DIR=./data/gacha
```

Run `go run ./cmd/catalog` and open `http://localhost:8081/catalog/`. The existing
base database must already contain `users`; the command applies gacha migrations
transactionally. It does not initialize, copy or reconcile economy balances.
FFmpeg must be installed for uploads. Sessions last 12 hours and expire on restart.

## Serve with the bot

Set `CATALOG_ADMIN_PASSWORD` and enable the existing API in the bot configuration.
The panel is served at `/catalog/` on that API's port, even with Discord gacha
commands disabled. Catalog sessions are separate from player API keys. The
administrative routes are excluded from the public API's CORS middleware.

For an HTTPS domain, proxy `/catalog/` to the Go server and set
`CATALOG_SECURE_COOKIES=true`. Keep the same public origin for the UI and API, allow
13 MiB request bodies, and allow up to 180 seconds for imports. The standalone
command binds only to loopback by default; use `CATALOG_ADDR=0.0.0.0:8081` inside a
container with an explicitly chosen port mapping. Mount the same persistent media
directory as the bot. A shared password gives full administrative catalog access.

## Workflows

- Browse or search names, native names, aliases, IDs and work titles. Filter by
  universe, gender, publication status, missing portraits or pending photos.
  Choose grid/list layout and sort by likes, name, newest or photo count.
- Favorites are shared editorial marks on characters and individual photos. They
  do not change AniList popularity, wishlists, ownership or character pricing.
- Create characters manually, including a work, or import one AniList character
  by ID/URL. Imports use the existing rate-limited client and official portrait
  approval. Reimporting refreshes provider metadata; no bulk import starts from
  opening the panel. Failed imports may retain metadata for a later retry.
- Edit names, aliases, gender, description and likes. Link additional works with
  genres and studios without modifying the metadata of works shared by other
  characters. Concurrent metadata edits return a conflict instead of overwriting
  a newer revision.
- Add PNG/JPEG/GIF files, up to 12 MiB and 24 megapixels each. Uploads are rendered
  to the bot's 420×600 bordered format; GIFs keep animation, bounded to 12 seconds.
  The UI offers explicit approval on upload. Duplicate originals are rejected.
- Review, favorite and credit photos. Choose an approved main portrait for the
  bot's cards. Replacing a photo uploads a new one and archives the previous one
  atomically, preserving its file and history.
- Archive characters to disable their rolls without deleting claims or keys.
  Restore returns them disabled until explicitly enabled with an approved photo.
  Archive photos to hide them from public media and bot galleries. Restoring a
  photo returns it to pending review. Archival does not reclaim disk space.

## API

All endpoints are under `/catalog/api/`. Send `X-Catalog-Request: 1` on writes.
JSON writes use `Content-Type: application/json`; upload uses multipart form data.
Login returns an HttpOnly, SameSite=Strict session cookie. No database credentials
are sent to the browser. Authentication failures are rate-limited. The panel must
be served through HTTPS when accessed remotely.

| Method | Path | Purpose |
| --- | --- | --- |
| POST | `/login` | `{ "password": "..." }` |
| POST | `/logout` | Clear browser session |
| GET | `/session` | Check administrator session |
| GET | `/stats` | Real catalog counts |
| GET | `/characters` | `q`, `page`, `kind`, `gender`, `status`, `view`, `sort`; 36 per page |
| POST | `/characters` | Create a character |
| GET | `/characters/{id}` | Metadata, works, sources and all photos |
| PUT | `/characters/{id}` | Save metadata with the current `updated_at` revision |
| PATCH | `/characters/{id}` | `action`: `favorite`, `archive`, `enable`; boolean `value` |
| POST | `/characters/{id}/assets` | `file`, `approve`, optional `attribution` and `replace_id` |
| PATCH | `/assets/{id}` | `favorite`, `primary`, `archive`, `review` or `attribution` |
| GET | `/assets/{id}/file` | Authenticated photo preview, including archived/pending files |
| POST | `/import` | `{ "id": 17 }`: import/refresh an AniList character |

Migration `011_catalog_admin.sql` adds editorial state and an index enforcing one
main portrait per character. Database constraints prevent archived characters or
photos from being republished accidentally by older clients. Existing collections,
keys, balances, source records and media files remain intact.

## Validate

`go test -race ./...` runs unit tests. Set `GACHA_TEST_DATABASE_URL` to a disposable
PostgreSQL database whose name includes `gacha_test` for the integration suite.
It covers authentication, CSRF, catalog creation/search/edit conflicts, archives,
claim preservation, photo replacement/deduplication, and file path containment.
The browser checks use a separate disposable fixture; never create test characters
in the real catalog.

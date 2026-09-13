# Gacha media storage

Character metadata, ownership and approval remain in PostgreSQL. Rendered PNG/GIF cards can live on the filesystem or in a private S3-compatible bucket. `GACHA_PUBLIC_URL` remains the application's HTTPS origin; existing catalog paths and Discord URLs do not change. The application checks approval before serving a public image. Administrative previews require authentication and use private, non-storable responses.

## OpenDAL implementation

`internal/mediastore` uses Apache OpenDAL's Go binding and the official `fs` and `s3` service packages, pinned together at v0.1.16. The binding uses purego and libffi; builds use `CGO_ENABLED=0`. There is no AWS SDK dependency. See [the binding's requirements](https://opendal.apache.org/docs/bindings/go/getting-started/) and [OpenDAL S3 configuration](https://opendal.apache.org/services/s3/go/).

Filesystem reads, streamed writes, publication and deletion use the OpenDAL fs operator. Go's `os.Root` resolves paths safely before passing pinned `/proc/self/fd` descriptors to OpenDAL. Holding those descriptors through the operation prevents symlink replacement from redirecting IO outside the media directory. Writes go to a temporary file and are published by atomic rename only after successful completion.

For S3, OpenDAL loads credentials and produces signed HTTP requests using its `PresignStat`, `PresignRead` and `PresignWrite` APIs. A small `net/http` adapter transports them on the server. This preserves cancellation, streaming, content types, ETags, byte ranges and conditional writes, which the binding's basic Reader/Writer APIs do not expose as per-operation options. Deletion uses the native `Operator.Delete` API because the pinned S3 service does not support presigned deletion. Signed URLs are never returned to clients or included in transport error messages. Copying uses `If-None-Match: *`, so another writer winning the race cannot be overwritten.

Operators are shared across requests. Every returned object must be closed. `Store.Close` waits for open objects and in-flight operations before releasing native operators; application entry points call `CloseMedia` during shutdown. Native filesystem and delete calls check Go cancellation between calls and have OpenDAL timeout layers, but cannot be interrupted mid-FFI-call by a Go context. S3 HTTP transfers use request contexts, bounded retries and a two-minute client timeout; native credential discovery/signing and deletion remain subject to OpenDAL's timeout behavior.

## Runtime requirements

Use Go 1.25+ on Linux amd64 or arm64. The pinned official service packages ship Linux shared libraries; this adapter also requires `/proc/self/fd`. Their native libraries require glibc 2.38 or newer, libgcc and libffi. The Dockerfile uses Debian Trixie for both build and runtime, with `libffi8`, `libgcc-s1`, certificates and FFmpeg installed. A successful Go compile alone does not verify these runtime requirements.

The service packages embed compressed OpenDAL libraries and extract them into `TMPDIR` on first use. That directory must be writable and permit executable memory mappings (not a `noexec` mount). Docker sets it to `/app/tmp`, owned by the unprivileged application user with mode 0700. Use a private temporary directory for non-Docker deployments as well. The binding keeps loaded shared libraries for the process lifetime; closing a Store frees its operators and readers.

FFmpeg continues rendering into temporary local files before storage publication. Preserve enough temporary disk space even when final images live in S3. Existing media paths, public domains, database rows and filesystem defaults remain unchanged; changing the storage engine requires no data migration.

## Configuration

Filesystem is the default:

```dotenv
GACHA_STORAGE_BACKEND=filesystem
GACHA_MEDIA_DIR=/app/data/gacha
GACHA_PUBLIC_URL=https://pousadinha.com
```

S3:

```dotenv
GACHA_STORAGE_BACKEND=s3
GACHA_MEDIA_DIR=/app/data/gacha
GACHA_PUBLIC_URL=https://pousadinha.com
GACHA_S3_BUCKET=character-media
GACHA_S3_REGION=sa-east-1
GACHA_S3_PREFIX=gacha
GACHA_S3_ENDPOINT=
GACHA_S3_PATH_STYLE=false
```

The prefix is optional and must not start or end with `/`. A catalog path such as `series/123-character/photo-anilist-123/hash-v1.png` becomes `gacha/series/123-character/photo-anilist-123/hash-v1.png` in the bucket. The prefix does not appear in public URLs.

For MinIO, use its endpoint (e.g. `http://minio:9000`), region `us-east-1`, and `GACHA_S3_PATH_STYLE=true`. For Cloudflare R2, use your account's HTTPS S3 endpoint, region `auto`, and path style. The bucket must already exist. Production endpoints should use HTTPS.

OpenDAL handles credentials. The adapter explicitly forwards Go environment values for `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, optional `AWS_SESSION_TOKEN`, `AWS_PROFILE`, and `AWS_EC2_METADATA_DISABLED`; this also supports values loaded from `.env` with CGO disabled. OpenDAL can additionally discover shared profiles and workload credentials through its native provider. Pass provider-specific environment settings (such as `AWS_SHARED_CREDENTIALS_FILE`) to the process itself, rather than relying on a late `.env` load. Keep credentials out of version control. Docker Compose forwards static credentials and profile selection; mount profile files or configure workload identity separately when using those mechanisms.

Grant `s3:GetObject`, `s3:PutObject`, and `s3:DeleteObject` on the configured prefix. Grant `s3:ListBucket` restricted to that prefix so missing objects return 404 instead of 403; fallback deliberately does not treat access-denied as missing. No public-read ACL or public bucket is needed. These operations do not create buckets or manage their policies. Enable provider-side encryption/versioning according to your deployment requirements.

Use identical settings for the bot, catalog server and import CLI. Settings are loaded at startup. Do not change bucket/prefix while requests are running. One configuration addresses one shared, immutable key namespace.

## Existing local catalog

In S3 mode, new imports and uploads go to S3. Reads check S3 first, then the local directory **only when S3 reports that the key does not exist**. Permission failures, network failures and server errors remain errors. Keep the existing media volume mounted on every instance that needs to read uncopied files.

Plan and apply a copy using the configured S3 backend:

```sh
go run ./cmd/gacha media-copy
go run ./cmd/gacha media-copy --apply
```

The default invocation only counts catalog entries. `--apply` processes database pages of 200 entries, uploads missing objects and compares SHA-256 of the actual stored bytes against each local rendered file. It includes pending and archived images, preserving future restoration. It does not use `gacha_assets.sha256` for verification because that column hashes the original input before rendering. Files uploaded directly to S3 with no local original are checked for remote existence.

Copying stops on the first failure. Rerunning is safe: existing remote objects are checked rather than overwritten. All local originals are retained; no database paths, metadata or approval states are changed. Pause imports, replacements and permanent deletions for the final copy pass to obtain a stable catalog. Confirm the copy completes and test the application before moving to nodes without the old volume. Keep a backup. Switching back to filesystem alone does not copy S3-only uploads back to disk.

The CLI performs the application's normal startup migrations before dispatching commands; this feature adds no database migration.

## Serving and deletion

Public and private routes share the same reader. S3 reads stream from the requested offset, supporting HTTP HEAD, byte ranges and conditional requests without buffering whole GIFs. S3 ETags are used for conditional requests and `If-Match` across seeks. Approval is rechecked before cache validation. Public responses retain the existing five-minute cache lifetime; revocation cannot invalidate copies already cached elsewhere.

Failures detected before streaming return a generic 503; missing files return 404. Failures after HTTP headers have been sent can interrupt the response body. Authenticated previews of locally managed assets do not redirect to their upstream source on storage failure. Remote-only catalog entries retain their original redirect behavior.

Catalog archive/reject actions retain media for restoration. Run permanent extra-image deletion while imports and copy jobs are paused. It removes database references first, then both S3 and filesystem copies. Cleanup failures are reported and may leave unreferenced objects. Distributed PostgreSQL/object-storage transactions are not atomic: an interrupted import or ambiguous commit can also leave an orphan, so deletion never assumes a failed commit proves an object is unused. Do not apply bucket lifecycle expiry to live catalog prefixes.

## Validation

```sh
go test -race ./internal/mediastore ./internal/gacha ./internal/catalogweb
```

The storage tests cover path confinement, atomic local publication, cancellation, S3 streaming, fallback/error distinction, copying, and deletion. To include tests against a disposable S3-compatible service, set `GACHA_TEST_S3_ENDPOINT`, `GACHA_TEST_S3_ACCESS_KEY`, and `GACHA_TEST_S3_SECRET_KEY`. Pre-create a disposable bucket and set `GACHA_TEST_S3_BUCKET` (default `gacha-test`). Each integration test uses a unique prefix and removes only its own objects; it never creates or deletes buckets. Never use production credentials.

Also run `CGO_ENABLED=0 go test ./internal/mediastore` in the deployment runtime to verify native loading without CGO.

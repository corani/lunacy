# Read-only synchronization API

The implementation follows [the v1 plan](plans/lunacy-api-v1.md). Use
[lunacy-cli](client.md) to inspect full/incremental responses before building a
consumer.

## Requests

All routes are relative to the service's base URL, including any deployment
prefix. Traefik Basic Authentication protects API, UI and covers; the optional
application credentials protect **WebDAV only**. Do not expose the backend
directly assuming those credentials protect the API.

| GET route | Result |
| --- | --- |
| `api/v1/sync` | `{watermark, baseline_at, books, annotations}` |
| `api/v1/sync?since=<RFC3339>` | Same shape, with changed eligible records |
| `api/v1/book?filename=<encoded identifier>` | `{book, annotations}` with all current annotations |
| `api/v1/book/cover?filename=<encoded identifier>` | Existing cover image or generated SVG fallback |

An omitted `since` means full retrieval; an empty, repeated, malformed,
over-precision or future timestamp is rejected. Explicit timezone offsets are
accepted; at most three fractional digits are allowed. Responses use UTC with
three fractional digits. Sync and detail have `Cache-Control: no-store`.

Book identifiers are exact canonical filenames, not titles or arbitrary local
files. Annotation `id` is the API identity; `ann_id` is the source deduplication
key. Both remain JSON integers. Annotation IDs are stable only within the retained
database. Changing a source path is a new book identity.

## Eligibility and fields

Only books with backup metadata and reading evidence are exported: annotations
(including bookmarks), positive progress/time/words in current or daily records,
or source reading/finished state (`1`/`3`). Zero-percent unopened positions,
ratings or metadata alone do not qualify. Cache-only stubs are excluded until
backup metadata arrives under their same key. Detail and cover return 404 for
excluded or unknown books.

Books include source metadata (`filename`, `title`, `author`, normalized `authors`,
`description`, `category`, parsed `series`/`series_index`, `favorite`, `rate`,
`add_time`, `read_state`), current `annot_count`, `updated_at`, `sync_updated_at`,
nullable `position` and `reading_stats`, and `links.ui`/`links.cover`.

Positions include nullable chapter/split/character/page fields, `pct`,
`last_read`, and `updated_at`. Reading statistics include nullable source
`used_time_ms`, `read_words` and their `updated_at`, plus complete current `days`
(`day`, `read_time_ms`, `words_read`, `pct`, `updated_at`). Totals are not inferred
from day sums. Days may exist before cumulative totals are known.

Annotations expose location, quote, source note, style, type, chapter label,
`created_at`, and observation `updated_at`. Inapplicable location fields are
explicitly null. Lists, including authors and days, are always arrays. Books are
sorted by filename, annotations by filename/id and days by epoch day.

Source added/creation/last-read times use Unix milliseconds; zero last-read means
unknown for backup fallback. Reading durations are milliseconds, daily dates are
epoch days, percentages are 0-100, and ratings are source integers (currently
0-5). Unknown source states are preserved. No confirmed completion date is
invented. Source data is not permission to overwrite Obsidian-local enrichment.

Resolve relative links against a service base URL with a trailing slash.
`#/books/<encoded filename>` remains the UI deeplink. Cover links never contain
server filesystem paths or credentials. Existing server-readable cover references
are supported. Backups currently do **not** import image bytes or device cover
paths; generated covers remain the fallback. Store operations support explicit
cover reference/removal and prepared content hashes for future supported sources.

## Watermarks and deployment

Before first startup with this version, back up `lunacy.db` using a consistent
SQLite backup (stop the service first if copying database files). The appended
migration atomically initializes existing observation timestamps to one
`baseline_at`; it cannot reconstruct historical edit times. Cumulative totals
remain null until a new backup supplies them.

Incremental bounds are **inclusive**: `since <= updated_at <= watermark`.
Boundary records can repeat. Books are selected by aggregate `sync_updated_at`;
annotations are independently selected by their own timestamp. Every returned
annotation has its current parent book included. Rating, position or statistics
updates do not resend unrelated old annotations.

Consumers save the **server** watermark only after successfully applying the
entire response. The CLI saves it after validating and writing the output, since
it is an inspection tool rather than an importer. No changes still returns a
watermark. Identical source reimports do not advance record timestamps.

SQLite immediate transactions serialize mutations and snapshot publication,
including independent connections. A persisted floor protects subsequent
timestamps against restart/backward clocks. Responses are materialized and the
transaction released before network delivery. Backup records are committed in
per-book batches; partially completed backups can be visible, while a failed
book batch rolls back its records and timestamps.

To skip historical data, perform one full CLI sync to an inspection/discard file
and keep its saved watermark. Inclusive boundary rows may repeat once; clients
must deduplicate by identity. After restoring an older database, reset the
client watermark and replay. There is no deletion feed, pagination, file-rename
reassociation or API editing.

Explicit handler errors, including legacy API errors, use:

```json
{"error":{"code":"invalid_since","message":"..."}}
```

Input errors are 400, missing/excluded books 404 and store failures 500. Internal
database details stay in server logs. Successful legacy API responses are
unchanged. Static files, WebDAV, Traefik and mux-generated method/routing errors
do not necessarily use this envelope.

## Validation

Run `GOFLAGS=-race ./build.sh test vet` and `./build.sh lint build`. Tests cover
conditional changes, source merging, eligibility transitions, atomic rollback,
migration baseline, independent-store concurrency/backward clocks, slow HTTP
clients, legacy errors and actual server/client integration.

Optional personal-backup checks use isolated temporary databases and never
modify or retain the source archives:

```sh
LUNACY_TEST_BACKUP='/path/to/backup.mrpro' \
LUNACY_TEST_NEXT_BACKUP='/path/to/next.mrpro' \
go test ./internal/importer -run '^TestBackupPerformance$' -v -count=1
```

This reports initial/repeat/next timing, write counts, response size and changed
records; it also verifies identical reimports preserve all record timestamps.
Do not commit personal archives, responses or credentials.

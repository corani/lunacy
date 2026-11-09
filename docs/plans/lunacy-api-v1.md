# Lunacy read-only sync API v1

## Goal

Replace the data-access role of unwise with a dedicated lunacy API for a future
Obsidian plugin. Moon+ Reader remains the source of truth. The API exposes
structured book metadata, annotations, reading position, and reading history;
the plugin controls note creation, enrichment, formatting, and metadata mapping.

This document is an implementation plan and proposed contract, not a description
of functionality already implemented.

## Scope

### Included

- Full initial retrieval of eligible books and incremental retrieval using a
  timestamp watermark.
- Books with backup metadata and reading evidence: annotations, nonzero progress,
  recorded reading time/words, or an explicit reading/finished state. Include books
  with reading position but no annotations; do not require an annotation count.
- Existing canonical book metadata and annotation fields.
- Current reading position, percentage, last-read time, and Moon+ reading state.
- Rating, cumulative reading time/words, and daily reading history.
- Stable references using the existing canonical filename/path for books and the
  lunacy database ID for annotations.
- A deeplink to the book in the existing lunacy UI and access to its cover.
- Existing Traefik HTTP Basic Authentication, without application-level tokens,
  users, or granular permissions.
- Change detection that does not treat an identical reimport as a new change.
- Schema migration, focused tests, and import-performance measurements.

### Excluded

- Implementing the Obsidian plugin, book search, or Google Books integration.
- Editing books or annotations through the UI or API.
- Bidirectional sync and conflict resolution.
- Deletion propagation, tombstones, and annotation-deletion reconciliation.
- Pagination, a retained event log, or Readwise compatibility.
- Automatically converting existing unwise annotation IDs.
- Inventing metadata or completion dates that Moon+ does not provide.
- Renamed-file detection or new generated book IDs.
- Exporting cache-only stub books or their annotations before backup metadata
  arrives. Daily automatic Moon+ backups make that temporary delay acceptable.
- Exporting untouched unread books. Adding or changing library metadata alone does
  not make a book eligible or cause it to appear as updated in the API.

The existing UI stays read-only. Its existing list pagination remains unchanged;
only the new sync API omits pagination. Existing API error bodies intentionally
change to the shared structured JSON format; successful responses and existing
status codes remain unchanged.

## Current implementation

Relevant code:

- `internal/store/schema.go`: append-only schema migrations.
- `internal/store/queries.go`: records, upserts, and UI queries.
- `internal/importer/backup.go`: backup metadata, annotations, statistics, read
  states, and fallback positions.
- `internal/importer/cache.go`: `.po` position and `.an` annotation imports.
- `internal/parser/mrbooks.go`: source records from Moon+ databases.
- `internal/api/handler.go`: existing read-only UI/API routes.
- `internal/webdav/handler.go`: upload-triggered imports and elapsed-time logging.
- `cmd/lunacy/main.go`: routing and optional application authentication for WebDAV.

Books are keyed by `filename`, a canonical, generally lowercased path. Annotations
have a generated SQLite integer `id` and an `ann_id` derived from their Moon+
creation timestamp. `(filename, ann_id)` is the cross-source upsert key.

There are no synchronization timestamps today:

| Existing field | Meaning |
| --- | --- |
| Book `add_time` | Moon+ book-added time, in Unix milliseconds |
| Position `last_read` | `.po` file modification time, in Unix milliseconds; zero for backup fallback |
| Annotation `created_at` | Moon+ annotation creation time, in Unix milliseconds |
| Reading-day `day` | Unix epoch day, not a modification timestamp |

The backup parser already reads cumulative `usedTime` and `readWords`, but the
importer currently persists only daily statistics. The existing UI derives
reading totals by summing daily records.

Additional implementation gaps relevant to this plan:

- `UpsertBook` includes `cover_file` in INSERT but never updates it on conflict.
  The backup importer also never supplies `Book.CoverFile`. Adding a conflict
  assignment alone therefore does not establish a working cover-import path.
- `UpsertBookAuthors` deletes and reinserts associations through separate database
  calls, without a transactional comparison.
- The existing `jsonError` logs the error and calls `http.Error`; despite its name,
  it does not return JSON.

## Identity and future plugin compatibility

### Books

Use `filename` as the book identifier throughout the API. Do not introduce a
generated book ID or identify books by title, author, ISBN, or Obsidian note path.

Do not substitute `add_time` for the canonical filename in v1. Millisecond
precision does not guarantee uniqueness: the documented Moon+ schema has no
unique constraint on `addTime`, and neither uniqueness across batch additions
nor stability across library recreation has been established. A duplicate would
associate unrelated books with one Obsidian note. Keep `add_time` as metadata;
reconsider timestamp-based identity only if source guarantees can be verified,
not merely because clashes appear unlikely.

The future plugin can store this value in a property such as `lunacy_book_id`.
The property name is a plugin decision, not part of the API contract.

- A changed book title retains the same identity.
- Renaming an Obsidian note retains the same association.
- Changing the source filename/path creates a different identity in v1.
- Cache-only stub books and their annotations are excluded from the versioned
  API. Keep existing internal stub creation/import behavior unchanged.
- A book becomes eligible once it has both backup metadata and reading evidence.
  That transition must advance its sync timestamp; existing retained annotations
  for that book must also become discoverable, even if their content did not change.
- Do not add automatic reassociation of basename stubs to full backup paths.
  Existing import behavior remains authoritative; the daily-backup delay is
  accepted rather than introducing a new identity-resolution feature.

### Export eligibility

Apply one shared eligibility rule across sync, detail, and cover endpoints. A book
qualifies only when `is_stub = 0` and at least one of these conditions holds:

- At least one stored annotation exists, including a source bookmark.
- Its current reading percentage is greater than zero.
- Cumulative reading time or words read is greater than zero.
- A reading-day record has positive time, words, or progress.
- Moon+ `read_state` explicitly marks it reading (`1`) or finished (`3`).

A position row alone does not qualify: Moon+ can store an unopened position at
zero percent. A nonzero file modification time, an added date, a rating, a shelf
assignment, or other metadata alone is not reading evidence either.

Do not require a nonzero `read_state` when other evidence exists. A source book
with state `0` and real progress or annotations still qualifies, without changing
the source state returned by the API.

Untouched unread books remain stored for the UI and future imports, but are
treated as not updated by the versioned API regardless of internal metadata
timestamps. This avoids exporting most of the roughly 5,000-book library.

If a previously excluded book gains evidence, return its complete current metadata
and position on the next incremental sync even if that metadata predates the
client's watermark. If a book loses all evidence and becomes excluded, do not send
a deletion event or instruct the plugin to delete an existing note; deletion
propagation remains out of scope.

### Annotations

Expose both `id` and `ann_id`, with different documented purposes:

- `id`: lunacy's integer API identity, used by the plugin to associate callouts.
- `ann_id`: source-derived timestamp identity used by lunacy for deduplication.

Ordinary upserts preserve `id`. Do not delete and reinsert annotations as part of
change detection. Do not use quote text as identity: identical quotes at different
locations may be distinct annotations.

IDs are stable within the retained lunacy database, not across database rebuilds.
Keep IDs as integer JSON values and verify that supported source timestamps fit
JavaScript's safe-integer range.

### Enrichment and local writing

Return structured data, not Markdown or an Obsidian frontmatter schema. The future
plugin may search Google Books to create a complete note when no associated note
exists, without changing the book's lunacy identity.

Descriptions, quotes, and notes returned here represent lunacy's source view.
They are not permission to overwrite enriched descriptions, wiki-links, or
commentary already present in Obsidian. Ownership and local-change detection belong
in the plugin. No API writes are introduced to accommodate those future concerns.

## Change tracking

### Proposed persisted fields

Store observation timestamps as Unix milliseconds internally and return them as
UTC RFC 3339 strings with millisecond precision.

| Record | New fields | Advance when |
| --- | --- | --- |
| Book | `updated_at`, `sync_updated_at` | Own metadata changes; aggregate additionally changes for exported children |
| Position | `updated_at` | An applied position, progress, or last-read value changes |
| Annotation | `updated_at` | An applied annotation field changes |
| Reading totals | `updated_at` | Cumulative source time or word count changes |
| Reading day | `updated_at` | Time, words, or percentage for that day changes |

`books.updated_at` covers exported book metadata, including authors and read
state. `books.sync_updated_at` is the discoverability timestamp for the whole book:
metadata, annotations, position, reading totals, and reading days.

Do not derive either timestamp from `created_at`, `add_time`, or `last_read`.
Editing an old annotation or rating must be discoverable even when its original
source dates do not change.

Store cumulative statistics in a new one-row-per-book table, tentatively
`reading_totals(filename, used_time_ms, read_words, updated_at)`. Keep
`reading_days` for the original daily records. Preserve the existing internal
`read_time_s` column's current millisecond interpretation; an unrelated column
rename is not needed.

Reading-day queries must include the existing `pct` column. Map `read_time_s` to
the JSON field `read_time_ms` explicitly, either with
`read_time_s AS read_time_ms` or by scanning into a typed field tagged
`json:"read_time_ms"`. `GetReadingDays` already uses the latter approach; do not
assume that matching the SQL column name is required for correct JSON output.

### Conditional writes

Use conditional SQLite upserts rather than a new SELECT-and-compare round trip
per row:

1. Compute the effective incoming values according to source precedence.
2. Insert new records, or update existing records only when those effective values
   differ.
3. Use null-safe comparisons such as `IS NOT` for nullable position fields.
4. Set `updated_at` only for a real change.
5. Advance the parent `sync_updated_at` atomically with the changed child.

Compare exported values rather than internal bookkeeping. For example, an
unchanged cover path must not create a change simply because the backup was
re-extracted. If cover bytes change at the same path, record a change using a
content comparison or fingerprint, not merely extraction time.

For cover references, add a source-aware conflict-update path to the store rather
than simply assigning `cover_file = excluded.cover_file`: the current importer
supplies an empty value, which must not clear a valid existing cover unintentionally.
Distinguish an unavailable source field from an explicitly removed cover.

Before wiring a cover value into this path, establish how supported imports
actually supply server-readable images. Moon+ device paths are not usable local
server paths, and a backup's cover reference does not prove that image bytes are
included. Preserve generated fallbacks when images are unavailable; do not promise
that adding the SQL assignment alone imports covers. A real applied cover-reference
or image-content change must update both book timestamps.

Export eligibility is also observable: if a book gains reading evidence, or backup
metadata upgrades a stub that already has evidence under the same key, advance its
aggregate timestamp. Timestamp retained annotations as needed so an incremental
client can discover them when the book becomes eligible. Perform this atomically
with the transition. This does not expose untouched books/stubs or add identity
reassociation.

Detect an eligibility transition inside the store transaction, comparing the
effective before/after state once that book's batch has been applied. Advance its
aggregate timestamp and bring retained annotations into the same sync window.
For transition timestamp `T`, the annotation operation is:

```sql
UPDATE annotations
SET updated_at = ?
WHERE filename = ? AND updated_at < ?;
```

Bind `(T, filename, T)` and advance `books.sync_updated_at` to at least `T` within
the same transaction. Do not change annotation IDs, source creation times, or
content. The shared timestamp floor ensures `T` is not behind a published
watermark. Execute this only for a genuine excluded-to-eligible transition, not
on every reimport or every subsequent metadata update.

This timestamp change records first/re-established export visibility, even when
annotation content is unchanged. A book with no retained annotations still advances
its aggregate timestamp. Comparing before/after once per committed book batch avoids
transient eligibility changes caused by intermediate writes within that batch.

Internal source-change timestamps may still track metadata changes for excluded
books, but query eligibility before treating those rows as API updates. Do not
discard their metadata or use their old metadata timestamps to miss a later
eligibility transition.

Compare normalized author sets in the transaction-scoped store helper, before any
delete/reinsert or timestamp advancement. The importer supplies normalized source
authors, but does not independently query the current set or decide whether it
changed. Ignore duplicate authors and input ordering for set comparison.

Only a changed set is rewritten and contributes to advancing the book's own and
aggregate timestamps. Read the current set, compare it, apply associations, and
update timestamps in the same transaction; an error rolls all of them back.
Preserve the raw author field and the existing normalized author representation.

Treat a genuinely changed `.po` modification time as a last-read change, even if
the position is the same. Replaying exactly the same content and source timestamp
is a no-op; a newly timestamped upload is not necessarily one.

### Source precedence

Retain existing effective precedence and avoid creating artificial changes when
switching between backup and cache sources:

- Backup metadata is authoritative over stub metadata.
- Backup position is a fallback; it must not overwrite a real `.po` position.
- `.an` can provide fields absent from backup annotations, notably chapter labels
  and richer location/type information. Missing source fields must not clear those
  stored values merely because a backup was imported.
- A field explicitly cleared in a source that actually supplies that field is a
  real change.

Review the backup/cache annotation merge carefully. The current unconditional
upserts can replace richer cache fields with backup defaults. Correct directly
coupled merge behavior before enabling timestamps; otherwise alternating uploads
would produce misleading updates.

### Transaction boundaries

Perform a record change and its parent timestamp update in the same transaction.
For backups, group related writes by book or a small bounded batch, rather than
committing every individual annotation or day. Prefer one aggregate parent update
per changed book per batch.

Do not hold a write transaction while reading/decompressing the backup, parsing
source databases, or generating covers. Prepare source data first.

Each committed batch must be internally consistent and discoverable. A failed
batch rolls back its data and timestamps and reports an error. Already committed
batches can remain visible; the API is a current-state feed, not a guarantee that
an entire multi-minute backup import is atomic.

All source mutation paths must use the same transaction/change-tracking mechanism,
including cache imports, read-state updates, author updates, and cover changes.

## Watermark semantics and concurrency

### Contract

- Omit `since` to retrieve all currently eligible books and their annotations,
  not the complete unread library.
- Supply `since` to retrieve records satisfying
  `since <= relevant_updated_at <= watermark`.
- Bounds are intentionally inclusive. Boundary records may be returned again.
- Return `watermark` from the server, even for an empty successful response.
- The client advances its saved watermark only after applying the whole response
  successfully. It must not use its own clock or advance it after a failed write.
- Each response is built from one consistent SQLite snapshot.
- There is no user-supplied `until` in v1: one combined sync request avoids the
  separate-books/separate-annotations window problem.

This returns the latest current state of changed records, not all intermediate
edits. Deleted records are absent, without deletion events.

### Preventing skipped concurrent updates

Capturing `time.Now()` and querying rows independently is insufficient. In
particular, a writer could assign an old timestamp before a sync begins and commit
after the sync snapshot, causing that change to be skipped next time.

Use a shared store-level serialization boundary for mutation commits and sync
snapshot establishment:

1. Serialize mutation transactions and the short sync-snapshot transaction.
2. Maintain a persisted timestamp floor in a singleton sync-state record.
3. Choose the sync upper bound as at least both server time and that floor.
4. Establish the snapshot and persist its upper-bound floor before publishing the
   response.
5. Assign subsequent mutation timestamps no earlier than the persisted floor.
   Equal-millisecond timestamps are allowed because the lower bound is inclusive.

A short serialized transaction for sync reads and floor publication is acceptable
for this one-user, unpaginated service. Materialize the response, commit, and
release the boundary before writing it to the network. The implementation must
also ensure serialization at SQLite's writer-transaction boundary, not rely only
on a process-local mutex or a deferred transaction with no established snapshot.

Persisting the floor protects returned watermarks across process restarts and
backward clock adjustments. It is not a public revision token or an event log.
Database restoration to an older snapshot still requires a client watermark reset.

## Proposed API

Add routes under `/api/v1` without changing existing UI routes or successful API
responses. Existing API error bodies also adopt the common JSON format described
below. All paths below are relative to the externally configured lunacy service
base URL.

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/api/v1/sync?since=<timestamp>` | Combined full/incremental retrieval |
| GET | `/api/v1/book?filename=<encoded-path>` | Full current detail for one book |
| GET | `/api/v1/book/cover?filename=<encoded-path>` | Existing image or generated fallback |

There are no write routes or pagination parameters.

### `GET /api/v1/sync`

`since` accepts an RFC 3339 timestamp, including an explicit timezone and at most
millisecond precision. Normalize offsets to UTC. Reject empty, malformed,
over-precision, or future values rather than silently falling back to a full sync.
An omitted parameter, not an empty parameter, means full retrieval.

Example incremental request:

```http
GET /api/v1/sync?since=2026-11-04T04%3A00%3A00.000Z
Accept: application/json
```

Example response, with illustrative values:

```json
{
  "watermark": "2026-11-04T04:30:00.000Z",
  "baseline_at": "2026-11-04T03:00:00.000Z",
  "books": [
    {
      "filename": "/storage/books/example.epub",
      "title": "Example Book",
      "author": "Example Author",
      "authors": ["Example Author"],
      "description": "Book description from Moon+ Reader.",
      "category": "Example Series #2",
      "series": "Example Series",
      "series_index": 2,
      "favorite": "Example Shelf",
      "rate": 4,
      "add_time": 1793760000000,
      "read_state": 1,
      "annot_count": 1,
      "updated_at": "2026-11-04T03:20:00.000Z",
      "sync_updated_at": "2026-11-04T04:20:00.000Z",
      "position": {
        "chapter": 3,
        "split_index": 1,
        "char_offset": 420,
        "page": null,
        "pct": 27.5,
        "last_read": 1793765400000,
        "updated_at": "2026-11-04T04:10:00.000Z"
      },
      "reading_stats": {
        "used_time_ms": 7200000,
        "read_words": 24000,
        "updated_at": "2026-11-04T04:20:00.000Z",
        "days": [
          {
            "day": 20761,
            "read_time_ms": 3600000,
            "words_read": 12000,
            "pct": 27.5,
            "updated_at": "2026-11-04T04:20:00.000Z"
          }
        ]
      },
      "links": {
        "ui": "#/books/%2Fstorage%2Fbooks%2Fexample.epub",
        "cover": "api/v1/book/cover?filename=%2Fstorage%2Fbooks%2Fexample.epub"
      }
    }
  ],
  "annotations": [
    {
      "id": 17,
      "ann_id": 1793760000000,
      "filename": "/storage/books/example.epub",
      "chapter": 3,
      "split_index": 1,
      "char_offset": 300,
      "page": null,
      "length": 20,
      "color": -256,
      "created_at": 1793760000000,
      "type": 1,
      "underline": 0,
      "strikethrough": 0,
      "note": "Source annotation note.",
      "text": "An illustrative quote.",
      "chapter_label": "Chapter Three",
      "updated_at": "2026-11-04T04:15:00.000Z"
    }
  ]
}
```

Response rules:

- Select books by `sync_updated_at`, not only their metadata timestamp.
- Apply the shared export-eligibility predicate to books. Filter annotations
  through their owning book's eligibility as well; never return an annotation
  belonging to an excluded book.
- Select eligible annotations independently by their `updated_at`.
- Every returned annotation's owning book is included. Parent timestamps should
  ensure this invariant; verify it in queries and tests.
- Return each book once, with its complete current metadata, position, and reading
  statistics from the response snapshot.
- Return only changed annotations for incremental requests. A rating or progress
  change must not resend every historical annotation.
- `annot_count` is the current count, not the number in this response.
  Reuse the existing count semantics, but keep the aggregate independent of joins
  to authors or reading days so those rows cannot multiply the count.
- `books`, `annotations`, `authors`, and `days` are JSON arrays, never `null`.
- `position` is `null` when absent. In a present position, inapplicable location
  fields are `null`, not fabricated zeros.
- `reading_stats` can exist with daily history but unavailable cumulative totals.
  In that case `used_time_ms`, `read_words`, and their `updated_at` are `null`.
  The entire object is `null` only when neither totals nor days exist.
- Sort books by `filename`, annotations by `(filename, id)`, and days by `day` for
  deterministic output. The plugin can order annotations by their source position.
- Do not expose filesystem-only `cover_file` paths as actionable client URLs.
- Preserve unknown numeric source values; do not coerce them into Obsidian enums.

### Reading-data meaning

- `rate`: Moon+ star rating, currently parsed as an integer; zero represents no
  rating. The plugin decides how to format `personalRating`.
- `read_state`: source values currently documented as `0` unread, `1` reading,
  and `3` finished. Do not automatically replace these with percentage-derived
  completion or Obsidian labels.
- `pct`: percentage on a 0-100 scale, not a 0-1 fraction.
- `last_read`: source time, with zero meaning unknown for backup fallback.
- `used_time_ms`: cumulative Moon+ `usedTime`, not a synthesized daily sum.
  The plugin can convert it to `totalMinutes`.
- `read_words`: cumulative Moon+ `readWords`.
- Daily dates remain epoch-day numbers, preserving their source meaning.

There is currently no imported explicit completion date. Daily dates/progress and
reading state provide evidence from which the plugin may derive `readDate`, but
the API must not label the latest reading day as a confirmed completion date.
Verify whether an explicit completion field exists in the documented source format
before adding one. Preserve unavailable values rather than returning invented data.

`category`, `series`, and `series_index` retain lunacy's current interpretation:
the series fields are parsed from Moon+ category, not separate authoritative source
fields. `authors` uses the existing normalized author associations while `author`
retains the current canonical author string. No new genre/tag mapping is imposed.

### `GET /api/v1/book`

Require a non-empty `filename`, supplied as one URL-encoded query value. Treat it
as an exact canonical identifier, not an arbitrary local file to open.

Return:

```json
{
  "book": {},
  "annotations": []
}
```

`book` has exactly the same shape as a sync book record. `annotations` contains
all current annotations for that book, with the same annotation shape.

Use one consistent snapshot. This endpoint is useful for inspecting or explicitly
refreshing a book, but its response does not advance the global sync watermark.
Return `404` for an unknown or ineligible book, including cache-only stubs and
untouched unread books.

### Covers and deeplinks

Reuse the current cover-serving behavior: imported image when available, otherwise
a generated fallback for an eligible book. Return `404` for an unknown or
ineligible book in the versioned cover endpoint.

The existing `parseHash` in `static/app.js` recognizes
`#/books/<encoded-filename>`. Preserve this route as the v1 deeplink contract and
test its round-trip decoding; if the UI routing changes later, keep this route
working or provide an explicit compatible redirect.

Return service-relative references in `links`, so the plugin resolves them against
its configured service base URL. Require/document a trailing slash on that base
URL; preserve a configured deployment prefix when resolving links.

Do not build absolute URLs from untrusted `Host` or forwarded headers, introduce
a public-URL setting solely for this feature, or embed credentials in links.
Traefik must continue to route both the UI and API at the configured service base.

### Authentication and HTTP behavior

Use the deployment's existing Traefik Basic Authentication for the new routes,
including cover access. No token-validation endpoint is needed: a successful sync
request verifies connectivity and authentication.

Currently the optional application BasicAuth wrapper in `main.go` covers WebDAV,
not the API/UI. Do not claim it protects `/api/v1`. Direct backend access remains a
deployment concern; changing the application authentication model is not part of
this plan.

- Successful data responses: `200`, `Content-Type: application/json`.
- Add `Cache-Control: no-store` to sync/detail responses.
- Malformed input: `400`.
- Unknown book: `404`.
- Internal store/import-related query failure: `500`, logged through the existing
  logger, without a partial success-shaped response.
- Authentication failures are generated by Traefik and may not use the API's JSON
  error format.
- Unsupported mutation methods must not expose a write operation.

Use one shared structured JSON error helper for explicit errors from existing
and versioned API handlers, for example:

```json
{
  "error": {
    "code": "invalid_since",
    "message": "since must be an RFC 3339 timestamp with at most millisecond precision"
  }
}
```

Replace the current plain-text `jsonError` implementation and keep directly
dependent call sites consistent. The helper takes an HTTP status, stable
machine-readable error code, safe public message, and the underlying error for
logging. Set `Content-Type: application/json` before writing the error status.
Keep internal database details in logs rather than public messages.

Existing handlers adopt this error-body shape while preserving their current
status codes and successful response shapes. The UI's `api()` helper currently
checks HTTP status and ignores error bodies, so it does not require a coordinated
parser change. Add regression checks for this intentional contract change.

Do not apply the JSON helper to HTML/static-asset failures or claim that Traefik,
WebDAV, or ServeMux-generated routing/method errors use this shape. The v1 handlers
must distinguish not-found/input errors from store failures before invoking it.

Do not add permissive CORS globally. Prefer the future plugin's native Obsidian
HTTP request mechanism; if browser-style requests require CORS, configure only the
needed origins at Traefik.

## Migration and rollout

### Database baseline

Append migrations; do not edit previously applied entries.

1. Add observation/aggregate timestamps and reading-total storage.
2. Add indexed access for book `sync_updated_at` and annotation `updated_at`.
3. Initialize all pre-existing records to one persisted `baseline_at` captured at
   migration, since historical last-modification times are unavailable.
4. Initialize the synchronization timestamp floor to at least that baseline.
5. Keep missing cumulative statistics unknown until the next backup supplies them.

Backfilling one baseline must be recoverable across interrupted startup. Do not
let a partially completed backfill receive different dates on every restart.
Implement the initialization transactionally and ensure a stored baseline cannot
be confused with an uninitialized zero.

Return `baseline_at` in sync responses to make this limitation explicit:

- No `since`: import all existing eligible books and their annotations.
- `since` earlier than or equal to the baseline: existing eligible records are
  returned; untouched unread books and stubs remain excluded.
- To skip legacy imports, configure the plugin's initial watermark after the
  baseline, normally using a server watermark obtained without applying records.
- A pre-baseline historical cutoff cannot reconstruct which old annotations were
  genuinely edited after that date.

### Existing Obsidian notes

The future plugin may configure/reset its watermark to a chosen timestamp.
After the baseline is skipped, only subsequent observed changes are imported.
An edited old annotation may create a duplicate because unwise and lunacy use
different IDs; manual resolution is explicitly accepted for v1.

Existing book notes need a one-time association with canonical lunacy filenames.
Initial matching/manual selection is a plugin responsibility. Once stored, the
association replaces ongoing title mappings.

No vault files, templates, or local annotations are changed by implementing this
API. Existing unmarked legacy quotes remain outside automatic API migration.

## Implementation sequence

### 1. Establish correctness and performance baselines

- Record import time for a representative backup and database using the existing
  elapsed-time logs. Work on isolated copies, never the running database.
- Measure an initial import, an identical repeat, and a backup with a small known
  set of changes.
- Inspect effective backup/cache precedence and identify every mutation entry
  point before adding timestamps.
- Use small synthetic fixtures in automated tests; do not commit personal backups
  or vault contents.

### 2. Add schema and transactional store operations

- Add migrations, timestamp-floor/baseline state, indexes, and reading totals.
- Introduce shared transaction-scoped upsert helpers rather than duplicating SQL
  across importers.
- Implement one shared export-eligibility predicate and timestamp eligibility
  transitions independently of pre-existing metadata dates.
- Implement null-safe conditional writes and parent aggregate updates.
- Put author-set comparison, mutation, and timestamp advancement in the same store
  transaction; do not delegate current-state comparisons to importers.
- Include read-state changes and explicitly handle the missing cover conflict
  update without clearing a cover when an input does not provide one.
- Make eligibility-transition annotation timestamp updates explicit and atomic.
- Preserve existing ordinary UI queries and successful response shapes.

### 3. Integrate importers

- Preserve source timestamps separately from observation timestamps.
- Persist cumulative source statistics already available from the parser.
- Group related backup mutations and aggregate timestamp writes.
- Retain `.po` precedence and source-aware annotation merging.
- Ensure `.an`, `.po`, and backup paths use the same change-tracking mechanism.
- Keep current deletion behavior without adding tombstones or promising deletion
  discovery. Deletion-only changes are not a v1 synchronization guarantee.
- Surface failures; do not advance sync metadata for rolled-back writes.

### 4. Add versioned read APIs

- Implement the combined snapshot feed and timestamp validation.
- Add full book detail and cover wrappers using existing helpers where possible.
- Replace the existing plain-text API error helper with the shared JSON helper,
  including existing API handler call sites.
- Add typed response structs rather than extending UI-specific structs ad hoc.
- Produce correctly escaped service-relative deeplinks.
- Keep missing arrays/objects and timestamp units consistent with the contract.

### 5. Validate and document operation

- Run focused store/importer/API tests and the repository's vet, lint, and build.
- Compare import performance to the baseline before and after conditional writes
  and batching, under equivalent conditions.
- Exercise the real Traefik route and authentication with a test client.
- Document baseline migration, first full sync, skipping legacy records, watermark
  reset, and the lack of deletion propagation.

## Focused validation cases

### Store and import behavior

- Initial imports set timestamps and preserve source dates.
- Identical reimports preserve all observation and aggregate timestamps.
- Metadata/rating/read-state/author changes affect book discovery.
- Unchanged author sets, including reordered/duplicated input names, do not advance
  timestamps; changed associations and timestamps roll back together on failure.
- A real applied cover-reference/content change advances both book timestamps;
  absent source cover data does not clear an existing cover.
- Position updates with real progress return an eligible book with zero
  annotations; zero-progress unopened rows do not make untouched books eligible.
- Metadata-only changes to untouched unread books remain excluded.
- First reading evidence makes a previously excluded book discoverable even when
  its metadata is older than the watermark.
- Eligibility transitions advance retained annotation timestamps exactly once per
  transition, preserving IDs/content/source dates; ordinary repeated imports do
  not repeat the timestamp backfill.
- Edited annotations retain `id` and `created_at`, advance their update timestamp,
  and make their book discoverable.
- Identical quote text in distinct source annotations retains distinct IDs.
- Nullable position comparisons detect changes correctly.
- Reading-total/day changes affect discovery without resending old annotations.
- Backup fallback does not overwrite `.po`; mixed backup/cache cycles do not erase
  fields absent from a source or continually manufacture changes.
- Failed transactions roll back data and parent timestamps together.
- Migration backfill survives restart and does not invent cumulative totals.

### API and concurrency

- Full sync includes annotated books, books with progress but no annotations, and
  books with reading history or an explicit reading/finished state, once backup
  metadata exists. It excludes untouched unread books and cache-only stubs.
- Evidence in any qualifying source is sufficient, even with `read_state = 0`.
  Positive daily evidence qualifies even if current progress has been reset.
- Stub-to-full transitions with reading evidence expose retained children on the
  next incremental sync; detail and cover requests reject all ineligible books.
- Incremental sync uses inclusive bounds and returns current snapshots.
- Every returned annotation has its owning book in the response.
- Metadata/position-only changes do not return unrelated historical annotations.
- Empty successful responses still return a server watermark.
- Invalid timestamps, empty parameters, future cutoffs, and unknown books produce
  explicit errors.
- Null/empty handling, units, source enums, and IDs match the documented contract.
- Daily output includes `pct` and maps internal `read_time_s` to `read_time_ms`.
- Both existing and v1 API handler errors use the JSON envelope, preserve intended
  statuses, and do not expose internal errors; successful legacy responses remain
  unchanged.
- Filenames containing spaces, slashes, Unicode, `#`, `?`, or `%` round-trip through
  query parameters and deeplinks.
- Concurrent imports cannot commit an update behind an already published
  watermark. Test commits immediately around snapshot establishment, including
  equal-millisecond timestamps.
- Restart and backward-clock cases preserve the published timestamp floor.
- Slow/failed HTTP response delivery does not hold import serialization open.
- Existing UI endpoints retain successful behavior and status codes, with the
  documented intentional change to API error bodies.

### Deterministic concurrency harness

Use a temporary SQLite database, real transaction operations, a controllable clock,
and channels/barriers to force interleavings. Do not rely on arbitrary sleeps,
large fixtures, or the production database.

Provide narrow test-only synchronization checkpoints immediately around timestamp
assignment, writer commit, snapshot establishment, and timestamp-floor publication.
Keep them out of the public API and avoid adding runtime configuration solely for
tests. Use bounded waits to turn a deadlock into a clear test failure.

Exercise both orders:

1. Writer first: pause a writer after assigning its mutation timestamp while its
   write transaction is still open. Start sync and verify it cannot publish a
   snapshot/watermark past that uncommitted mutation. Release the writer; the
   mutation must appear in the resulting response or remain eligible for the next
   response.
2. Sync first: pause sync after snapshot establishment/floor selection but before
   commit. Attempt a writer on a separate connection and verify the writer
   boundary prevents an old-timestamp commit. Finish sync, release the writer,
   then fetch using the published watermark. The committed mutation must appear;
   the earlier snapshot must not include its uncommitted state.

Repeat with a fixed clock so writer and watermark have equal millisecond values;
this directly exercises the inclusive lower bound. Repeat after reopening the
store with a backward clock to verify that the persisted floor, not only memory,
prevents skipped updates. Include independent store instances/connections that do
not share a Go mutex, but use the same database and persisted floor, so a process
mutex cannot conceal missing SQLite transaction serialization.

For network backpressure, use a blocking HTTP response writer or test server:
pause response delivery after the snapshot has been materialized/committed and
verify an import can still commit. Assert snapshot contents and next-sync contents,
not merely that goroutines eventually finish.

### Performance

Measure wall-clock time, changed record count, and database write activity for the
same representative inputs. Comparisons should remain inside existing upserts, not
add one query per field/record.

Measure API response size using the eligible subset, not all library metadata.
The sample library contains thousands of mostly unread books, which must not be
returned merely because a backup imported their metadata. Pagination remains
deferred for the initial version.

Do not assume a speedup or promise a percentage improvement without measurements.
If runtime materially regresses, investigate transaction frequency, parent
timestamp writes, author-set churn, and cover extraction before acceptance.
Keep optimizations directly tied to this change rather than expanding into an
unrelated importer rewrite.

## Completion criteria

- A client can perform a full sync and then use only server watermarks.
- Actual metadata, annotation, reading-position, and statistics changes are
  discoverable; identical inputs are not new changes.
- Books with backup metadata and reading evidence are usable through the API even
  without annotations; untouched unread books and cache-only stubs stay excluded.
- Source identities and dates remain distinct from sync timestamps.
- No editing, pagination, deletion feed, external book-search dependency, or
  Obsidian formatting policy is introduced.
- Migration, concurrent-sync correctness, existing behavior, and import
  performance have been verified.

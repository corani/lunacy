# Moon+ Reader Backup Format (.mrpro)

A `.mrpro` file is a standard ZIP archive. The package name inside is
`com.flyersoft.moonreaderp` (the Pro variant; the free version uses
`com.flyersoft.moonreader`).

## Archive structure

```
com.flyersoft.moonreaderp/
    _names.list          -- index: maps tag number -> original filename
    1.tag  ..  N.tag     -- payload files (see below)
```

### `_names.list`

Plain text, one path per line. Each line corresponds to a `.tag` file:
line 1 -> `1.tag`, line 2 -> `2.tag`, etc. The paths are the original
Android paths the files came from (under `shared_prefs/` or `databases/`).

**Important:** paths in `_names.list` and in all SharedPreferences XML files
use XML entity encoding. Ampersands appear as `&amp;`, quotes as `&quot;`, etc.
These must be unescaped before matching against filesystem paths.

## Tag file contents

All tag files live under `com.flyersoft.moonreaderp/` and are named `<n>.tag`.
Their actual filetype is determined by `_names.list`. The table below lists
the known files from a real backup.

| Tag | Original path | Format | Purpose |
|-----|---------------|--------|---------|
| 1 | `shared_prefs/options1002.xml` | XML SharedPreferences | Global app settings |
| 2 | `shared_prefs/positions10.xml` | XML SharedPreferences | Per-book reading positions |
| 3-13, 22, 29, 35, 36, 42, 47 | `shared_prefs/theme_*.xml` | XML SharedPreferences | Theme configurations |
| 14 | `shared_prefs/theme_*.xml` | XML SharedPreferences | Theme configuration |
| 15 | `shared_prefs/history.txt` | Plain text (one path/line) | Recently-opened book paths |
| 16 | `shared_prefs/favfolders6` | Plain text (one path/line) | Favorite/library folder roots |
| 17 | `shared_prefs/lastBookMark.xml` | XML SharedPreferences | Read count per book path |
| 18 | `shared_prefs/state_filter.xml` | XML SharedPreferences | Read filter state per book |
| 19 | `shared_prefs/network2_gutenberg.org.xml` | XML (CRLF) | Gutenberg catalog config |
| 20 | `shared_prefs/network2_mobileread.com.xml` | XML (CRLF) | MobileRead catalog config |
| 21 | `shared_prefs/dict.intent0` | Plain text | Default dictionary app/activity |
| 23 | `shared_prefs/embryo.xml` | XML SharedPreferences | Layout resource IDs cache |
| 24 | `shared_prefs/pdf_toc_failed.xml` | XML SharedPreferences (empty) | PDFs whose TOC failed to parse |
| 25 | `shared_prefs/ratio.xml` | XML SharedPreferences | Last statistics page index |
| 26 | `shared_prefs/reader_bar_icons` | Custom text | Toolbar icon visibility flags |
| 27 | `shared_prefs/theme_pro*.xml` | XML SharedPreferences | Pro theme config |
| 28 | `shared_prefs/com.flyersoft.moonreaderp_preferences.xml` | XML SharedPreferences | Internal A/B flags |
| 30 | `shared_prefs/isFootnoteEpub.xml` | XML SharedPreferences | Per-filename footnote mode flag |
| 31 | `shared_prefs/search_history.list` | Plain text (one term/line) | Search history terms |
| 32 | `shared_prefs/pdf_words_failed.xml` | XML SharedPreferences | PDFs whose word-count failed |
| 33 | `shared_prefs/sorts/shelf_sort_2_fav_*` | Custom text | Shelf sort order (`path**N`) |
| 34 | `shared_prefs/sdcardBooks*_font.xml` | XML SharedPreferences | Per-book font substitution |
| 37 | `shared_prefs/tts_filter` | Custom text (`old#->#new`) | TTS text substitution rules |
| 38-40 | `shared_prefs/sdcardBooks*_font.xml` | XML SharedPreferences | Per-book font substitution |
| 40 | `shared_prefs/import_failed_list.xml` | XML SharedPreferences | Import failure timestamps |
| 41 | `shared_prefs/select_bar` | Custom text | Contextual toolbar button flags |
| 43 | `shared_prefs/AwOriginVisitLoggerPrefs.xml` | XML SharedPreferences | Analytics/telemetry visit log |
| 44 | `shared_prefs/shelf_search` | Plain text (one term/line) | Shelf/library search history |
| 45 | `shared_prefs/last_fav_index.xml` | XML SharedPreferences | Font substitution map |
| 46 | `shared_prefs/theme_pro*.xml` | XML SharedPreferences | Pro theme config |
| 48 | `shared_prefs/network2_webnovel.xml` | XML (CRLF) | WebNovel catalog config |
| 49 | `shared_prefs/dict_history.list` | Plain text | Dictionary lookup history |
| 50 | `shared_prefs/webdav.xml` | XML SharedPreferences | WebDAV server credentials |
| 51 | `shared_prefs/keycode_event.xml` | XML SharedPreferences (empty) | Hardware key bindings |
| 52 | `shared_prefs/*.epub.r` | Custom text (`old#->#new`) | Per-book text substitution rules |
| 53 | `shared_prefs/names_replacement` | Custom text (`old#->#`) | Global text replacement rules |
| 54 | `shared_prefs/*.epub.r` | Custom text (`old#->#new`) | Per-book text substitution rules |
| 55 | `databases/mrbooks.db` | SQLite 3 | Main book library database |
| 56 | `databases/mrbooks.db-journal` | (empty) | SQLite WAL journal |

## Key file formats

### `positions10.xml` (tag 2) - reading positions

Android SharedPreferences XML. Each `<string>` entry maps a lowercased
file path to a position value in the same format as the `.po` cache files
(see [cache-format.md](cache-format.md)).

```xml
<string name="/sdcard/books/author/book.epub">39@0#32445:100%</string>
```

The position strings use the same EPUB/PDF formats as `.po` files but without
the leading `<deviceID>*` prefix — they start directly with the position data.

File paths in the XML names use `&amp;` for ampersands. They must be HTML-unescaped
before matching against book filenames.

### `options1002.xml` (tag 1) - global settings

Android SharedPreferences XML with hundreds of app settings (fonts, margins,
sync, display options, etc.). Notable field:

| Key | Type | Description |
|-----|------|-------------|
| `statistics_words` | long | Lifetime total words read across all books |

### `history.txt` (tag 15) - recently opened books

Plain text, one absolute device path per line, most-recent first.

### `lastBookMark.xml` (tag 17) - read count

Maps book path to number of times that book has been completed (read-through
count), as `<int>` values.

### `state_filter.xml` (tag 18) - read filter state

Maps book path to an integer read state, as `<int>` values.

| Value | Meaning |
|-------|---------|
| `1`   | Reading (started but not finished) |
| `3`   | Finished |

Books not present in this file have implicit state 0 (unread/unknown). A book
marked `3` should be treated as finished regardless of the recorded progress
percentage — the app lets users mark books finished even at e.g. 99%.

File paths use XML entity encoding (`&amp;` etc.) and must be unescaped.

### `shelf_sort_2_fav_*` (tag 33) - shelf/favorites list

Plain text. Each line: `<absolutePath>**<N>` where `N` is `0` or `1`.

### `reader_bar_icons` (tag 26) - toolbar icon visibility

Custom format, one entry per line: `<index>*<bool>`.

### `tts_filter`, `*.epub.r`, `names_replacement` - text substitution

Custom format for find-and-replace rules applied during TTS or display:

```
<originalText>#->#<replacementText>
```

One rule per line. Replacement text may be empty (deletion rule).

### `dict_history.list` (tag 49), `shelf_search` (tag 44), `search_history.list` (tag 31)

Plain text, one item per line (lookup terms, search queries, etc.).

### `webdav.xml` (tag 50) - WebDAV credentials

```xml
<string name="url">http://...</string>
<string name="user">username</string>
<string name="pass">password</string>
<boolean name="ok" value="true" />
```

---

## SQLite database: `mrbooks.db` (tag 55)

SQLite 3, user version 7. Contains five tables.

### `books` table

The main library catalogue.

```sql
CREATE TABLE books (
    _id           integer primary key autoincrement,
    book          text,    -- display title; may contain "#@#"-delimited edit history
    filename      text,    -- original-case absolute path
    lowerFilename text,    -- lowercased path (for matching)
    author        text,    -- may be multi-author separated by "\n" or ";"
    description   text,    -- book synopsis/blurb (may be empty)
    category      text,    -- encodes series + genre, see below
    thumbFile     text,    -- path to thumbnail image
    coverFile     text,    -- path to cover image
    addTime       text,    -- Unix epoch ms as string
    favorite      text,    -- shelf name: "Read", "Reading", "To Read", or ""
    downloadUrl   text,
    rate          text,    -- user star rating 0-5 as string
    bak1          text,
    bak2          text
);
```

**`book` and `author` fields** may contain `#@#`-delimited edit history. Strip
everything from the first `#@#` to get the current value.

**`author` field** may contain multiple authors separated by newlines (`\n`) or
semicolons (`;`). Comma is also used but is ambiguous with `Last, First` name
format and should not be split on.

**`category` field** encodes series membership and genre:

```
<Series Name>
#<seriesIndex>.0#
<genre>
```

Example: `<Ender's Saga>\n#1.0#\nScience Fiction/Fantasy\n`

**`favorite` field** is the shelf name in the Moon+ "Read Filter" / shelf system:
`"Read"`, `"Reading"`, `"To Read"`, or `""` (unsorted/default).

### `notes` table

Highlights, bookmarks, and annotations. This table mirrors the `.an` cache
files (see [cache-format.md](cache-format.md)) but is stored in the database.

```sql
CREATE TABLE notes (
    _id             integer primary key autoincrement,
    book            text,     -- display title
    filename        text,     -- original-case absolute path
    lowerFilename   text,     -- lowercased path
    lastChapter     NUMERIC,  -- 0-based chapter index
    lastSplitIndex  NUMERIC,  -- sub-split index (usually 0)
    lastPosition    NUMERIC,  -- character offset within chapter
    highlightLength NUMERIC,  -- length in characters
    highlightColor  NUMERIC,  -- Android ARGB packed int (signed)
    time            NUMERIC,  -- Unix epoch ms
    bookmark        text,     -- unused; always empty
    note            text,     -- user-written note (may be empty)
    original        text,     -- the highlighted text
    underline       NUMERIC,  -- 1 = underlined
    strikethrough   NUMERIC,  -- 1 = strikethrough
    bak             text
);
```

The `highlightColor` values and position fields follow the same conventions
as the `.an` files (see [cache-format.md](cache-format.md)).

### `statistics` table

Per-book reading time and word-count history.

```sql
CREATE TABLE statistics (
    _id       integer primary key autoincrement,
    filename  text,     -- original-case absolute path
    usedTime  NUMERIC,  -- total reading time in milliseconds
    readWords NUMERIC,  -- total words read (lifetime)
    dates     text      -- newline-separated per-day records (see below)
);
```

**`dates` field format.** Each non-empty line is one of:

- Bare epoch day: `20456`
- Full record: `20456|3912800@13284 #15.3%`

Full record fields: `<epochDay>|<readTimeMs>@<wordsRead> #<pct>%`

| Field        | Description                                         |
|--------------|-----------------------------------------------------|
| `epochDay`   | Days since 1970-01-01                               |
| `readTimeMs` | Reading time that day in milliseconds               |
| `wordsRead`  | Words read that day                                 |
| `pct`        | Progress percentage at end of session that day      |

Words-per-minute can be derived as `wordsRead / (readTimeMs / 60000)`.

Example: `20461|466705@1949 #100%` = 2026-01-08, 7m 46s, 1949 words, 250 w/m, 100%.

### `tmpbooks` table

Same schema as `books`. Used as a staging area for books being imported.

### `covers2` table

Cache of extracted cover images.

```sql
CREATE TABLE covers2 (
    _id      integer primary key autoincrement,
    filename text,   -- original-case absolute path
    cover    text    -- path to the extracted cover image file
);
```

---

## Cross-check: Cache vs Backup

The cache (`data/Cache/`) and backup (`data/Backup/*.mrpro`) represent two independent
copies of the same data, written at different times. Key findings from cross-checking
sample data:

### Reading positions: `.po` vs `positions10.xml`

- `positions10.xml` is populated in backups and can serve as a fallback source of
  position data when no `.po` file has been pushed yet.
- `.po` files are more authoritative: they carry a filesystem mtime reflecting the
  exact time the user last read, while the backup has no equivalent timestamp.
- When both exist, prefer the `.po` value.

### Annotations: `.an` vs `notes` table

- `.an` files exist only for books that have been annotated since the last device
  sync. The `notes` table in the backup is the only long-term record for books
  no longer on the device.
- PDF annotation counts differ between `.an` (v2 format) and the `notes` table
  because `.an` v2 stores chapter-navigation bookmarks as separate records while
  the DB stores only user-created highlights and notes.

### Read state: `state_filter.xml` vs progress percentage

- `state_filter.xml` is the authoritative source for whether a book is finished.
  A book at 99% may have `state=3` (finished) if the user manually marked it so.
- Treat a book as finished if `state=3` OR `pct >= 100`.

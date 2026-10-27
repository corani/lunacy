# Moon+ Reader Cache File Format

Moon+ Reader writes two types of files directly to `data/Cache/` as reading progresses.
The filename is the book's filename (including its original extension) with an additional
extension appended: `.po` for position/progress, `.an` for annotations and highlights.

`.po` files are created when the user closes a book or switches away. `.an` files are
created only on the first annotation - books with no highlights or bookmarks have no
`.an` file.

---

## `.po` - Reading Position

Plain text, one line, no newline at end. Format varies by book type.

### Structure

```
<deviceID>*<position-data>
```

The `<deviceID>` is a fixed numeric identifier that is the same across all `.po` files
on a given device/installation. It is NOT a per-book timestamp; it does not reflect
when the position was last updated. Its exact meaning is unknown (possibly a device
registration or app install identifier).

The `.po` file's **filesystem mtime** is the best available "last read" timestamp.
Moon+ Reader writes the file in real-time as the user reads, so the mtime is always
current. It is always >= the timestamp of the latest annotation in the corresponding
`.an` file (reading continues after highlights).

### EPUB / MOBI position

```
<deviceID>*<chapter>@<splitIndex>#<charOffset>:<pct>%
```

| Field        | Type   | Description                                               |
|--------------|--------|-----------------------------------------------------------|
| `deviceID`   | int    | Fixed device/installation identifier (same for all books) |
| `chapter`    | int    | 0-based chapter index in the spine                        |
| `splitIndex` | int    | Sub-split within a chapter (0 for most books)             |
| `charOffset` | int    | Character offset within the chapter's text                |
| `pct`        | string | Human-readable percentage (e.g. `100%`, `28.8%`, `0.0%`) |

Example: `1705161250742*39@0#32445:100%`

### PDF position

```
<deviceID>*<page>:<pct>%
```

| Field      | Type   | Description                                               |
|------------|--------|-----------------------------------------------------------|
| `deviceID` | int    | Fixed device/installation identifier (same for all books) |
| `page`     | int    | 1-based page number                                       |
| `pct`      | string | Human-readable percentage                                 |

Example: `1705161250742*314:100%`

### Not-yet-opened books

```
<deviceID>*0@0#0:0.0%
```

---

## `.an` - Annotations and Highlights

Zlib-compressed (`zlib.decompress(data)`) text. After decompression the content is
UTF-8. Two format versions exist; detect by checking whether the first line contains
`#A*`.

Only created when the first annotation is made on a book — books with no highlights
or bookmarks have no `.an` file at all.

### Format v1 (EPUB/MOBI - newline-delimited)

Used for EPUB and MOBI books. Line endings are `\n`.

**File header** (first three lines):

```
<seqID>
indent:<bool>
trim:<bool>
```

| Field    | Values         | Description                                             |
|----------|----------------|---------------------------------------------------------|
| `seqID`  | int            | High-water annotation ID (highest ID written so far)    |
| `indent` | `true`/`false` | Whether indented paragraph style is enabled             |
| `trim`   | `true`/`false` | Whether leading/trailing space trimming is on           |

**Annotation records** - separated by a line containing only `#`:

```
#
<id>
<bookTitle>
<filePath>
<lowerFilePath>
<chapter>
<splitIndex>
<charOffset>
<highlightLength>
<highlightColor>
<timestamp>
<noteText>
<noteText2>
<highlightedText>
<type>
<underline>
<strikethrough>
```

| Field             | Type   | Description                                                    |
|-------------------|--------|----------------------------------------------------------------|
| `id`              | int    | Annotation ID                                                  |
| `bookTitle`       | string | Display title of the book                                      |
| `filePath`        | string | Original path on device (e.g. `/sdcard/Books/...`)             |
| `lowerFilePath`   | string | Same path, lowercased                                          |
| `chapter`         | int    | 0-based chapter index in the spine                             |
| `splitIndex`      | int    | Sub-split within the chapter (0 for most books)                |
| `charOffset`      | int    | Character offset of the highlight start within the chapter     |
| `highlightLength` | int    | Length in characters of the highlighted text                   |
| `highlightColor`  | int    | Android ARGB packed int (32-bit signed)                        |
| `timestamp`       | long   | Unix epoch in milliseconds when the annotation was created     |
| `noteText`        | string | User-written note attached to the highlight (may be empty)     |
| `noteText2`       | string | Secondary note field (appears unused; always empty)            |
| `highlightedText` | string | The actual highlighted text; may contain `<BR>` line-break tags|
| `type`            | int    | `1` = highlight, `0` = bookmark                                |
| `underline`       | int    | `1` = underlined, `0` = not                                    |
| `strikethrough`   | int    | `1` = strikethrough, `0` = not                                 |

**Example (decoded):**

```
454839
indent:false
trim:false
#
1158
Killing Floor
/sdcard/Books/Lee Child/Jack Reacher/01. Killing Floor - Lee Child.epub
/sdcard/books/lee child/jack reacher/01. killing floor - lee child.epub
11
0
5290
361
1996532479
1763907035361


When the unexpected gets dumped on you, don't waste time...
1
0
0
```

---

### Format v2 (PDF - inline-delimited)

Used for PDF books. Records are packed onto fewer lines using inline delimiters.
Detect by checking whether the file contains `#A*`.

**Structure:**

```
<seqID>#A*<id>#A1#<timestamp>#A2#<position>#A3#<length>#A4#<color>#A5#<type>#A6#<note>#A7#<highlightedText>#A8#<chapterLabel>#A@
```

Each record starts after `#A*` and ends at `#A@`. Multiple records may be packed
onto the same physical line. The `<seqID>` value appears once at the very beginning
of the file, before the first `#A*`.

| Token  | Field            | Type   | Description                                               |
|--------|------------------|--------|-----------------------------------------------------------|
| `#A*`  | record separator | -      | Marks the start of a new record                           |
| `#A1#` | `timestamp`      | long   | Unix epoch in milliseconds                                |
| `#A2#` | `position`       | int    | Page number (1-based) for PDF                             |
| `#A3#` | `length`         | int    | Length of highlighted text in characters                  |
| `#A4#` | `color`          | int    | Android ARGB packed int (32-bit signed)                   |
| `#A5#` | `type`           | int    | `1` = highlight, `0` = bookmark                           |
| `#A6#` | `note`           | string | User-written note (may be empty)                          |
| `#A7#` | `highlightedText`| string | The highlighted text (may span multiple lines with `\r\n`)|
| `#A8#` | `chapterLabel`   | string | Chapter/section heading at this position (may be empty)   |
| `#A@`  | record end       | -      | Marks the end of the record                               |

**Example (decoded, one record):**

```
315#A*#9#A1#1765377968047#A2#0#A3#0#A4#0#A5#0#A6##A7##A8#(10/315) Contents#A@
```

This is a bookmark (type=0) at page 9, with chapter label `(10/315) Contents`.

---

### Highlight colors (both formats)

Colors are Android ARGB packed 32-bit integers. Common values:

| Packed int   | Hex          | Appearance     |
|--------------|--------------|----------------|
| `1996532479` | `#7700AAFF`  | Blue (default) |
| `-2004318072`| `#88888888`  | Gray           |

To decode: treat as unsigned 32-bit, then extract `A=(v>>24)&0xFF`, `R=(v>>16)&0xFF`,
`G=(v>>8)&0xFF`, `B=v&0xFF`.

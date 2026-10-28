package store

import (
	"fmt"
	"strings"
)

// Book is the canonical book record used by the web UI.
type Book struct {
	Filename    string  `json:"filename"`
	Title       string  `json:"title"`
	Author      string  `json:"author"`
	Description string  `json:"description"`
	Category    string  `json:"category"`
	Series      string  `json:"series"`
	SeriesIndex float64 `json:"series_index"`
	Favorite    string  `json:"favorite"`
	Rate        int     `json:"rate"`
	AddTime     int64   `json:"add_time"`
	CoverFile   string  `json:"cover_file"`
	IsStub      bool    `json:"is_stub"`
	ReadState   int     `json:"read_state"` // 0=unread, 1=reading, 3=finished

	// Joined fields (populated by list queries)
	Pct        float64 `json:"pct"`
	LastRead   int64   `json:"last_read"`
	AnnotCount int     `json:"annot_count"`
}

// Position is a book's current reading position.
type Position struct {
	Filename   string  `json:"filename"`
	Chapter    *int    `json:"chapter,omitempty"`
	SplitIndex *int    `json:"split_index,omitempty"`
	CharOffset *int    `json:"char_offset,omitempty"`
	Page       *int    `json:"page,omitempty"`
	Pct        float64 `json:"pct"`
	LastRead   int64   `json:"last_read"`
}

// Annotation is a single highlight or bookmark.
type Annotation struct {
	ID            int    `json:"id"`
	AnnID         int    `json:"ann_id"`
	Filename      string `json:"filename"`
	Chapter       *int   `json:"chapter,omitempty"`
	SplitIndex    *int   `json:"split_index,omitempty"`
	CharOffset    *int   `json:"char_offset,omitempty"`
	Page          *int   `json:"page,omitempty"`
	Length        int    `json:"length"`
	Color         int64  `json:"color"`
	CreatedAt     int64  `json:"created_at"`
	Type          int    `json:"type"`
	Underline     int    `json:"underline"`
	Strikethrough int    `json:"strikethrough"`
	Note          string `json:"note"`
	Text          string `json:"text"`
	ChapterLabel  string `json:"chapter_label"`
}

// ReadingDay is one entry in a book's reading history.
type ReadingDay struct {
	Day        int     `json:"day"`
	ReadTimeMs int64   `json:"read_time_ms"`
	WordsRead  int64   `json:"words_read"`
	Pct        float64 `json:"pct"`
}

// AuthorSummary aggregates books by author.
type AuthorSummary struct {
	Author    string `json:"author"`
	BookCount int    `json:"book_count"`
}

// SeriesSummary aggregates books by series.
type SeriesSummary struct {
	Series    string `json:"series"`
	BookCount int    `json:"book_count"`
}

// Stats holds global reading statistics.
type Stats struct {
	TotalBooks  int   `json:"total_books"`
	TotalWords  int64 `json:"total_words"`
	ReadingDays int   `json:"reading_days"`
}

// UpsertReadState updates only the read_state field for a book.
func (s *Store) UpsertReadState(filename string, state int) error {
	_, err := s.db.Exec(`UPDATE books SET read_state=? WHERE filename=?`, state, filename)
	return err
}

// UpsertBook inserts or updates a book row.
func (s *Store) UpsertBook(b Book) error {
	_, err := s.db.Exec(`
		INSERT INTO books (filename, title, author, description, category, series, series_index,
		                   favorite, rate, add_time, cover_file, is_stub)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(filename) DO UPDATE SET
		  title        = CASE WHEN excluded.is_stub = 0 THEN excluded.title        ELSE title END,
		  author       = CASE WHEN excluded.is_stub = 0 THEN excluded.author       ELSE author END,
		  description  = CASE WHEN excluded.is_stub = 0 THEN excluded.description  ELSE description END,
		  category     = CASE WHEN excluded.is_stub = 0 THEN excluded.category     ELSE category END,
		  series       = CASE WHEN excluded.is_stub = 0 THEN excluded.series       ELSE series END,
		  series_index = CASE WHEN excluded.is_stub = 0 THEN excluded.series_index ELSE series_index END,
		  favorite     = CASE WHEN excluded.is_stub = 0 THEN excluded.favorite     ELSE favorite END,
		  rate         = CASE WHEN excluded.is_stub = 0 THEN excluded.rate         ELSE rate END,
		  add_time     = CASE WHEN excluded.is_stub = 0 THEN excluded.add_time     ELSE add_time END,
		  is_stub      = excluded.is_stub`,
		b.Filename, b.Title, b.Author, b.Description, b.Category, b.Series, b.SeriesIndex,
		b.Favorite, b.Rate, b.AddTime, b.CoverFile, boolToInt(b.IsStub))
	return err
}

// UpsertPosition inserts or updates a position row.
func (s *Store) UpsertPosition(p Position) error {
	_, err := s.db.Exec(`
		INSERT INTO positions (filename, chapter, split_index, char_offset, page, pct, last_read)
		VALUES (?,?,?,?,?,?,?)
		ON CONFLICT(filename) DO UPDATE SET
		  chapter=excluded.chapter, split_index=excluded.split_index,
		  char_offset=excluded.char_offset, page=excluded.page,
		  pct=excluded.pct, last_read=excluded.last_read`,
		p.Filename, p.Chapter, p.SplitIndex, p.CharOffset, p.Page, p.Pct, p.LastRead)
	return err
}

// UpsertPositionIfEmpty inserts a position row only when no row exists yet or
// when the existing row has last_read=0 (i.e. only backup-sourced data, no real .po).
func (s *Store) UpsertPositionIfEmpty(filename string, p Position) error {
	_, err := s.db.Exec(`
		INSERT INTO positions (filename, chapter, split_index, char_offset, page, pct, last_read)
		VALUES (?,?,?,?,?,?,?)
		ON CONFLICT(filename) DO UPDATE SET
		  chapter=excluded.chapter, split_index=excluded.split_index,
		  char_offset=excluded.char_offset, page=excluded.page,
		  pct=excluded.pct, last_read=excluded.last_read
		WHERE last_read=0`,
		p.Filename, p.Chapter, p.SplitIndex, p.CharOffset, p.Page, p.Pct, p.LastRead)
	return err
}

// UpsertAnnotation inserts or updates an annotation row.
func (s *Store) UpsertAnnotation(a Annotation) error {
	_, err := s.db.Exec(`
		INSERT INTO annotations
		  (filename, ann_id, chapter, split_index, char_offset, page, length, color,
		   created_at, ann_type, underline, strikethrough, note, text, chapter_label)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(filename, ann_id) DO UPDATE SET
		  chapter=excluded.chapter, split_index=excluded.split_index,
		  char_offset=excluded.char_offset, page=excluded.page,
		  length=excluded.length, color=excluded.color,
		  created_at=excluded.created_at, ann_type=excluded.ann_type,
		  underline=excluded.underline, strikethrough=excluded.strikethrough,
		  note=excluded.note, text=excluded.text, chapter_label=excluded.chapter_label`,
		a.Filename, a.AnnID, a.Chapter, a.SplitIndex, a.CharOffset, a.Page,
		a.Length, a.Color, a.CreatedAt, a.Type, a.Underline, a.Strikethrough,
		a.Note, a.Text, a.ChapterLabel)
	return err
}

// FindFilenameByBasename looks up a book's full filename by matching the
// basename of its lowerFilename. Returns "" if not found or ambiguous.
func (s *Store) FindFilenameByBasename(basename string) (string, error) {
	var filename string
	err := s.db.QueryRow(
		`SELECT filename FROM books WHERE filename LIKE ? AND is_stub=0 LIMIT 1`,
		"%/"+basename,
	).Scan(&filename)
	if err != nil {
		return "", nil // not found — caller will create stub
	}
	return filename, nil
}

func (s *Store) UpsertReadingDay(filename string, day int, readTimeMs, wordsRead int64, pct float64) error {
	_, err := s.db.Exec(`
		INSERT INTO reading_days (filename, day, read_time_s, words_read, pct)
		VALUES (?,?,?,?,?)
		ON CONFLICT(filename, day) DO UPDATE SET
		  read_time_s=excluded.read_time_s, words_read=excluded.words_read, pct=excluded.pct`,
		filename, day, readTimeMs, wordsRead, pct)
	return err
}

// DeleteBooksNotIn removes books (and cascade-deletes their children) whose
// filename is not in the provided set.
func (s *Store) DeleteBooksNotIn(filenames []string) error {
	if len(filenames) == 0 {
		_, err := s.db.Exec(`DELETE FROM books`)
		return err
	}
	placeholders := strings.Repeat("?,", len(filenames))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, len(filenames))
	for i, f := range filenames {
		args[i] = f
	}
	_, err := s.db.Exec(`DELETE FROM books WHERE filename NOT IN (`+placeholders+`)`, args...)
	return err
}

// DeleteAnnotationsByFile removes all annotations for a file (used before full re-import).
func (s *Store) DeleteAnnotationsByFile(filename string) error {
	_, err := s.db.Exec(`DELETE FROM annotations WHERE filename=?`, filename)
	return err
}

// BookExists reports whether a book with the given filename exists.
func (s *Store) BookExists(filename string) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM books WHERE filename=?`, filename).Scan(&n)
	return n > 0, err
}

// ListBooks returns a paginated book list with joined position and annotation count.
func (s *Store) ListBooks(shelf, author, series, search, sort, dir string, limit, offset int) ([]Book, error) {
	where, args := buildBookFilter(shelf, author, series, search)

	d := "DESC"
	if dir == "asc" {
		d = "ASC"
	}
	recentExpr := "COALESCE(NULLIF(p.last_read,0), MAX(rd.day)*86400000, b.add_time)"
	orderBy := recentExpr + " " + d
	switch sort {
	case "title":
		orderBy = "b.title " + d
	case "author":
		orderBy = "b.author " + d
	case "progress":
		orderBy = "COALESCE(p.pct,0) " + d
	}

	query := fmt.Sprintf(`
		SELECT b.filename, b.title, b.author, b.description, b.category, b.series, b.series_index,
		       b.favorite, b.rate, b.add_time, b.cover_file, b.is_stub, b.read_state,
		       COALESCE(p.pct,0), COALESCE(p.last_read,0),
		       COUNT(DISTINCT a.id)
		FROM books b
		LEFT JOIN positions p ON p.filename=b.filename
		LEFT JOIN annotations a ON a.filename=b.filename
		LEFT JOIN reading_days rd ON rd.filename=b.filename
		%s
		GROUP BY b.filename
		ORDER BY %s
		LIMIT ? OFFSET ?`, where, orderBy)

	args = append(args, limit, offset)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck
	var books []Book
	for rows.Next() {
		var b Book
		var isStub int
		if err := rows.Scan(&b.Filename, &b.Title, &b.Author, &b.Description, &b.Category,
			&b.Series, &b.SeriesIndex, &b.Favorite, &b.Rate, &b.AddTime,
			&b.CoverFile, &isStub, &b.ReadState, &b.Pct, &b.LastRead, &b.AnnotCount); err != nil {
			return nil, err
		}
		b.IsStub = isStub != 0
		books = append(books, b)
	}
	return books, rows.Err()
}

// GetBook returns a single book by filename.
func (s *Store) GetBook(filename string) (Book, error) {
	var b Book
	var isStub int
	err := s.db.QueryRow(`
		SELECT b.filename, b.title, b.author, b.description, b.category, b.series, b.series_index,
		       b.favorite, b.rate, b.add_time, b.cover_file, b.is_stub, b.read_state,
		       COALESCE(p.pct,0), COALESCE(p.last_read,0), COUNT(a.id)
		FROM books b
		LEFT JOIN positions p ON p.filename=b.filename
		LEFT JOIN annotations a ON a.filename=b.filename
		WHERE b.filename=?
		GROUP BY b.filename`, filename).
		Scan(&b.Filename, &b.Title, &b.Author, &b.Description, &b.Category, &b.Series, &b.SeriesIndex,
			&b.Favorite, &b.Rate, &b.AddTime, &b.CoverFile, &isStub, &b.ReadState,
			&b.Pct, &b.LastRead, &b.AnnotCount)
	b.IsStub = isStub != 0
	return b, err
}

// GetAnnotations returns all annotations for a book ordered by chapter/position.
func (s *Store) GetAnnotations(filename string) ([]Annotation, error) {
	rows, err := s.db.Query(`
		SELECT id, ann_id, filename, chapter, split_index, char_offset, page,
		       length, color, created_at, ann_type, underline, strikethrough,
		       note, text, chapter_label
		FROM annotations
		WHERE filename=?
		ORDER BY COALESCE(chapter,0), COALESCE(char_offset,0), COALESCE(page,0)`,
		filename)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck
	var anns []Annotation
	for rows.Next() {
		var a Annotation
		if err := rows.Scan(&a.ID, &a.AnnID, &a.Filename, &a.Chapter, &a.SplitIndex,
			&a.CharOffset, &a.Page, &a.Length, &a.Color, &a.CreatedAt,
			&a.Type, &a.Underline, &a.Strikethrough, &a.Note, &a.Text, &a.ChapterLabel); err != nil {
			return nil, err
		}
		anns = append(anns, a)
	}
	return anns, rows.Err()
}

// GetReadingDays returns the reading history for a book.
func (s *Store) GetReadingDays(filename string) ([]ReadingDay, error) {
	rows, err := s.db.Query(`
		SELECT day, read_time_s, words_read, pct FROM reading_days
		WHERE filename=? ORDER BY day`, filename)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck
	var days []ReadingDay
	for rows.Next() {
		var d ReadingDay
		if err := rows.Scan(&d.Day, &d.ReadTimeMs, &d.WordsRead, &d.Pct); err != nil {
			return nil, err
		}
		days = append(days, d)
	}
	return days, rows.Err()
}

// UpsertBookAuthors replaces all author entries for a book.
func (s *Store) UpsertBookAuthors(filename string, authors []string) error {
	_, err := s.db.Exec(`DELETE FROM book_authors WHERE filename=?`, filename)
	if err != nil {
		return err
	}
	for _, a := range authors {
		if a == "" {
			continue
		}
		if _, err := s.db.Exec(`INSERT OR IGNORE INTO book_authors (filename, author) VALUES (?,?)`, filename, a); err != nil {
			return err
		}
	}
	return nil
}

// ListAuthors returns all distinct authors with book counts, from the join table.
func (s *Store) ListAuthors() ([]AuthorSummary, error) {
	rows, err := s.db.Query(`
		SELECT author, COUNT(*) FROM book_authors
		GROUP BY author ORDER BY author`)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck
	var out []AuthorSummary
	for rows.Next() {
		var a AuthorSummary
		if err := rows.Scan(&a.Author, &a.BookCount); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ListSeries returns all series with book counts.
func (s *Store) ListSeries() ([]SeriesSummary, error) {
	rows, err := s.db.Query(`
		SELECT series, COUNT(*) FROM books
		WHERE series != '' GROUP BY series ORDER BY series`)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck
	var out []SeriesSummary
	for rows.Next() {
		var ss SeriesSummary
		if err := rows.Scan(&ss.Series, &ss.BookCount); err != nil {
			return nil, err
		}
		out = append(out, ss)
	}
	return out, rows.Err()
}

// GetStats returns global stats.
func (s *Store) GetStats() (Stats, error) {
	var st Stats
	err := s.db.QueryRow(`SELECT COUNT(*) FROM books`).Scan(&st.TotalBooks)
	if err != nil {
		return st, err
	}
	err = s.db.QueryRow(`SELECT COALESCE(SUM(words_read),0) FROM reading_days`).Scan(&st.TotalWords)
	if err != nil {
		return st, err
	}
	err = s.db.QueryRow(`SELECT COUNT(DISTINCT day) FROM reading_days`).Scan(&st.ReadingDays)
	return st, err
}

func buildBookFilter(shelf, author, series, search string) (string, []any) {
	var clauses []string
	var args []any
	if shelf != "" {
		clauses = append(clauses, "b.favorite=?")
		args = append(args, shelf)
	}
	if author != "" {
		clauses = append(clauses, "EXISTS (SELECT 1 FROM book_authors ba WHERE ba.filename=b.filename AND ba.author=?)")
		args = append(args, author)
	}
	if series != "" {
		clauses = append(clauses, "b.series=?")
		args = append(args, series)
	}
	if search != "" {
		for word := range strings.FieldsSeq(search) {
			w := "%" + word + "%"
			clauses = append(clauses, "(b.title LIKE ? OR b.author LIKE ? OR b.series LIKE ?)")
			args = append(args, w, w, w)
		}
	}
	if len(clauses) == 0 {
		return "", args
	}
	return "WHERE " + strings.Join(clauses, " AND "), args
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

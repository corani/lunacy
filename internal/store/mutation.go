package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"slices"
	"strings"
)

// Mutation applies one book's prepared source records atomically.
type Mutation struct {
	conn     *sql.Conn
	ctx      context.Context
	filename string
	at       int64
	metadata bool
	changed  bool
}

const eligible = `b.is_stub=0 AND (
	b.read_state IN (1,3)
	OR EXISTS (SELECT 1 FROM annotations a WHERE a.filename=b.filename)
	OR EXISTS (SELECT 1 FROM positions p WHERE p.filename=b.filename AND p.pct>0)
	OR EXISTS (SELECT 1 FROM reading_totals t WHERE t.filename=b.filename AND (t.used_time_ms>0 OR t.read_words>0))
	OR EXISTS (SELECT 1 FROM reading_days d WHERE d.filename=b.filename AND (d.read_time_s>0 OR d.words_read>0 OR d.pct>0))
)`

func (s *Store) transaction(ctx context.Context, fn func(*sql.Conn) error) error {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close() //nolint:errcheck
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return err
	}
	// Rollback must also work after request cancellation.
	defer conn.ExecContext(context.WithoutCancel(ctx), "ROLLBACK") //nolint:errcheck
	if err := fn(conn); err != nil {
		return err
	}
	_, err = conn.ExecContext(ctx, "COMMIT")
	return err
}

func isEligible(ctx context.Context, conn *sql.Conn, filename string) (bool, error) {
	var ok bool
	err := conn.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM books b WHERE b.filename=? AND `+eligible+`)`, filename).Scan(&ok)
	return ok, err
}

func (s *Store) timestamp(ctx context.Context, conn *sql.Conn) (int64, error) {
	var floor int64
	if err := conn.QueryRowContext(ctx, `SELECT timestamp_floor FROM sync_state WHERE id=1`).Scan(&floor); err != nil {
		return 0, err
	}
	return max(s.now().UnixMilli(), floor), nil
}

func (s *Store) MutateBook(ctx context.Context, filename string, fn func(*Mutation) error) error {
	return s.transaction(ctx, func(conn *sql.Conn) error {
		before, err := isEligible(ctx, conn, filename)
		if err != nil {
			return err
		}
		at, err := s.timestamp(ctx, conn)
		if err != nil {
			return err
		}
		m := &Mutation{conn: conn, ctx: ctx, filename: filename, at: at}
		if err := fn(m); err != nil {
			return err
		}
		after, err := isEligible(ctx, conn, filename)
		if err != nil {
			return err
		}
		if !before && after {
			if _, err := conn.ExecContext(ctx, `UPDATE annotations SET updated_at=? WHERE filename=? AND updated_at<?`, at, filename, at); err != nil {
				return err
			}
			m.changed = true
		}
		if m.changed {
			if _, err := conn.ExecContext(ctx, `UPDATE books SET sync_updated_at=?, updated_at=CASE WHEN ? THEN ? ELSE updated_at END WHERE filename=?`, at, m.metadata, at, filename); err != nil {
				return err
			}
			_, err = conn.ExecContext(ctx, `UPDATE sync_state SET timestamp_floor=? WHERE id=1`, at)
		}
		return err
	})
}

// upsert compares effective exported values in SQL. Table/column names are
// private constants at the call sites, never user-provided identifiers.
func (m *Mutation) upsert(table string, keys, columns []string, values []any, expressions map[string]string, condition string, metadata bool) error {
	all := append(slices.Clone(keys), columns...)
	all = append(all, "updated_at")
	args := append(slices.Clone(values), m.at)
	var sets, diffs []string
	for _, col := range columns {
		expr := "excluded." + col
		if v, ok := expressions[col]; ok {
			expr = v
		}
		sets = append(sets, col+"="+expr)
		diffs = append(diffs, col+" IS NOT ("+expr+")")
	}
	sets = append(sets, "updated_at=excluded.updated_at")
	where := "(" + strings.Join(diffs, " OR ") + ")"
	if condition != "" {
		where += " AND (" + condition + ")"
	}
	query := "INSERT INTO " + table + " (" + strings.Join(all, ",") + ") VALUES (" +
		strings.TrimSuffix(strings.Repeat("?,", len(all)), ",") + ") ON CONFLICT(" +
		strings.Join(keys, ",") + ") DO UPDATE SET " + strings.Join(sets, ",") + " WHERE " + where
	result, err := m.conn.ExecContext(m.ctx, query, args...)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if n > 0 {
		m.changed = true
		m.metadata = m.metadata || metadata
	}
	return err
}

func (m *Mutation) Book(b Book) error {
	if b.Filename != m.filename {
		return fmt.Errorf("book filename does not match mutation")
	}
	cols := []string{"title", "author", "description", "category", "series", "series_index", "favorite", "rate", "add_time", "cover_file", "is_stub"}
	exprs := make(map[string]string)
	for _, col := range cols {
		exprs[col] = "CASE WHEN excluded.is_stub=0 THEN excluded." + col + " ELSE " + col + " END"
	}
	exprs["is_stub"] = "MIN(is_stub,excluded.is_stub)"
	exprs["cover_file"] = "CASE WHEN excluded.cover_file!='' THEN excluded.cover_file ELSE cover_file END"
	return m.upsert("books", []string{"filename"}, cols,
		[]any{b.Filename, b.Title, b.Author, b.Description, b.Category, b.Series, b.SeriesIndex, b.Favorite, b.Rate, b.AddTime, b.CoverFile, boolToInt(b.IsStub)}, exprs, "", true)
}

// Cover applies an explicit server-readable cover, or an explicit removal.
// Read and hash the bytes before entering MutateBook, not while holding its lock.
type Cover struct {
	File string
	Hash string
}

func PrepareCover(path string) (Cover, error) {
	if path == "" {
		return Cover{}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Cover{}, err
	}
	hash := sha256.Sum256(data)
	return Cover{File: path, Hash: hex.EncodeToString(hash[:])}, nil
}

func (m *Mutation) Cover(c Cover) error {
	result, err := m.conn.ExecContext(m.ctx, `UPDATE books SET cover_file=?, cover_hash=? WHERE filename=? AND (cover_file IS NOT ? OR cover_hash IS NOT ?)`, c.File, c.Hash, m.filename, c.File, c.Hash)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if n > 0 {
		m.changed, m.metadata = true, true
	}
	return err
}

func (m *Mutation) ReadState(state int) error {
	result, err := m.conn.ExecContext(m.ctx, `UPDATE books SET read_state=? WHERE filename=? AND read_state IS NOT ?`, state, m.filename, state)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if n > 0 {
		m.changed, m.metadata = true, true
	}
	return err
}

func (m *Mutation) Position(p Position, fallback bool) error {
	if p.Filename != m.filename {
		return fmt.Errorf("position filename does not match mutation")
	}
	condition := ""
	if fallback {
		condition = "last_read=0"
	}
	return m.upsert("positions", []string{"filename"}, []string{"chapter", "split_index", "char_offset", "page", "pct", "last_read"},
		[]any{p.Filename, p.Chapter, p.SplitIndex, p.CharOffset, p.Page, p.Pct, p.LastRead}, nil, condition, false)
}

func (m *Mutation) Annotation(a Annotation, backup bool) error {
	if a.Filename != m.filename {
		return fmt.Errorf("annotation filename does not match mutation")
	}
	exprs := map[string]string(nil)
	if backup {
		exprs = map[string]string{
			"page": "page", "ann_type": "ann_type", "chapter_label": "chapter_label",
			"chapter":     "CASE WHEN page IS NULL THEN excluded.chapter ELSE chapter END",
			"split_index": "CASE WHEN page IS NULL THEN excluded.split_index ELSE split_index END",
			"char_offset": "CASE WHEN page IS NULL THEN excluded.char_offset ELSE char_offset END",
		}
	}
	return m.upsert("annotations", []string{"filename", "ann_id"},
		[]string{"chapter", "split_index", "char_offset", "page", "length", "color", "created_at", "ann_type", "underline", "strikethrough", "note", "text", "chapter_label"},
		[]any{a.Filename, a.AnnID, a.Chapter, a.SplitIndex, a.CharOffset, a.Page, a.Length, a.Color, a.CreatedAt, a.Type, a.Underline, a.Strikethrough, a.Note, a.Text, a.ChapterLabel}, exprs, "", false)
}

func (m *Mutation) ReadingDay(d ReadingDay) error {
	return m.upsert("reading_days", []string{"filename", "day"}, []string{"read_time_s", "words_read", "pct"},
		[]any{m.filename, d.Day, d.ReadTimeMs, d.WordsRead, d.Pct}, nil, "", false)
}

func (m *Mutation) ReadingTotals(usedTimeMs, words int64) error {
	return m.upsert("reading_totals", []string{"filename"}, []string{"used_time_ms", "read_words"},
		[]any{m.filename, usedTimeMs, words}, nil, "", false)
}

func (m *Mutation) Authors(authors []string) error {
	set := make(map[string]bool)
	for _, a := range authors {
		if a != "" {
			set[a] = true
		}
	}
	next := make([]string, 0, len(set))
	for a := range set {
		next = append(next, a)
	}
	slices.Sort(next)
	rows, err := m.conn.QueryContext(m.ctx, `SELECT author FROM book_authors WHERE filename=? ORDER BY author`, m.filename)
	if err != nil {
		return err
	}
	current := []string{}
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			rows.Close() //nolint:errcheck
			return err
		}
		current = append(current, a)
	}
	err = rows.Err()
	rows.Close() //nolint:errcheck
	if err != nil {
		return err
	}
	if slices.Equal(current, next) {
		return nil
	}
	if _, err := m.conn.ExecContext(m.ctx, `DELETE FROM book_authors WHERE filename=?`, m.filename); err != nil {
		return err
	}
	for _, a := range next {
		if _, err := m.conn.ExecContext(m.ctx, `INSERT INTO book_authors(filename,author) VALUES (?,?)`, m.filename, a); err != nil {
			return err
		}
	}
	m.changed, m.metadata = true, true
	return nil
}

package store

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"time"
)

var ErrFutureSince = errors.New("since is later than the server watermark")

func syncTime(ms int64) string {
	return time.UnixMilli(ms).UTC().Format("2006-01-02T15:04:05.000Z")
}

type SyncResponse struct {
	Watermark   string           `json:"watermark"`
	BaselineAt  string           `json:"baseline_at"`
	Books       []SyncBook       `json:"books"`
	Annotations []SyncAnnotation `json:"annotations"`
}

type BookResponse struct {
	Book        SyncBook         `json:"book"`
	Annotations []SyncAnnotation `json:"annotations"`
}

type SyncBook struct {
	Filename      string        `json:"filename"`
	Title         string        `json:"title"`
	Author        string        `json:"author"`
	Authors       []string      `json:"authors"`
	Description   string        `json:"description"`
	Category      string        `json:"category"`
	Series        string        `json:"series"`
	SeriesIndex   float64       `json:"series_index"`
	Favorite      string        `json:"favorite"`
	Rate          int           `json:"rate"`
	AddTime       int64         `json:"add_time"`
	ReadState     int           `json:"read_state"`
	AnnotCount    int           `json:"annot_count"`
	UpdatedAt     string        `json:"updated_at"`
	SyncUpdatedAt string        `json:"sync_updated_at"`
	Position      *SyncPosition `json:"position"`
	ReadingStats  *ReadingStats `json:"reading_stats"`
	Links         BookLinks     `json:"links"`
}

type BookLinks struct {
	UI    string `json:"ui"`
	Cover string `json:"cover"`
}

type SyncPosition struct {
	Chapter    *int    `json:"chapter"`
	SplitIndex *int    `json:"split_index"`
	CharOffset *int    `json:"char_offset"`
	Page       *int    `json:"page"`
	Pct        float64 `json:"pct"`
	LastRead   int64   `json:"last_read"`
	UpdatedAt  string  `json:"updated_at"`
}

type ReadingStats struct {
	UsedTimeMs *int64           `json:"used_time_ms"`
	ReadWords  *int64           `json:"read_words"`
	UpdatedAt  *string          `json:"updated_at"`
	Days       []SyncReadingDay `json:"days"`
}

type SyncReadingDay struct {
	ReadingDay
	UpdatedAt string `json:"updated_at"`
}

type SyncAnnotation struct {
	ID            int    `json:"id"`
	AnnID         int    `json:"ann_id"`
	Filename      string `json:"filename"`
	Chapter       *int   `json:"chapter"`
	SplitIndex    *int   `json:"split_index"`
	CharOffset    *int   `json:"char_offset"`
	Page          *int   `json:"page"`
	Length        int    `json:"length"`
	Color         int64  `json:"color"`
	CreatedAt     int64  `json:"created_at"`
	Type          int    `json:"type"`
	Underline     int    `json:"underline"`
	Strikethrough int    `json:"strikethrough"`
	Note          string `json:"note"`
	Text          string `json:"text"`
	ChapterLabel  string `json:"chapter_label"`
	UpdatedAt     string `json:"updated_at"`
}

// Sync materializes the response and publishes its floor under SQLite's writer
// lock. No database transaction remains open while HTTP sends the result.
func (s *Store) Sync(ctx context.Context, since *int64) (SyncResponse, error) {
	response := SyncResponse{Books: []SyncBook{}, Annotations: []SyncAnnotation{}}
	err := s.transaction(ctx, func(conn *sql.Conn) error {
		until, err := s.timestamp(ctx, conn)
		if err != nil {
			return err
		}
		if since != nil && *since > until {
			return ErrFutureSince
		}
		var baseline int64
		if err := conn.QueryRowContext(ctx, `SELECT baseline_at FROM sync_state WHERE id=1`).Scan(&baseline); err != nil {
			return err
		}
		response.Watermark, response.BaselineAt = syncTime(until), syncTime(baseline)
		lower := int64(0)
		if since != nil {
			lower = *since
		}
		rows, err := conn.QueryContext(ctx, `SELECT b.filename FROM books b WHERE `+eligible+`
			AND (b.sync_updated_at BETWEEN ? AND ? OR EXISTS
				(SELECT 1 FROM annotations a WHERE a.filename=b.filename AND a.updated_at BETWEEN ? AND ?))
			ORDER BY b.filename`, lower, until, lower, until)
		if err != nil {
			return err
		}
		filenames := []string{}
		for rows.Next() {
			var filename string
			if err := rows.Scan(&filename); err != nil {
				rows.Close() //nolint:errcheck
				return err
			}
			filenames = append(filenames, filename)
		}
		err = rows.Err()
		rows.Close() //nolint:errcheck
		if err != nil {
			return err
		}
		for _, filename := range filenames {
			book, err := syncBook(ctx, conn, filename)
			if err != nil {
				return err
			}
			anns, err := syncAnnotations(ctx, conn, filename, lower, until)
			if err != nil {
				return err
			}
			response.Books = append(response.Books, book)
			response.Annotations = append(response.Annotations, anns...)
		}
		_, err = conn.ExecContext(ctx, `UPDATE sync_state SET timestamp_floor=? WHERE id=1`, until)
		return err
	})
	return response, err
}

func (s *Store) SyncBook(ctx context.Context, filename string) (BookResponse, error) {
	var response BookResponse
	err := s.transaction(ctx, func(conn *sql.Conn) error {
		var err error
		response.Book, err = syncBook(ctx, conn, filename)
		if err != nil {
			return err
		}
		response.Annotations, err = syncAnnotations(ctx, conn, filename, 0, 1<<63-1)
		return err
	})
	return response, err
}

// ExportedCover uses the same eligibility rule without exposing arbitrary paths.
func (s *Store) ExportedCover(ctx context.Context, filename string) (Book, error) {
	var book Book
	err := s.db.QueryRowContext(ctx, `SELECT b.filename,b.title,b.author,b.cover_file FROM books b
		WHERE b.filename=? AND `+eligible, filename).Scan(&book.Filename, &book.Title, &book.Author, &book.CoverFile)
	return book, err
}

func syncBook(ctx context.Context, conn *sql.Conn, filename string) (SyncBook, error) {
	b := SyncBook{Authors: []string{}}
	var updated, aggregate int64
	err := conn.QueryRowContext(ctx, `SELECT b.filename,b.title,b.author,b.description,b.category,b.series,b.series_index,
		b.favorite,b.rate,b.add_time,b.read_state,b.updated_at,b.sync_updated_at,
		(SELECT COUNT(*) FROM annotations a WHERE a.filename=b.filename)
		FROM books b WHERE b.filename=? AND `+eligible, filename).
		Scan(&b.Filename, &b.Title, &b.Author, &b.Description, &b.Category, &b.Series, &b.SeriesIndex,
			&b.Favorite, &b.Rate, &b.AddTime, &b.ReadState, &updated, &aggregate, &b.AnnotCount)
	if err != nil {
		return b, err
	}
	b.UpdatedAt, b.SyncUpdatedAt = syncTime(updated), syncTime(aggregate)
	// QueryEscape uses '+' for spaces; fragment paths require percent encoding.
	encoded := url.QueryEscape(filename)
	b.Links = BookLinks{UI: "#/books/" + url.PathEscape(filename), Cover: "api/v1/book/cover?filename=" + encoded}
	rows, err := conn.QueryContext(ctx, `SELECT author FROM book_authors WHERE filename=? ORDER BY author`, filename)
	if err != nil {
		return b, err
	}
	for rows.Next() {
		var author string
		if err := rows.Scan(&author); err != nil {
			rows.Close() //nolint:errcheck
			return b, err
		}
		b.Authors = append(b.Authors, author)
	}
	err = rows.Err()
	rows.Close() //nolint:errcheck
	if err != nil {
		return b, err
	}
	var p SyncPosition
	err = conn.QueryRowContext(ctx, `SELECT chapter,split_index,char_offset,page,pct,last_read,updated_at FROM positions WHERE filename=?`, filename).
		Scan(&p.Chapter, &p.SplitIndex, &p.CharOffset, &p.Page, &p.Pct, &p.LastRead, &updated)
	if err == nil {
		p.UpdatedAt = syncTime(updated)
		b.Position = &p
	} else if !errors.Is(err, sql.ErrNoRows) {
		return b, err
	}
	stats := ReadingStats{Days: []SyncReadingDay{}}
	err = conn.QueryRowContext(ctx, `SELECT used_time_ms,read_words,updated_at FROM reading_totals WHERE filename=?`, filename).
		Scan(&stats.UsedTimeMs, &stats.ReadWords, &updated)
	if err == nil {
		at := syncTime(updated)
		stats.UpdatedAt = &at
	} else if !errors.Is(err, sql.ErrNoRows) {
		return b, err
	}
	rows, err = conn.QueryContext(ctx, `SELECT day,read_time_s,words_read,pct,updated_at FROM reading_days WHERE filename=? ORDER BY day`, filename)
	if err != nil {
		return b, err
	}
	for rows.Next() {
		var d SyncReadingDay
		if err := rows.Scan(&d.Day, &d.ReadTimeMs, &d.WordsRead, &d.Pct, &updated); err != nil {
			rows.Close() //nolint:errcheck
			return b, err
		}
		d.UpdatedAt = syncTime(updated)
		stats.Days = append(stats.Days, d)
	}
	err = rows.Err()
	rows.Close() //nolint:errcheck
	if stats.UsedTimeMs != nil || len(stats.Days) > 0 {
		b.ReadingStats = &stats
	}
	return b, err
}

func syncAnnotations(ctx context.Context, conn *sql.Conn, filename string, since, until int64) ([]SyncAnnotation, error) {
	rows, err := conn.QueryContext(ctx, `SELECT id,ann_id,filename,chapter,split_index,char_offset,page,length,color,
		created_at,ann_type,underline,strikethrough,note,text,chapter_label,updated_at FROM annotations
		WHERE filename=? AND updated_at BETWEEN ? AND ? ORDER BY id`, filename, since, until)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck
	anns := []SyncAnnotation{}
	for rows.Next() {
		var a SyncAnnotation
		var at int64
		if err := rows.Scan(&a.ID, &a.AnnID, &a.Filename, &a.Chapter, &a.SplitIndex, &a.CharOffset, &a.Page,
			&a.Length, &a.Color, &a.CreatedAt, &a.Type, &a.Underline, &a.Strikethrough, &a.Note, &a.Text, &a.ChapterLabel, &at); err != nil {
			return nil, err
		}
		a.UpdatedAt = syncTime(at)
		anns = append(anns, a)
	}
	return anns, rows.Err()
}

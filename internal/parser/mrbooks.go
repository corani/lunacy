// Package parser queries a Moon+ Reader mrbooks.db SQLite database.
package parser

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

// MRBook mirrors the books table row in mrbooks.db.
type MRBook struct {
	Filename      string
	LowerFilename string
	Title         string
	Author        string
	Description   string
	Category      string
	Favorite      string
	Rate          string
	AddTime       int64
}

// MRNote mirrors the notes table row in mrbooks.db.
type MRNote struct {
	LowerFilename  string
	LastChapter    int
	LastSplitIndex int
	LastPosition   int
	HighlightLen   int
	HighlightColor int64
	Time           int64
	Note           string
	Original       string
	Underline      int
	Strikethrough  int
}

// MRStat mirrors the statistics table row in mrbooks.db.
type MRStat struct {
	Filename  string
	UsedTime  int64
	ReadWords int64
	Days      []MRStatDay
}

// MRStatDay holds per-day reading data from the dates field.
type MRStatDay struct {
	Day        int     // Unix epoch days since 1970-01-01
	ReadTimeMs int64   // milliseconds read that day
	WordsRead  int64   // words read that day
	Pct        float64 // progress percentage at end of day
}

// MRBooks reads mrbooks.db from a temporary file written to dir.
type MRBooks struct {
	db *sql.DB
}

// OpenMRBooks opens an mrbooks.db from the given raw bytes.
// A temporary file is used because SQLite requires a path.
func OpenMRBooks(data []byte) (*MRBooks, error) {
	tmp, err := os.CreateTemp("", "mrbooks-*.db")
	if err != nil {
		return nil, fmt.Errorf("mrbooks: create temp: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close() //nolint:errcheck
		os.Remove(tmpPath) //nolint:errcheck
		return nil, fmt.Errorf("mrbooks: write temp: %w", err)
	}
	tmp.Close() //nolint:errcheck

	db, err := sql.Open("sqlite", filepath.Clean(tmpPath)+"?mode=ro")
	if err != nil {
		os.Remove(tmpPath) //nolint:errcheck
		return nil, fmt.Errorf("mrbooks: open: %w", err)
	}
	return &MRBooks{db: db}, nil
}

// Close closes the database and removes the temporary file.
func (m *MRBooks) Close() error {
	// Retrieve the path before closing.
	var path string
	_ = m.db.QueryRow("PRAGMA database_list").Scan(nil, nil, &path)
	err := m.db.Close()
	if path != "" {
		os.Remove(path) //nolint:errcheck
	}
	return err
}

// Books returns all rows from the books table.
func (m *MRBooks) Books() ([]MRBook, error) {
	rows, err := m.db.Query(`
		SELECT book, filename, lowerFilename, author, description, category,
		       favorite, rate, addTime
		FROM books`)
	if err != nil {
		return nil, fmt.Errorf("mrbooks: query books: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var books []MRBook
	for rows.Next() {
		var b MRBook
		if err := rows.Scan(&b.Title, &b.Filename, &b.LowerFilename,
			&b.Author, &b.Description, &b.Category, &b.Favorite, &b.Rate, &b.AddTime); err != nil {
			return nil, fmt.Errorf("mrbooks: scan book: %w", err)
		}
		books = append(books, b)
	}
	return books, rows.Err()
}

// Notes returns all rows from the notes table.
func (m *MRBooks) Notes() ([]MRNote, error) {
	rows, err := m.db.Query(`
		SELECT lowerFilename, lastChapter, lastSplitIndex, lastPosition,
		       highlightLength, highlightColor, time, note, original,
		       underline, strikethrough
		FROM notes`)
	if err != nil {
		return nil, fmt.Errorf("mrbooks: query notes: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var notes []MRNote
	for rows.Next() {
		var n MRNote
		if err := rows.Scan(&n.LowerFilename, &n.LastChapter, &n.LastSplitIndex,
			&n.LastPosition, &n.HighlightLen, &n.HighlightColor,
			&n.Time, &n.Note, &n.Original, &n.Underline, &n.Strikethrough); err != nil {
			return nil, fmt.Errorf("mrbooks: scan note: %w", err)
		}
		notes = append(notes, n)
	}
	return notes, rows.Err()
}

// Statistics returns all rows from the statistics table.
func (m *MRBooks) Statistics() ([]MRStat, error) {
	rows, err := m.db.Query(`SELECT filename, usedTime, readWords, dates FROM statistics`)
	if err != nil {
		return nil, fmt.Errorf("mrbooks: query statistics: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var stats []MRStat
	for rows.Next() {
		var s MRStat
		var datesStr string
		if err := rows.Scan(&s.Filename, &s.UsedTime, &s.ReadWords, &datesStr); err != nil {
			return nil, fmt.Errorf("mrbooks: scan stat: %w", err)
		}
		s.Days = parseDays(datesStr)
		stats = append(stats, s)
	}
	return stats, rows.Err()
}

// parseDays parses the dates field. Each non-empty line is either:
//   - bare epoch day:              "20456"
//   - full record:                 "20456|3912800@13284 #15.3%"
//     where fields are day | readTimeMs @ wordsRead  #pct%
func parseDays(s string) []MRStatDay {
	var days []MRStatDay
	for line := range strings.SplitSeq(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		d := MRStatDay{}
		if pipe := strings.IndexByte(line, '|'); pipe >= 0 {
			dayStr := line[:pipe]
			rest := line[pipe+1:]
			day, err := strconv.Atoi(dayStr)
			if err != nil {
				continue
			}
			d.Day = day
			// rest: "<ms>@<words> #<pct>%"
			if before, after, ok := strings.Cut(rest, "@"); ok {
				ms, _ := strconv.ParseInt(before, 10, 64)
				d.ReadTimeMs = ms
				tail := after
				if before, after, ok := strings.Cut(tail, " #"); ok {
					words, _ := strconv.ParseInt(before, 10, 64)
					d.WordsRead = words
					pctStr := strings.TrimSuffix(after, "%")
					d.Pct, _ = strconv.ParseFloat(pctStr, 64)
				} else {
					words, _ := strconv.ParseInt(tail, 10, 64)
					d.WordsRead = words
				}
			}
		} else {
			day, err := strconv.Atoi(line)
			if err != nil {
				continue
			}
			d.Day = day
		}
		days = append(days, d)
	}
	return days
}

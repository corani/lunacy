package importer

import (
	"context"
	"fmt"
	"html"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/corani/lunacy/internal/logger"
	"github.com/corani/lunacy/internal/parser"
	"github.com/corani/lunacy/internal/store"
)

// BackupImporter handles .mrpro file processing.
type BackupImporter struct {
	st       *store.Store
	davRoot  string
	maxCount int
	log      *logger.Logger
}

// NewBackupImporter creates a BackupImporter.
func NewBackupImporter(st *store.Store, davRoot string, maxCount int, log *logger.Logger) *BackupImporter {
	return &BackupImporter{st: st, davRoot: davRoot, maxCount: maxCount, log: log}
}

// Import processes a .mrpro file at path.
func (b *BackupImporter) Import(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("backup import: read %s: %w", path, err)
	}

	bk, err := parser.OpenBackup(data)
	if err != nil {
		return fmt.Errorf("backup import: open: %w", err)
	}

	// Extract and query mrbooks.db.
	dbData, err := bk.Extract("databases/mrbooks.db")
	if err != nil {
		return fmt.Errorf("backup import: extract mrbooks.db: %w", err)
	}
	mrdb, err := parser.OpenMRBooks(dbData)
	if err != nil {
		return fmt.Errorf("backup import: open mrbooks: %w", err)
	}
	defer mrdb.Close() //nolint:errcheck

	books, err := mrdb.Books(ctx)
	if err != nil {
		return fmt.Errorf("backup import: read books: %w", err)
	}
	notes, err := mrdb.Notes(ctx)
	if err != nil {
		return fmt.Errorf("backup import: read notes: %w", err)
	}
	stats, err := mrdb.Statistics(ctx)
	if err != nil {
		return fmt.Errorf("backup import: read stats: %w", err)
	}

	b.log.Info("importing backup", "file", filepath.Base(path), "books", len(books), "notes", len(notes), "stats", len(stats))

	notesByFile := make(map[string][]parser.MRNote)
	for _, n := range notes {
		notesByFile[n.LowerFilename] = append(notesByFile[n.LowerFilename], n)
	}
	statsByFile := make(map[string]parser.MRStat)
	for _, stat := range stats {
		statsByFile[strings.ToLower(stat.Filename)] = stat
	}
	known := make(map[string]bool)
	for _, book := range books {
		known[book.LowerFilename] = true
	}
	positions := make(map[string]store.Position)
	states := make(map[string]int)
	if bk.Has("shared_prefs/positions10.xml") {
		data, err := bk.Extract("shared_prefs/positions10.xml")
		if err != nil {
			return fmt.Errorf("backup import: extract positions: %w", err)
		}
		positions, err = backupPositions(data, known)
		if err != nil {
			return err
		}
	}
	if bk.Has("shared_prefs/state_filter.xml") {
		data, err := bk.Extract("shared_prefs/state_filter.xml")
		if err != nil {
			return fmt.Errorf("backup import: extract read states: %w", err)
		}
		states, err = backupReadStates(data, known)
		if err != nil {
			return err
		}
	}

	filenames := make([]string, 0, len(books))
	annotCount, dayCount := 0, 0
	for _, mb := range books {
		series, seriesIndex := parseCategory(mb.Category)
		title := cleanField(mb.Title)
		author := cleanField(mb.Author)

		sb := store.Book{
			Filename:    mb.LowerFilename,
			Title:       title,
			Author:      author,
			Description: mb.Description,
			Category:    mb.Category,
			Series:      series,
			SeriesIndex: seriesIndex,
			Favorite:    mb.Favorite,
			Rate:        parseRate(mb.Rate),
			AddTime:     mb.AddTime,
		}
		err := b.st.MutateBook(ctx, sb.Filename, func(m *store.Mutation) error {
			if err := m.Book(sb); err != nil {
				return err
			}
			if err := m.Authors(SplitAuthors(mb.Author)); err != nil {
				return err
			}
			for _, n := range notesByFile[sb.Filename] {
				sa := store.Annotation{
					Filename: sb.Filename, AnnID: int(n.Time),
					Chapter: &n.LastChapter, SplitIndex: &n.LastSplitIndex, CharOffset: &n.LastPosition,
					Length: n.HighlightLen, Color: n.HighlightColor, CreatedAt: n.Time, Type: 1,
					Underline: n.Underline, Strikethrough: n.Strikethrough, Note: n.Note, Text: n.Original,
				}
				if err := m.Annotation(sa, true); err != nil {
					return err
				}
				annotCount++
			}
			if stat, ok := statsByFile[sb.Filename]; ok {
				if err := m.ReadingTotals(stat.UsedTime, stat.ReadWords); err != nil {
					return err
				}
				for _, day := range stat.Days {
					if err := m.ReadingDay(store.ReadingDay{Day: day.Day, ReadTimeMs: day.ReadTimeMs, WordsRead: day.WordsRead, Pct: day.Pct}); err != nil {
						return err
					}
					dayCount++
				}
			}
			if p, ok := positions[sb.Filename]; ok {
				if err := m.Position(p, true); err != nil {
					return err
				}
			}
			if state, ok := states[sb.Filename]; ok {
				return m.ReadState(state)
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("backup import: book %s: %w", sb.Filename, err)
		}
		filenames = append(filenames, sb.Filename)
	}
	if err := b.st.DeleteBooksNotIn(ctx, filenames); err != nil {
		return fmt.Errorf("backup import: delete stale books: %w", err)
	}
	b.log.Info("backup imported", "file", filepath.Base(path), "annotations", annotCount, "reading_days", dayCount)

	// Enforce max backup count across all .mrpro files under davRoot.
	if err := b.enforceMaxCount(ctx); err != nil {
		return fmt.Errorf("backup import: enforce max count: %w", err)
	}

	return nil
}

func backupFilename(raw string, known map[string]bool) string {
	full := strings.ToLower(html.UnescapeString(raw))
	base := filepath.Base(full)
	if known[base] {
		return base
	}
	if known[full] {
		return full
	}
	return ""
}

func backupReadStates(data []byte, known map[string]bool) (map[string]int, error) {
	states := make(map[string]int)
	re := regexp.MustCompile(`<int name="([^"]+)" value="(\d+)"`)
	for _, m := range re.FindAllSubmatch(data, -1) {
		filename := backupFilename(string(m[1]), known)
		state, err := strconv.Atoi(string(m[2]))
		if err != nil {
			return nil, fmt.Errorf("backup import: invalid reading state: %w", err)
		}
		if filename != "" {
			states[filename] = state
		}
	}
	return states, nil
}

func backupPositions(data []byte, known map[string]bool) (map[string]store.Position, error) {
	positions := make(map[string]store.Position)
	// Use regexp to avoid XML parser choking on bare '%' in position values.
	re := regexp.MustCompile(`<string name="([^"]+)">([^<]+)</string>`)
	for _, m := range re.FindAllSubmatch(data, -1) {
		filename := backupFilename(string(m[1]), known)
		posStr := string(m[2])
		if filename == "" {
			continue
		}

		pos, err := parser.ParsePO(posStr)
		if err != nil {
			return nil, fmt.Errorf("backup import: position %s: %w", filename, err)
		}
		if pos.IsNull {
			continue
		}

		sp := store.Position{Filename: filename, Pct: pos.Pct, LastRead: 0}
		if pos.IsPDF {
			sp.Page = &pos.Page
		} else {
			sp.Chapter = &pos.Chapter
			sp.SplitIndex = &pos.SplitIndex
			sp.CharOffset = &pos.CharOffset
		}
		positions[filename] = sp
	}
	return positions, nil
}

// enforceMaxCount deletes the oldest .mrpro files when the total count exceeds maxCount.
// It walks the entire davRoot so backups in any subdirectory are counted.
func (b *BackupImporter) enforceMaxCount(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if b.maxCount <= 0 {
		return nil
	}

	type fileInfo struct {
		path    string
		modTime int64
	}
	var files []fileInfo
	err := filepath.WalkDir(b.davRoot, func(path string, d fs.DirEntry, err error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err != nil || d.IsDir() {
			return err
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".mrpro") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		files = append(files, fileInfo{path: path, modTime: info.ModTime().UnixMilli()})
		return nil
	})
	if err != nil {
		return err
	}

	// Sort oldest first.
	for i := 0; i < len(files)-1; i++ {
		for j := i + 1; j < len(files); j++ {
			if files[j].modTime < files[i].modTime {
				files[i], files[j] = files[j], files[i]
			}
		}
	}

	for len(files) > b.maxCount {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := os.Remove(files[0].path); err != nil && !os.IsNotExist(err) {
			return err
		}
		files = files[1:]
	}
	return nil
}

// copyToBackupDir copies src to the backup directory, returning the destination path.
func (b *BackupImporter) CopyToDir(src, destDir string) (string, error) {
	dst := filepath.Join(destDir, filepath.Base(src))
	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer in.Close() //nolint:errcheck

	out, err := os.Create(dst)
	if err != nil {
		return "", err
	}
	defer out.Close() //nolint:errcheck

	if _, err := io.Copy(out, in); err != nil {
		return "", err
	}
	return dst, nil
}

// SplitAuthors splits an author string on newlines and semicolons,
// trims whitespace, and deduplicates. Comma is not used as a separator
// because "Last, First" is a common single-author format.
func SplitAuthors(raw string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == '\n' || r == ';'
	}) {
		a := strings.TrimSpace(part)
		if a != "" && !seen[a] {
			seen[a] = true
			out = append(out, a)
		}
	}
	return out
}

// Format examples: "<Series Name>\n#3.0#\n" or just "Fiction\n".
var categoryRe = regexp.MustCompile(`<([^>]+)>\s*\n#([\d.]+)#`)

func parseCategory(cat string) (series string, index float64) {
	m := categoryRe.FindStringSubmatch(cat)
	if m == nil {
		return "", 0
	}
	f, _ := strconv.ParseFloat(m[2], 64)
	return m[1], f
}

// cleanField strips trailing edit-history markers (separated by #@#).
func cleanField(s string) string {
	if before, _, ok := strings.Cut(s, "#@#"); ok {
		return strings.TrimSpace(before)
	}
	return strings.TrimSpace(s)
}

// parseRate converts the rate string ("0"-"5" or "") to an int.
func parseRate(s string) int {
	v, _ := strconv.Atoi(strings.TrimSpace(s))
	return v
}

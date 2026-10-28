package importer

import (
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
func (b *BackupImporter) Import(path string) error {
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

	books, err := mrdb.Books()
	if err != nil {
		return fmt.Errorf("backup import: read books: %w", err)
	}
	notes, err := mrdb.Notes()
	if err != nil {
		return fmt.Errorf("backup import: read notes: %w", err)
	}
	stats, err := mrdb.Statistics()
	if err != nil {
		return fmt.Errorf("backup import: read stats: %w", err)
	}

	b.log.Info("importing backup", "file", filepath.Base(path), "books", len(books), "notes", len(notes), "stats", len(stats))

	// Sync books (backup is leading).
	filenames := make([]string, 0, len(books))
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
		if err := b.st.UpsertBook(sb); err != nil {
			return fmt.Errorf("backup import: upsert book %s: %w", mb.LowerFilename, err)
		}
		if err := b.st.UpsertBookAuthors(mb.LowerFilename, SplitAuthors(mb.Author)); err != nil {
			return fmt.Errorf("backup import: upsert authors %s: %w", mb.LowerFilename, err)
		}
		filenames = append(filenames, mb.LowerFilename)
	}

	if err := b.st.DeleteBooksNotIn(filenames); err != nil {
		return fmt.Errorf("backup import: delete stale books: %w", err)
	}

	// Import annotations from notes table.
	annotCount := 0
	for _, n := range notes {
		filename := n.LowerFilename
		exists, err := b.st.BookExists(filename)
		if err != nil || !exists {
			continue
		}
		sa := store.Annotation{
			Filename:      filename,
			AnnID:         int(n.Time),
			Chapter:       &n.LastChapter,
			SplitIndex:    &n.LastSplitIndex,
			CharOffset:    &n.LastPosition,
			Length:        n.HighlightLen,
			Color:         n.HighlightColor,
			CreatedAt:     n.Time,
			Type:          1,
			Underline:     n.Underline,
			Strikethrough: n.Strikethrough,
			Note:          n.Note,
			Text:          n.Original,
		}
		if err := b.st.UpsertAnnotation(sa); err != nil {
			return fmt.Errorf("backup import: upsert annotation: %w", err)
		}
		annotCount++
	}

	// Import reading statistics.
	dayCount := 0
	for _, stat := range stats {
		filename := strings.ToLower(stat.Filename)
		exists, err := b.st.BookExists(filename)
		if err != nil || !exists {
			continue
		}
		for _, day := range stat.Days {
			if err := b.st.UpsertReadingDay(filename, day.Day, day.ReadTimeMs, day.WordsRead, day.Pct); err != nil {
				return fmt.Errorf("backup import: upsert reading day: %w", err)
			}
			dayCount++
		}
	}

	b.log.Info("backup imported", "file", filepath.Base(path), "annotations", annotCount, "reading_days", dayCount)

	// Import positions from positions10.xml (fallback; only used when no .po exists).
	if posData, err := bk.Extract("shared_prefs/positions10.xml"); err == nil {
		if err := b.importPositions(posData); err != nil {
			b.log.Error("positions10.xml", "err", err)
		}
	}

	// Import read states from state_filter.xml.
	if stateData, err := bk.Extract("shared_prefs/state_filter.xml"); err == nil {
		if err := b.importReadStates(stateData); err != nil {
			b.log.Error("state_filter.xml", "err", err)
		}
	}

	// Enforce max backup count across all .mrpro files under davRoot.
	if err := b.enforceMaxCount(); err != nil {
		return fmt.Errorf("backup import: enforce max count: %w", err)
	}

	return nil
}

// importReadStates parses state_filter.xml and updates read_state on matching books.
func (b *BackupImporter) importReadStates(data []byte) error {
	re := regexp.MustCompile(`<int name="([^"]+)" value="(\d+)"`)
	for _, m := range re.FindAllSubmatch(data, -1) {
		filename := strings.ToLower(filepath.Base(html.UnescapeString(string(m[1]))))
		state, _ := strconv.Atoi(string(m[2]))

		exists, err := b.st.BookExists(filename)
		if err != nil || !exists {
			full := strings.ToLower(html.UnescapeString(string(m[1])))
			exists, err = b.st.BookExists(full)
			if err != nil || !exists {
				continue
			}
			filename = full
		}
		if err := b.st.UpsertReadState(filename, state); err != nil {
			b.log.Error("read state", "file", filename, "err", err)
		}
	}
	return nil
}

// importPositions parses positions10.xml and upserts positions that have no
// real .po data yet (last_read = 0).
func (b *BackupImporter) importPositions(data []byte) error {
	// Use regexp to avoid XML parser choking on bare '%' in position values.
	re := regexp.MustCompile(`<string name="([^"]+)">([^<]+)</string>`)
	for _, m := range re.FindAllSubmatch(data, -1) {
		path := strings.ToLower(html.UnescapeString(string(m[1])))
		posStr := string(m[2])

		filename := filepath.Base(path)
		exists, err := b.st.BookExists(filename)
		if err != nil || !exists {
			// Try the full lowercase path as filename key.
			exists, err = b.st.BookExists(path)
			if err != nil || !exists {
				continue
			}
			filename = path
		}

		pos, err := parser.ParsePO(posStr)
		if err != nil || pos.IsNull {
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
		if err := b.st.UpsertPositionIfEmpty(filename, sp); err != nil {
			b.log.Error("upsert position", "file", filename, "err", err)
		}
	}
	return nil
}

// enforceMaxCount deletes the oldest .mrpro files when the total count exceeds maxCount.
// It walks the entire davRoot so backups in any subdirectory are counted.
func (b *BackupImporter) enforceMaxCount() error {
	if b.maxCount <= 0 {
		return nil
	}

	type fileInfo struct {
		path    string
		modTime int64
	}
	var files []fileInfo
	err := filepath.WalkDir(b.davRoot, func(path string, d fs.DirEntry, err error) error {
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
	defer in.Close()  //nolint:errcheck

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

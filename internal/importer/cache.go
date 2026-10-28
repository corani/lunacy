package importer

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/corani/lunacy/internal/parser"
	"github.com/corani/lunacy/internal/store"
)

// ImportPosition parses a .po file and upserts the position into the store.
// webdavPath is the path relative to the WebDAV root, e.g. "Cache/book.epub.po".
// mtimeMs is the file's filesystem mtime in Unix milliseconds.
func ImportPosition(st *store.Store, content, webdavPath string, mtimeMs int64) error {
	filename, err := resolveFilename(st, webdavPath, ".po")
	if err != nil {
		return fmt.Errorf("position import: resolve filename: %w", err)
	}
	if filename == "" {
		return nil
	}

	pos, err := parser.ParsePO(content)
	if err != nil {
		return fmt.Errorf("position import %s: %w", webdavPath, err)
	}

	sp := store.Position{
		Filename: filename,
		Pct:      pos.Pct,
		LastRead: mtimeMs,
	}
	if pos.IsPDF {
		sp.Page = &pos.Page
	} else {
		sp.Chapter = &pos.Chapter
		sp.SplitIndex = &pos.SplitIndex
		sp.CharOffset = &pos.CharOffset
	}

	return st.UpsertPosition(sp)
}

// ImportAnnotations parses a .an file and upserts all annotations into the store.
func ImportAnnotations(st *store.Store, content, webdavPath string) error {
	filename, err := resolveFilename(st, webdavPath, ".an")
	if err != nil {
		return fmt.Errorf("annotation import: resolve filename: %w", err)
	}
	if filename == "" {
		return nil
	}

	anns, err := parser.ParseAN(content)
	if err != nil {
		return fmt.Errorf("annotation import %s: %w", webdavPath, err)
	}

	for _, a := range anns {
		sa := store.Annotation{
			Filename:      filename,
			AnnID:         int(a.CreatedAt), // use timestamp as stable cross-source key
			Length:        a.Length,
			Color:         a.Color,
			CreatedAt:     a.CreatedAt,
			Type:          a.Type,
			Underline:     a.Underline,
			Strikethrough: a.Strikethrough,
			Note:          a.Note,
			Text:          a.Text,
			ChapterLabel:  a.ChapterLabel,
		}
		if a.Page != 0 {
			sa.Page = &a.Page
		} else {
			sa.Chapter = &a.Chapter
			sa.SplitIndex = &a.SplitIndex
			sa.CharOffset = &a.CharOffset
		}
		if err := st.UpsertAnnotation(sa); err != nil {
			return fmt.Errorf("annotation import: upsert: %w", err)
		}
	}
	return nil
}

// resolveFilename maps a WebDAV cache path (e.g. "Cache/Book.epub.po") to the
// canonical filename key used in the books table. It first tries to find an
// existing non-stub book whose full path ends with the basename, falling back
// to creating a stub keyed on the bare basename if no match is found.
func resolveFilename(st *store.Store, webdavPath, ext string) (string, error) {
	base := filepath.Base(webdavPath)
	if !strings.HasSuffix(strings.ToLower(base), ext) {
		return "", nil
	}
	basename := strings.ToLower(base[:len(base)-len(ext)])

	// Try to match against an existing book by its full-path basename.
	if full, err := st.FindFilenameByBasename(basename); err != nil {
		return "", err
	} else if full != "" {
		return full, nil
	}

	// No existing book — ensure a stub exists keyed on the bare basename.
	if err := ensureBook(st, basename); err != nil {
		return "", fmt.Errorf("ensure book: %w", err)
	}
	return basename, nil
}

// ensureBook creates a stub book row if one doesn't exist yet.
func ensureBook(st *store.Store, filename string) error {
	exists, err := st.BookExists(filename)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	title := filepath.Base(filename)
	if ext := filepath.Ext(title); ext != "" {
		title = title[:len(title)-len(ext)]
	}
	title = strings.ReplaceAll(title, "_", " ")

	return st.UpsertBook(store.Book{
		Filename: filename,
		Title:    title,
		IsStub:   true,
	})
}

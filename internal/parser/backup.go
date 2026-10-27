// Package parser extracts files from Moon+ Reader .mrpro backup archives.
//
// A .mrpro is a ZIP file containing numbered .tag files plus a _names.list
// index that maps line number (= tag number) to the original Android file path.
package parser

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"strings"
)

// Backup represents an opened .mrpro archive.
type Backup struct {
	// names maps original Android path suffix → zip.File
	files map[string]*zip.File
}

// OpenBackup opens a .mrpro ZIP from a byte slice and builds the name index.
func OpenBackup(data []byte) (*Backup, error) {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("open mrpro zip: %w", err)
	}

	// Build filename → *zip.File map for all .tag files.
	tagByNum := make(map[string]*zip.File)
	var namesFile *zip.File
	for _, f := range r.File {
		name := f.Name
		if strings.HasSuffix(name, "_names.list") {
			namesFile = f
		} else if strings.HasSuffix(name, ".tag") {
			// strip leading path, keep "N.tag"
			base := name[strings.LastIndex(name, "/")+1:]
			tagByNum[base] = f
		}
	}

	if namesFile == nil {
		return nil, fmt.Errorf("mrpro: _names.list not found")
	}

	// Read _names.list: line N (1-based) → original Android path.
	rc, err := namesFile.Open()
	if err != nil {
		return nil, fmt.Errorf("mrpro: open _names.list: %w", err)
	}
	nameBytes, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		return nil, fmt.Errorf("mrpro: read _names.list: %w", err)
	}

	lines := strings.Split(strings.TrimRight(string(nameBytes), "\n"), "\n")
	b := &Backup{files: make(map[string]*zip.File, len(lines))}
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		tagName := fmt.Sprintf("%d.tag", i+1)
		if f, ok := tagByNum[tagName]; ok {
			b.files[line] = f
		}
	}

	return b, nil
}

// Extract returns the raw bytes of the file whose original Android path ends
// with suffix. Returns an error if no match is found.
func (b *Backup) Extract(suffix string) ([]byte, error) {
	for path, f := range b.files {
		if strings.HasSuffix(path, suffix) {
			rc, err := f.Open()
			if err != nil {
				return nil, fmt.Errorf("mrpro: open %s: %w", path, err)
			}
			data, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return nil, fmt.Errorf("mrpro: read %s: %w", path, err)
			}
			return data, nil
		}
	}
	return nil, fmt.Errorf("mrpro: no file with suffix %q", suffix)
}

// Has reports whether the backup contains a file whose path ends with suffix.
func (b *Backup) Has(suffix string) bool {
	for path := range b.files {
		if strings.HasSuffix(path, suffix) {
			return true
		}
	}
	return false
}

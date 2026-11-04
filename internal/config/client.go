package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/parser"
)

// LoadClient requires an existing config and returns its exact resolved path.
func LoadClient(path string) (Config, string, error) {
	if path == "" {
		path = findConfig()
	}
	if path == "" {
		return Config{}, "", errors.New("client requires an existing config.yaml")
	}
	resolved, err := filepath.Abs(path)
	if err != nil {
		return Config{}, "", err
	}
	resolved, err = filepath.EvalSymlinks(resolved)
	if err != nil {
		return Config{}, "", err
	}
	cfg, err := Load(resolved)
	return cfg, resolved, err
}

// SaveWatermark changes only the client cursor in the selected existing config.
// Parsing with comments retains unrelated settings, including credential values.
func SaveWatermark(path, watermark string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	file, err := parser.ParseBytes(data, parser.ParseComments)
	if err != nil {
		return errors.New("invalid config YAML")
	}
	cursor, err := yaml.PathString("$.client.watermark")
	if err != nil {
		return err
	}
	if existing, err := cursor.FilterFile(file); err == nil {
		root, err := yaml.PathString("$")
		if err != nil {
			return err
		}
		replacement, err := root.ReadNode(strings.NewReader(strconv.Quote(watermark)))
		if err != nil {
			return err
		}
		if err := replacement.SetComment(existing.GetComment()); err != nil {
			return err
		}
		err = cursor.ReplaceWithNode(file, replacement)
		if err != nil {
			return errors.New("cannot replace client.watermark")
		}
	} else {
		client, err := yaml.PathString("$.client")
		if err != nil {
			return err
		}
		if err := client.MergeFromReader(file, strings.NewReader("watermark: "+strconv.Quote(watermark))); err != nil {
			return errors.New("cannot add client.watermark; client must be a YAML mapping")
		}
	}
	return AtomicWrite(path, []byte(file.String()+"\n"))
}

// AtomicWrite replaces a file using a same-directory, private staging file.
// Existing permission bits are retained; new files are private.
func AtomicWrite(path string, data []byte) error {
	mode := os.FileMode(0o600)
	if info, err := os.Stat(path); err == nil {
		if !info.Mode().IsRegular() {
			return errors.New("destination is not a regular file")
		}
		mode = info.Mode().Perm()
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name) //nolint:errcheck
	defer f.Close()       //nolint:errcheck
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Chmod(mode); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("replace destination: %w", err)
	}
	return nil
}

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSaveWatermarkPreservesConfig(t *testing.T) {
	dir, err := os.MkdirTemp(".", ".config-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	for _, cursor := range []string{"  watermark: '' # saved cursor\n", ""} {
		t.Run(cursor, func(t *testing.T) {
			path := filepath.Join(dir, "config.yaml")
			source := "# private config\nserver:\n  password: 'do not change # this'\nclient:\n  base_url: 'http://localhost/prefix/'\n  password: \"secret: # value\" # credential\n  timeout: 2s\n" + cursor + "backup:\n  max_count: 42 # retained\n"
			if err := os.WriteFile(path, []byte(source), 0o640); err != nil {
				t.Fatal(err)
			}
			if err := SaveWatermark(path, "2026-11-04T04:30:00.000Z"); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			for _, retained := range []string{"# private config", "password: 'do not change # this'", `password: "secret: # value" # credential`, "max_count: 42 # retained"} {
				if !strings.Contains(string(data), retained) {
					t.Errorf("lost unrelated YAML: %s\n%s", retained, data)
				}
				if cursor != "" && !strings.Contains(string(data), "# saved cursor") {
					t.Fatalf("lost watermark comment:\n%s", data)
				}
			}
			cfg, err := Load(path)
			if err != nil || cfg.Client.Watermark != "2026-11-04T04:30:00.000Z" || cfg.Client.Timeout != 2*time.Second {
				t.Fatalf("unexpected config: watermark=%q timeout=%v err=%v", cfg.Client.Watermark, cfg.Client.Timeout, err)
			}
			info, _ := os.Stat(path)
			if info.Mode().Perm() != 0o640 {
				t.Fatal("permission bits changed")
			}
			matches, _ := filepath.Glob(filepath.Join(dir, ".config.yaml.*"))
			if len(matches) != 0 {
				t.Fatal("staging files not cleaned up")
			}
		})
	}
}

func TestLoadClientResolvesSelectedSymlink(t *testing.T) {
	dir, err := os.MkdirTemp(".", ".config-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	path, _ := filepath.Abs(filepath.Join(dir, "chosen.yaml"))
	if err := os.WriteFile(path, []byte("client:\n  watermark: ''\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.yaml")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	_, selected, err := LoadClient(link)
	if err != nil || selected != path {
		t.Fatalf("selected=%q err=%v", selected, err)
	}
}

func TestSaveFailurePreservesConfig(t *testing.T) {
	dir, err := os.MkdirTemp(".", ".config-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	for _, source := range []string{"client: [broken", "client: null\npassword: 'untouched'\n"} {
		path := filepath.Join(dir, "config.yaml")
		if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := SaveWatermark(path, "2026-11-04T04:30:00.000Z"); err == nil {
			t.Fatal("expected save failure")
		}
		data, _ := os.ReadFile(path)
		if string(data) != source {
			t.Fatal("config changed after failure")
		}
	}
}

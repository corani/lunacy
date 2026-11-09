package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/corani/lunacy/internal/api"
	"github.com/corani/lunacy/internal/config"
	"github.com/corani/lunacy/internal/logger"
	"github.com/corani/lunacy/internal/store"
	"github.com/corani/lunacy/static"
)

func TestCLIRealAPIEndToEnd(t *testing.T) {
	dir, err := os.MkdirTemp(".", ".integration-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	st, err := store.Open(t.Context(), dir, logger.New("cli-integration"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Error(err)
		}
	})
	filename := "/storage/books/a + 书#?%.epub"
	book := store.Book{Filename: filename, Title: "Integration book", Author: "Author", ReadState: 1}
	chapter := 3
	if err := st.MutateBook(context.Background(), filename, func(m *store.Mutation) error {
		if err := m.Book(book); err != nil {
			return err
		}
		if err := m.Authors([]string{"Author", "Second author"}); err != nil {
			return err
		}
		if err := m.Annotation(store.Annotation{
			Filename: filename, AnnID: 1793760000000, CreatedAt: 1793760000000,
			Chapter: &chapter, Type: 1, Text: "A quote", Note: "A note",
		}, false); err != nil {
			return err
		}
		if err := m.Position(store.Position{Filename: filename, Chapter: &chapter, Pct: 27.5, LastRead: 1793765400000}, false); err != nil {
			return err
		}
		if err := m.ReadingTotals(7200000, 24000); err != nil {
			return err
		}
		return m.ReadingDay(store.ReadingDay{Day: 20761, ReadTimeMs: 3600000, WordsRead: 12000, Pct: 27.5})
	}); err != nil {
		t.Fatal(err)
	}
	app := api.New(st, dir, static.FS, logger.New("cli-integration"), "test").App()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || username != "moon" || password != "private-secret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		http.StripPrefix("/deployment", app).ServeHTTP(w, r)
	}))
	defer server.Close()
	path, _ := testConfig(t, server.URL+"/deployment/")
	invoke := func(args ...string) []byte {
		t.Helper()
		var stdout, stderr bytes.Buffer
		if err := run(append([]string{"--config", path}, args...), &stdout, &stderr); err != nil {
			t.Fatal(err)
		}
		if stderr.Len() != 0 {
			t.Fatalf("unexpected diagnostics: %s", &stderr)
		}
		return stdout.Bytes()
	}
	savedConfig := func() []byte {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	assertWatermark := func(want string) {
		t.Helper()
		cfg, err := config.Load(path)
		if err != nil || cfg.Client.Watermark != want {
			t.Fatalf("saved watermark=%q want=%q err=%v", cfg.Client.Watermark, want, err)
		}
	}
	waitAfter := func(value string) {
		t.Helper()
		at, err := timestamp(value)
		if err != nil {
			t.Fatal(err)
		}
		for time.Now().UnixMilli() <= at.UnixMilli() {
			time.Sleep(time.Millisecond)
		}
	}
	initial, err := st.SyncBook(context.Background(), filename)
	if err != nil {
		t.Fatal(err)
	}
	// Separate seed observations from the inclusive first watermark.
	waitAfter(initial.Book.SyncUpdatedAt)
	var full store.SyncResponse
	if err := json.Unmarshal(invoke("sync", "--full"), &full); err != nil {
		t.Fatal(err)
	}
	if len(full.Books) != 1 || len(full.Annotations) != 1 ||
		!reflect.DeepEqual(full.Books[0], initial.Book) || !reflect.DeepEqual(full.Annotations, initial.Annotations) {
		t.Fatalf("full sync differs from real store snapshot: %+v", full)
	}
	assertWatermark(full.Watermark)
	afterFull := savedConfig()
	var detail store.BookResponse
	if err := json.Unmarshal(invoke("book", "--filename", filename), &detail); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(detail.Book, full.Books[0]) || !reflect.DeepEqual(detail.Annotations, full.Annotations) {
		t.Fatal("book detail differs from full sync")
	}
	coverOutput := filepath.Join(dir, "cover.svg")
	if stdout := invoke("cover", "--filename", filename, "--output", coverOutput); len(stdout) != 0 {
		t.Fatal("cover wrote stdout")
	}
	expectedCover := httptest.NewRecorder()
	app.ServeHTTP(expectedCover, httptest.NewRequest(http.MethodGet,
		"/api/v1/book/cover?"+url.Values{"filename": {filename}}.Encode(), nil))
	actualCover, err := os.ReadFile(coverOutput)
	if err != nil || expectedCover.Code != http.StatusOK ||
		expectedCover.Header().Get("Content-Type") != "image/svg+xml" ||
		!bytes.Equal(actualCover, expectedCover.Body.Bytes()) {
		t.Fatalf("CLI cover differs from actual API cover: err=%v status=%d", err, expectedCover.Code)
	}
	if !bytes.Equal(savedConfig(), afterFull) {
		t.Fatal("detail or cover changed the sync cursor")
	}
	waitAfter(full.Watermark)
	book.Title, book.Rate = "Changed metadata", 4
	if err := st.UpsertBook(t.Context(), book); err != nil {
		t.Fatal(err)
	}
	var incremental store.SyncResponse
	if err := json.Unmarshal(invoke("sync"), &incremental); err != nil {
		t.Fatal(err)
	}
	current, err := st.SyncBook(context.Background(), filename)
	if err != nil {
		t.Fatal(err)
	}
	if len(incremental.Books) != 1 || len(incremental.Annotations) != 0 ||
		incremental.Books[0].AnnotCount != 1 || !reflect.DeepEqual(incremental.Books[0], current.Book) {
		t.Fatalf("metadata incremental sync lost data or replayed unchanged annotations: %+v", incremental)
	}
	before, _ := timestamp(full.Watermark)
	after, _ := timestamp(incremental.Watermark)
	if !after.After(before) {
		t.Fatal("incremental watermark did not advance")
	}
	assertWatermark(incremental.Watermark)
	afterIncremental := savedConfig()
	output := filepath.Join(dir, "error-output.json")
	if err := os.WriteFile(output, []byte("untouched"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"sync", "--since", "9999-01-01T00:00:00Z", "--output", output},
		{"book", "--filename", "missing", "--output", output},
		{"cover", "--filename", "missing", "--output", output},
	} {
		var stdout, stderr bytes.Buffer
		err := run(append([]string{"--config", path}, args...), &stdout, &stderr)
		if err == nil || !strings.Contains(err.Error(), "HTTP ") || stdout.Len() != 0 {
			t.Fatalf("expected actual API error without output, got %v", err)
		}
		data, err := os.ReadFile(output)
		if err != nil || string(data) != "untouched" || !bytes.Equal(savedConfig(), afterIncremental) {
			t.Fatal("real API error overwrote output or advanced watermark")
		}
	}
}

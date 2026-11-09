package webdav

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/corani/lunacy/internal/importer"
	"github.com/corani/lunacy/internal/logger"
	"github.com/corani/lunacy/internal/store"
)

func TestUploadImportUsesServiceContext(t *testing.T) {
	for _, cancelService := range []bool{false, true} {
		name := "disconnected request"
		if cancelService {
			name = "canceled service"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			st, err := store.Open(t.Context(), dir, logger.New("test"))
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close() //nolint:errcheck
			filename := "/books/a.epub"
			if err := st.UpsertBook(t.Context(), store.Book{Filename: filename, Title: "A"}); err != nil {
				t.Fatal(err)
			}
			serviceCtx, cancel := context.WithCancel(t.Context())
			defer cancel()
			requestCtx := t.Context()
			if cancelService {
				cancel()
			} else {
				var disconnect context.CancelFunc
				requestCtx, disconnect = context.WithCancel(requestCtx)
				disconnect()
			}
			davRoot := filepath.Join(dir, "webdav")
			bi := importer.NewBackupImporter(st, davRoot, 0, logger.New("test"))
			h := New(serviceCtx, davRoot, st, bi, logger.New("test"))
			request := httptest.NewRequest(http.MethodPut, "/dav/Cache/a.epub.po",
				strings.NewReader("2@1#100:25%")).WithContext(requestCtx)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, request)
			if rec.Code != http.StatusCreated {
				t.Fatalf("upload status: %d", rec.Code)
			}
			book, err := st.GetBook(t.Context(), filename)
			if err != nil {
				t.Fatal(err)
			}
			expectedPct := 25.0
			if cancelService {
				expectedPct = 0
			}
			if book.Pct != expectedPct {
				t.Fatalf("import used wrong context: progress %v, expected %v", book.Pct, expectedPct)
			}
		})
	}
}

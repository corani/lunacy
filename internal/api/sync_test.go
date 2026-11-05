package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/corani/lunacy/internal/logger"
	"github.com/corani/lunacy/internal/store"
	"github.com/corani/lunacy/static"
)

func testAPI(t *testing.T) (*Handler, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(t.Context(), dir, logger.New("test"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Error(err)
		}
	})
	return New(st, dir, static.FS, logger.New("test"), "test"), st
}

func checkErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func call(h *Handler, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.App().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func TestSyncContractAndLinks(t *testing.T) {
	h, st := testAPI(t)
	filename := "/storage/books/a + \u4e66#?%.epub"
	checkErr(t, st.MutateBook(context.Background(), filename, func(m *store.Mutation) error {
		if err := m.Book(store.Book{Filename: filename, Title: "A", ReadState: 1}); err != nil {
			return err
		}
		if err := m.Annotation(store.Annotation{Filename: filename, AnnID: 1793760000000, CreatedAt: 1793760000000, Type: 1, Text: "quote"}, false); err != nil {
			return err
		}
		return m.ReadingDay(store.ReadingDay{Day: 20761, ReadTimeMs: 3600000, WordsRead: 12000, Pct: 25})
	}))
	checkErr(t, st.UpsertBook(t.Context(), store.Book{Filename: "unread", Title: "Unread"}))
	checkErr(t, st.UpsertBook(t.Context(), store.Book{Filename: "stub", IsStub: true}))
	checkErr(t, st.UpsertAnnotation(t.Context(), store.Annotation{Filename: "stub", AnnID: 1}))
	checkErr(t, st.UpsertBook(t.Context(), store.Book{Filename: "position-only", Title: "Position"}))
	checkErr(t, st.UpsertPosition(t.Context(), store.Position{Filename: "position-only", Pct: 10}))
	rec := call(h, "/api/v1/sync")
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-store" ||
		rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("sync status/headers: %d %v", rec.Code, rec.Header())
	}
	var response store.SyncResponse
	checkErr(t, json.Unmarshal(rec.Body.Bytes(), &response))
	if len(response.Books) != 2 || len(response.Annotations) != 1 ||
		response.Books[0].Filename != filename || response.Books[0].AnnotCount != 1 {
		t.Fatalf("eligibility/order/count: %+v", response)
	}
	b := response.Books[0]
	if b.Authors == nil || b.Position != nil || b.ReadingStats.UsedTimeMs != nil ||
		b.ReadingStats.UpdatedAt != nil || b.ReadingStats.Days[0].ReadTimeMs != 3600000 ||
		b.ReadingStats.Days[0].Pct != 25 || strings.Contains(rec.Body.String(), "cover_file") {
		t.Fatalf("null/units/internal fields: %s", rec.Body.String())
	}
	var raw struct {
		Annotations []map[string]json.RawMessage `json:"annotations"`
	}
	checkErr(t, json.Unmarshal(rec.Body.Bytes(), &raw))
	for _, field := range []string{"chapter", "split_index", "char_offset", "page"} {
		if string(raw.Annotations[0][field]) != "null" {
			t.Errorf("%s must be present and null", field)
		}
	}
	decoded, err := url.PathUnescape(strings.TrimPrefix(b.Links.UI, "#/books/"))
	checkErr(t, err)
	if decoded != filename {
		t.Fatal("UI deeplink did not round-trip")
	}
	base, err := url.Parse("https://example.test/lunacy/")
	checkErr(t, err)
	cover, err := url.Parse(b.Links.Cover)
	checkErr(t, err)
	if base.ResolveReference(cover).Path != "/lunacy/api/v1/book/cover" || cover.Query().Get("filename") != filename {
		t.Fatal("cover link lost prefix or filename")
	}
	if r := call(h, "/"+b.Links.Cover); r.Code != http.StatusOK || r.Header().Get("Content-Type") != "image/svg+xml" {
		t.Fatalf("fallback cover: %d", r.Code)
	}
	detail := call(h, "/api/v1/book?"+url.Values{"filename": {filename}}.Encode())
	var bookResponse store.BookResponse
	checkErr(t, json.Unmarshal(detail.Body.Bytes(), &bookResponse))
	if detail.Code != http.StatusOK || len(bookResponse.Annotations) != 1 ||
		bookResponse.Book.Filename != b.Filename || bookResponse.Book.SyncUpdatedAt != b.SyncUpdatedAt {
		t.Fatalf("detail differs from sync: %s", detail.Body.String())
	}
}

func TestInputErrorsAndLegacyErrors(t *testing.T) {
	h, st := testAPI(t)
	cases := []struct {
		target string
		status int
		code   string
	}{
		{"/api/v1/sync?since=", 400, "invalid_since"},
		{"/api/v1/sync?since=%ZZ", 400, "invalid_query"},
		{"/api/v1/sync?since=garbage", 400, "invalid_since"},
		{"/api/v1/sync?since=2026-11-04T04:00:00.0001Z", 400, "invalid_since"},
		{"/api/v1/sync?since=2026-11-04T04:00:00", 400, "invalid_since"},
		{"/api/v1/sync?since=2026-11-04T04:00:00Z&since=2026-11-04T04:00:00Z", 400, "invalid_since"},
		{"/api/v1/sync?since=2026-02-30T00:00:00Z", 400, "invalid_since"},
		{"/api/v1/sync?since=9999-01-01T00:00:00Z", 400, "invalid_since"},
		{"/api/v1/book", 400, "invalid_filename"},
		{"/api/v1/book?filename=a&filename=b", 400, "invalid_filename"},
		{"/api/v1/book?filename=missing", 404, "not_found"},
		{"/api/v1/book/cover?filename=missing", 404, "not_found"},
		{"/api/book?f=missing", 404, "not_found"},
	}
	for _, tc := range cases {
		t.Run(tc.target, func(t *testing.T) {
			rec := call(h, tc.target)
			var result struct {
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			checkErr(t, json.Unmarshal(rec.Body.Bytes(), &result))
			if rec.Code != tc.status || rec.Header().Get("Content-Type") != "application/json" ||
				result.Error.Code != tc.code || result.Error.Message == "" || strings.Contains(result.Error.Message, "sql:") {
				t.Fatalf("unexpected API error: %d %s", rec.Code, rec.Body)
			}
		})
	}
	for _, target := range []string{"/api/v1/book", "/api/v1/book/cover"} {
		checkErr(t, st.UpsertBook(t.Context(), store.Book{Filename: "unread"}))
		if rec := call(h, target+"?filename=unread"); rec.Code != 404 {
			t.Fatal("unread book available in versioned detail/cover")
		}
	}
	if rec := call(h, "/api/v1/sync?since=2000-01-01T08:00:00%2B08:00"); rec.Code != 200 ||
		!strings.Contains(rec.Body.String(), `"books":[]`) || !strings.Contains(rec.Body.String(), `"annotations":[]`) {
		t.Fatalf("offset timestamp or empty arrays: %s", rec.Body.String())
	}
	for _, target := range []string{"/api/v1/sync", "/api/v1/book", "/api/v1/book/cover"} {
		rec := httptest.NewRecorder()
		h.App().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, target, nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("write route exposed for %s: %d", target, rec.Code)
		}
	}
	// Store failure must not become a v1 404 or a partial success.
	checkErr(t, st.DB().Close())
	for _, target := range []string{"/api/v1/sync", "/api/v1/book?filename=a", "/api/v1/book/cover?filename=a", "/api/stats"} {
		if rec := call(h, target); rec.Code != 500 || !strings.Contains(rec.Body.String(), `"internal_error"`) {
			t.Fatalf("store failure: %d %s", rec.Code, rec.Body.String())
		}
	}
}

func TestLegacyQueriesUseRequestContext(t *testing.T) {
	h, st := testAPI(t)
	checkErr(t, st.UpsertBook(t.Context(), store.Book{Filename: "a"}))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, target := range []string{"/api/stats", "/api/books", "/api/book/annotations?f=a", "/api/authors", "/api/series"} {
		rec := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, target, nil).WithContext(ctx)
		h.App().ServeHTTP(rec, request)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("%s ignored canceled request: %d", target, rec.Code)
		}
	}
}

type blockingResponse struct {
	header  http.Header
	started chan struct{}
	release chan struct{}
}

func (b *blockingResponse) Header() http.Header { return b.header }
func (b *blockingResponse) WriteHeader(int)     {}
func (b *blockingResponse) Write(p []byte) (int, error) {
	close(b.started)
	<-b.release
	return len(p), nil
}

func TestSlowHTTPClientDoesNotBlockImports(t *testing.T) {
	h, st := testAPI(t)
	checkErr(t, st.UpsertBook(t.Context(), store.Book{Filename: "a"}))
	checkErr(t, st.UpsertReadState(t.Context(), "a", 1))
	w := &blockingResponse{header: http.Header{}, started: make(chan struct{}), release: make(chan struct{})}
	sent := make(chan struct{})
	go func() {
		h.App().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/sync", nil))
		close(sent)
	}()
	<-w.started
	defer func() { close(w.release); <-sent }()
	done := make(chan error, 1)
	go func() { done <- st.UpsertReadState(t.Context(), "a", 3) }()
	select {
	case err := <-done:
		checkErr(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("HTTP response delivery held the writer transaction")
	}
}

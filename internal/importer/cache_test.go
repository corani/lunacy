package importer

import (
	"context"
	"errors"
	"testing"

	"github.com/corani/lunacy/internal/logger"
	"github.com/corani/lunacy/internal/store"
)

func TestCanceledCacheImportsDoNotCreateStubs(t *testing.T) {
	st, err := store.Open(t.Context(), t.TempDir(), logger.New("test"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close() //nolint:errcheck
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, err := range []error{
		ImportPosition(ctx, st, "2@1#100:25%", "Cache/new.epub.po", 1234),
		ImportAnnotations(ctx, st, "", "Cache/new.epub.an"),
	} {
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cache import ignored cancellation: %v", err)
		}
	}
	exists, err := st.BookExists(t.Context(), "new.epub")
	if err != nil || exists {
		t.Fatalf("canceled cache import created a stub: %v", err)
	}
}

func TestCachePositionPrecedenceAndReset(t *testing.T) {
	st, err := store.Open(t.Context(), t.TempDir(), logger.New("test"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close() //nolint:errcheck
	filename := "/books/a.epub"
	if err := st.UpsertBook(t.Context(), store.Book{Filename: filename, Title: "A"}); err != nil {
		t.Fatal(err)
	}
	if err := ImportPosition(t.Context(), st, "2@1#100:25%", "Cache/A.epub.po", 1234); err != nil {
		t.Fatal(err)
	}
	first, err := st.SyncBook(context.Background(), filename)
	if err != nil {
		t.Fatal(err)
	}
	if first.Book.Position.Pct != 25 || first.Book.Position.LastRead != 1234 {
		t.Fatal("cache position was not applied")
	}
	if err := st.UpsertPositionIfEmpty(t.Context(), filename, store.Position{Filename: filename, Pct: 10}); err != nil {
		t.Fatal(err)
	}
	if err := ImportPosition(t.Context(), st, "0@0#0:0.0%", "Cache/A.epub.po", 1235); err != nil {
		t.Fatal(err)
	}
	// A source reset clears progress; a timestamp alone is not eligibility.
	response, err := st.Sync(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Books) != 0 {
		t.Fatal("reset progress retained stale reading evidence")
	}
	book, err := st.GetBook(t.Context(), filename)
	if err != nil {
		t.Fatal(err)
	}
	if book.Pct != 0 || book.LastRead != 1235 {
		t.Fatal("zero-position cache source was ignored")
	}
}

func TestCacheAnnotationsBatchAndBackupMerge(t *testing.T) {
	st, err := store.Open(t.Context(), t.TempDir(), logger.New("test"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close() //nolint:errcheck
	filename := "/books/a.pdf"
	if err := st.UpsertBook(t.Context(), store.Book{Filename: filename, Title: "A"}); err != nil {
		t.Fatal(err)
	}
	content := `315#A*#9#A1#1234#A2#9#A3#0#A4#0#A5#0#A6##A7##A8#Contents#A@`
	if err := ImportAnnotations(t.Context(), st, content, "Cache/A.pdf.an"); err != nil {
		t.Fatal(err)
	}
	first, err := st.SyncBook(context.Background(), filename)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Annotations) != 1 || first.Annotations[0].Page == nil || first.Annotations[0].ChapterLabel != "Contents" {
		t.Fatalf("PDF annotation: %+v", first.Annotations)
	}
	if err := st.MutateBook(context.Background(), filename, func(m *store.Mutation) error {
		return m.Annotation(store.Annotation{Filename: filename, AnnID: 1234, CreatedAt: 1234, Type: 1}, true)
	}); err != nil {
		t.Fatal(err)
	}
	after, err := st.SyncBook(context.Background(), filename)
	if err != nil {
		t.Fatal(err)
	}
	if after.Annotations[0].Page == nil || *after.Annotations[0].Page != 9 || after.Annotations[0].ChapterLabel != "Contents" ||
		after.Annotations[0].Type != 0 || after.Annotations[0].ID != first.Annotations[0].ID {
		t.Fatal("backup erased cache-only fields or identity")
	}
	// Every annotation in one cache import shares a rollback boundary.
	if _, err := st.DB().Exec(`CREATE TRIGGER reject_annotations BEFORE INSERT ON annotations
		WHEN NEW.ann_id=5678 BEGIN SELECT RAISE(ABORT,'test failure'); END`); err != nil {
		t.Fatal(err)
	}
	content = `315#A*#9#A1#2345#A2#9#A3#0#A4#0#A5#0#A6##A7##A8#One#A@#A*#10#A1#5678#A2#10#A3#0#A4#0#A5#0#A6##A7##A8#Two#A@`
	if err := ImportAnnotations(t.Context(), st, content, "Cache/A.pdf.an"); err == nil {
		t.Fatal("cache failure reported success")
	}
	after, err = st.SyncBook(context.Background(), filename)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Annotations) != 1 {
		t.Fatal("partial cache batch committed")
	}
}

package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/corani/lunacy/internal/logger"
)

func testStore(t *testing.T) (*Store, *int64) {
	t.Helper()
	st, err := Open(t.Context(), t.TempDir(), logger.New("test"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Error(err)
		}
	})
	at := time.Now().Add(time.Hour).UnixMilli()
	st.now = func() time.Time { return time.UnixMilli(at) }
	return st, &at
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func getSync(t *testing.T, st *Store, since *int64) SyncResponse {
	t.Helper()
	r, err := st.Sync(context.Background(), since)
	must(t, err)
	return r
}

func await[T any](t *testing.T, channel <-chan T) T {
	t.Helper()
	select {
	case value := <-channel:
		return value
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for transaction checkpoint")
		var zero T
		return zero
	}
}

func TestChangeDetection(t *testing.T) {
	st, at := testStore(t)
	ctx := context.Background()
	book := Book{Filename: "/books/a.epub", Title: "A", Author: "Author", AddTime: 123}
	chapter := 2
	ann := Annotation{Filename: book.Filename, AnnID: 1234, CreatedAt: 1234, Text: "quote", Chapter: &chapter, ChapterLabel: "Chapter 2", Type: 1}
	apply := func(m *Mutation) error {
		if err := m.Book(book); err != nil {
			return err
		}
		if err := m.Authors([]string{"Author", "Other", "Author"}); err != nil {
			return err
		}
		if err := m.Annotation(ann, false); err != nil {
			return err
		}
		if err := m.Position(Position{Filename: book.Filename, Chapter: &chapter, Pct: 20, LastRead: 42}, false); err != nil {
			return err
		}
		if err := m.ReadingTotals(5000, 300); err != nil {
			return err
		}
		return m.ReadingDay(ReadingDay{Day: 20000, ReadTimeMs: 3000, WordsRead: 200, Pct: 20})
	}
	must(t, st.MutateBook(ctx, book.Filename, apply))
	initial := getSync(t, st, nil)
	if len(initial.Books) != 1 || len(initial.Annotations) != 1 {
		t.Fatalf("unexpected initial sync: %+v", initial)
	}
	b := initial.Books[0]
	if b.AddTime != 123 || b.Position.LastRead != 42 || b.ReadingStats.Days[0].ReadTimeMs != 3000 ||
		*b.ReadingStats.UsedTimeMs != 5000 || initial.Annotations[0].CreatedAt != 1234 {
		t.Fatal("source values/units were not preserved")
	}
	*at += 1000
	must(t, st.MutateBook(ctx, book.Filename, apply))
	must(t, st.UpsertBookAuthors(t.Context(), book.Filename, []string{"Other", "Author"}))
	repeat := getSync(t, st, nil)
	if !reflect.DeepEqual(initial.Books, repeat.Books) || !reflect.DeepEqual(initial.Annotations, repeat.Annotations) {
		t.Fatal("identical records or normalized author sets changed timestamps")
	}
	since := *at
	*at += 1000
	book.Rate = 4
	must(t, st.UpsertBook(t.Context(), book))
	changed := getSync(t, st, &since)
	if len(changed.Books) != 1 || len(changed.Annotations) != 0 || changed.Books[0].Rate != 4 {
		t.Fatalf("metadata change resent annotations: %+v", changed)
	}
	if changed.Books[0].UpdatedAt == b.UpdatedAt {
		t.Fatal("metadata timestamp did not change")
	}
	since = *at + 1
	*at += 1000
	ann.Note = "edited old note"
	must(t, st.UpsertAnnotation(t.Context(), ann))
	changed = getSync(t, st, &since)
	if len(changed.Annotations) != 1 || changed.Annotations[0].ID != initial.Annotations[0].ID ||
		changed.Annotations[0].CreatedAt != initial.Annotations[0].CreatedAt {
		t.Fatal("annotation update changed identity or was not discoverable")
	}
	since = *at + 1
	*at += 1000
	metadataAt := changed.Books[0].UpdatedAt
	must(t, st.UpsertPosition(t.Context(), Position{Filename: book.Filename, Page: &chapter, Pct: 35, LastRead: 43}))
	changed = getSync(t, st, &since)
	if len(changed.Books) != 1 || len(changed.Annotations) != 0 || changed.Books[0].Position.Chapter != nil ||
		changed.Books[0].UpdatedAt != metadataAt {
		t.Fatal("position update/null comparisons or metadata timestamp incorrect")
	}
	// Inclusive boundary repeats the book observed at exactly this watermark.
	boundary := *at
	if len(getSync(t, st, &boundary).Books) != 1 {
		t.Fatal("inclusive boundary lost")
	}
}

func TestExportEligibility(t *testing.T) {
	for _, evidence := range []string{"annotation", "position", "totals_time", "totals_words", "day_time", "day_words", "day_pct", "reading", "finished", "none"} {
		t.Run(evidence, func(t *testing.T) {
			st, at := testStore(t)
			filename := "/book.epub"
			must(t, st.UpsertBook(t.Context(), Book{Filename: filename, Title: "unread", Rate: 5, AddTime: 100}))
			must(t, st.UpsertPosition(t.Context(), Position{Filename: filename, LastRead: 100}))
			if len(getSync(t, st, nil).Books) != 0 {
				t.Fatal("zero position/rating/added time made book eligible")
			}
			since := *at + 1
			*at += 1000
			must(t, st.MutateBook(context.Background(), filename, func(m *Mutation) error {
				switch evidence {
				case "annotation":
					return m.Annotation(Annotation{Filename: filename, AnnID: 1, Type: 0}, false)
				case "position":
					return m.Position(Position{Filename: filename, Pct: 1}, false)
				case "totals_time":
					return m.ReadingTotals(1, 0)
				case "totals_words":
					return m.ReadingTotals(0, 1)
				case "day_time":
					return m.ReadingDay(ReadingDay{Day: 1, ReadTimeMs: 1})
				case "day_words":
					return m.ReadingDay(ReadingDay{Day: 1, WordsRead: 1})
				case "day_pct":
					return m.ReadingDay(ReadingDay{Day: 1, Pct: 1})
				case "reading":
					return m.ReadState(1)
				case "finished":
					return m.ReadState(3)
				default:
					return m.Book(Book{Filename: filename, Title: "metadata only"})
				}
			}))
			r := getSync(t, st, &since)
			if evidence == "none" {
				if len(r.Books) != 0 {
					t.Fatal("metadata-only update exported")
				}
				_, err := st.SyncBook(context.Background(), filename)
				if !errors.Is(err, sql.ErrNoRows) {
					t.Fatalf("ineligible detail: %v", err)
				}
			} else if len(r.Books) != 1 {
				t.Fatal("reading evidence not discovered")
			}
		})
	}
}

func TestStubTransitionAndSourcePrecedence(t *testing.T) {
	st, at := testStore(t)
	filename := "a.epub"
	must(t, st.UpsertBook(t.Context(), Book{Filename: filename, IsStub: true, Title: "stub"}))
	page := 7
	ann := Annotation{Filename: filename, AnnID: 1234, CreatedAt: 1234, Page: &page, ChapterLabel: "PDF", Type: 0, Text: "bookmark"}
	must(t, st.UpsertAnnotation(t.Context(), ann))
	must(t, st.UpsertPosition(t.Context(), Position{Filename: filename, Page: &page, Pct: 30, LastRead: 55}))
	if r := getSync(t, st, nil); len(r.Books) != 0 || len(r.Annotations) != 0 {
		t.Fatal("stub exported")
	}
	old, err := st.GetAnnotations(t.Context(), filename)
	must(t, err)
	since := *at + 1
	*at += 1000
	apply := func(m *Mutation) error {
		if err := m.Book(Book{Filename: filename, Title: "full metadata"}); err != nil {
			return err
		}
		if err := m.Annotation(Annotation{Filename: filename, AnnID: 1234, CreatedAt: 1234, Type: 1, Text: "bookmark"}, true); err != nil {
			return err
		}
		return m.Position(Position{Filename: filename, Pct: 10}, true)
	}
	must(t, st.MutateBook(context.Background(), filename, apply))
	r := getSync(t, st, &since)
	if len(r.Books) != 1 || len(r.Annotations) != 1 || r.Annotations[0].ID != old[0].ID ||
		r.Annotations[0].Page == nil || r.Annotations[0].Type != 0 || r.Annotations[0].ChapterLabel != "PDF" ||
		r.Books[0].Position.LastRead != 55 {
		t.Fatalf("transition or source precedence failed: %+v", r)
	}
	*at += 1000
	must(t, st.MutateBook(context.Background(), filename, apply))
	must(t, st.UpsertBook(t.Context(), Book{Filename: filename, IsStub: true, Title: "stub"}))
	repeat := getSync(t, st, nil)
	if !reflect.DeepEqual(r.Books, repeat.Books) || !reflect.DeepEqual(r.Annotations, repeat.Annotations) {
		t.Fatal("backup/cache replay manufactured changes or downgraded metadata")
	}
}

func TestRollbackAndCovers(t *testing.T) {
	st, at := testStore(t)
	filename := "a"
	must(t, st.UpsertBook(t.Context(), Book{Filename: filename, Title: "A", CoverFile: "/old-cover"}))
	must(t, st.UpsertReadState(t.Context(), filename, 1))
	must(t, st.UpsertBookAuthors(t.Context(), filename, []string{"A"}))
	old := getSync(t, st, nil)
	*at += 1000
	wantErr := errors.New("abort batch")
	err := st.MutateBook(context.Background(), filename, func(m *Mutation) error {
		if err := m.Authors([]string{"new author"}); err != nil {
			return err
		}
		if err := m.ReadState(3); err != nil {
			return err
		}
		if err := m.Annotation(Annotation{Filename: filename, AnnID: 10}, false); err != nil {
			return err
		}
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatal(err)
	}
	repeat := getSync(t, st, nil)
	if !reflect.DeepEqual(old.Books, repeat.Books) || len(repeat.Annotations) != 0 {
		t.Fatal("failed batch not rolled back")
	}
	must(t, st.UpsertBook(t.Context(), Book{Filename: filename, Title: "A"}))
	b, err := st.GetBook(t.Context(), filename)
	must(t, err)
	if b.CoverFile != "/old-cover" {
		t.Fatal("unavailable cover input cleared existing cover")
	}
	*at += 1000
	coverPath := filepath.Join(t.TempDir(), "cover.png")
	must(t, os.WriteFile(coverPath, []byte("first image bytes"), 0o600))
	cover, err := PrepareCover(coverPath)
	must(t, err)
	must(t, st.MutateBook(context.Background(), filename, func(m *Mutation) error { return m.Cover(cover) }))
	beforeCoverChange := getSync(t, st, nil)
	*at += 1000
	must(t, os.WriteFile(coverPath, []byte("different image bytes"), 0o600))
	cover, err = PrepareCover(coverPath)
	must(t, err)
	must(t, st.MutateBook(context.Background(), filename, func(m *Mutation) error { return m.Cover(cover) }))
	r := getSync(t, st, nil)
	if r.Books[0].UpdatedAt == beforeCoverChange.Books[0].UpdatedAt || r.Books[0].SyncUpdatedAt == beforeCoverChange.Books[0].SyncUpdatedAt {
		t.Fatal("same-path cover change not discovered")
	}
	*at += 1000
	must(t, st.MutateBook(context.Background(), filename, func(m *Mutation) error { return m.Cover(cover) }))
	if !reflect.DeepEqual(r.Books, getSync(t, st, nil).Books) {
		t.Fatal("unchanged cover content advanced timestamps")
	}
	must(t, st.MutateBook(context.Background(), filename, func(m *Mutation) error { return m.Cover(Cover{}) }))
	b, err = st.GetBook(t.Context(), filename)
	must(t, err)
	if b.CoverFile != "" {
		t.Fatal("explicit cover removal did not apply")
	}
}

func TestChildAndAuthorChanges(t *testing.T) {
	st, at := testStore(t)
	filename := "a"
	must(t, st.UpsertBook(t.Context(), Book{Filename: filename, Title: "A"}))
	must(t, st.UpsertAnnotation(t.Context(), Annotation{Filename: filename, AnnID: 1, Text: "same quote"}))
	must(t, st.UpsertAnnotation(t.Context(), Annotation{Filename: filename, AnnID: 2, Text: "same quote"}))
	old := getSync(t, st, nil)
	if len(old.Annotations) != 2 || old.Annotations[0].ID == old.Annotations[1].ID {
		t.Fatal("quote text was incorrectly used as identity")
	}
	for _, kind := range []string{"totals", "day", "authors"} {
		since := *at + 1
		*at += 1000
		must(t, st.MutateBook(context.Background(), filename, func(m *Mutation) error {
			switch kind {
			case "totals":
				return m.ReadingTotals(1000, 10)
			case "day":
				return m.ReadingDay(ReadingDay{Day: 1, ReadTimeMs: 100, Pct: 2})
			default:
				return m.Authors([]string{"New Author"})
			}
		}))
		r := getSync(t, st, &since)
		if len(r.Books) != 1 || len(r.Annotations) != 0 || r.Books[0].AnnotCount != 2 {
			t.Fatalf("%s change not independently discoverable: %+v", kind, r)
		}
		if kind != "authors" && r.Books[0].UpdatedAt != old.Books[0].UpdatedAt {
			t.Fatal("child change advanced book metadata timestamp")
		}
		if kind == "authors" && r.Books[0].UpdatedAt == old.Books[0].UpdatedAt {
			t.Fatal("author change did not advance metadata timestamp")
		}
	}
}

func TestMigrationBaselineAndRestart(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "lunacy.db"))
	must(t, err)
	for i, migration := range migrations[:len(migrations)-1] {
		_, err := db.Exec(migration.sql)
		must(t, err)
		_, err = db.Exec(fmt.Sprintf("PRAGMA user_version=%d", i+1))
		must(t, err)
	}
	_, err = db.Exec(`INSERT INTO books(filename,title) VALUES ('a','A');
		INSERT INTO annotations(filename,ann_id,created_at) VALUES ('a',123,123);
		INSERT INTO positions(filename,pct) VALUES ('a',10);
		INSERT INTO reading_days(filename,day,read_time_s,pct) VALUES ('a',20000,1234,10);`)
	must(t, err)
	must(t, db.Close())
	st, err := Open(t.Context(), dir, logger.New("test"))
	must(t, err)
	r := getSync(t, st, nil)
	if r.Books[0].UpdatedAt != r.BaselineAt || r.Annotations[0].UpdatedAt != r.BaselineAt ||
		r.Books[0].Position.UpdatedAt != r.BaselineAt || r.Books[0].ReadingStats.Days[0].UpdatedAt != r.BaselineAt ||
		r.Books[0].ReadingStats.UsedTimeMs != nil {
		t.Fatal("baseline backfill or unknown totals incorrect")
	}
	future := time.Now().Add(time.Hour).UnixMilli()
	st.now = func() time.Time { return time.UnixMilli(future) }
	r = getSync(t, st, nil)
	must(t, st.Close())
	st, err = Open(t.Context(), dir, logger.New("test"))
	must(t, err)
	defer st.Close() //nolint:errcheck
	st.now = func() time.Time { return time.UnixMilli(100) }
	must(t, st.UpsertReadState(t.Context(), "a", 3))
	restarted := getSync(t, st, &future)
	if restarted.Watermark != r.Watermark || restarted.BaselineAt != r.BaselineAt ||
		len(restarted.Books) != 1 || restarted.Books[0].UpdatedAt != r.Watermark {
		t.Fatal("restart/backward clock lost published floor")
	}
}

func TestWriterAndSyncInterleavings(t *testing.T) {
	for _, writerFirst := range []bool{true, false} {
		t.Run(fmt.Sprint(writerFirst), func(t *testing.T) {
			dir := t.TempDir()
			writer, err := Open(t.Context(), dir, logger.New("writer"))
			must(t, err)
			defer writer.Close() //nolint:errcheck
			reader, err := Open(t.Context(), dir, logger.New("reader"))
			must(t, err)
			defer reader.Close() //nolint:errcheck
			base := time.Now().Add(time.Hour).UnixMilli()
			writer.now = func() time.Time { return time.UnixMilli(base) }
			must(t, writer.UpsertBook(t.Context(), Book{Filename: "a", Title: "A"}))
			must(t, writer.UpsertReadState(t.Context(), "a", 1))
			getSync(t, reader, nil)
			entered, release := make(chan struct{}), make(chan struct{})
			done := make(chan error, 1)
			type syncResult struct {
				response SyncResponse
				err      error
			}
			synced := make(chan syncResult, 1)
			if writerFirst {
				writer.now = func() time.Time { return time.UnixMilli(base + 1000) }
				go func() {
					done <- writer.MutateBook(context.Background(), "a", func(m *Mutation) error {
						close(entered)
						<-release
						return m.ReadState(3)
					})
				}()
				await(t, entered)
				reader.now = func() time.Time { return time.UnixMilli(base + 2000) }
				go func() {
					r, err := reader.Sync(context.Background(), nil)
					synced <- syncResult{response: r, err: err}
				}()
				close(release)
				must(t, await(t, done))
				result := await(t, synced)
				must(t, result.err)
				r := result.response
				if r.Books[0].ReadState != 3 {
					t.Fatal("snapshot failed to include committed writer")
				}
			} else {
				reader.now = func() time.Time {
					close(entered)
					<-release
					return time.UnixMilli(base + 2000)
				}
				go func() {
					r, err := reader.Sync(context.Background(), nil)
					synced <- syncResult{response: r, err: err}
				}()
				await(t, entered)
				// Writer clock is behind the reader; its mutation must acquire
				// SQLite's lock before reading the newly published floor.
				writer.now = func() time.Time { return time.UnixMilli(base + 1000) }
				go func() { done <- writer.UpsertReadState(t.Context(), "a", 3) }()
				close(release)
				result := await(t, synced)
				must(t, result.err)
				r := result.response
				must(t, await(t, done))
				reader.now = func() time.Time { return time.UnixMilli(base) }
				since := base + 2000
				next := getSync(t, reader, &since)
				if r.Books[0].ReadState != 1 || len(next.Books) != 1 ||
					next.Books[0].ReadState != 3 || next.Books[0].UpdatedAt != r.Watermark {
					t.Fatal("writer committed behind published watermark")
				}
			}
		})
	}
}

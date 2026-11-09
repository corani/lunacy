package importer

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/corani/lunacy/internal/logger"
	"github.com/corani/lunacy/internal/store"
)

func TestCanceledBackupImport(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(t.Context(), dir, logger.New("test"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close() //nolint:errcheck
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	bi := NewBackupImporter(st, dir, 0, logger.New("test"))
	if err := bi.Import(ctx, syntheticBackup(t, "note", 5000, "2@1#100:25%")); !errors.Is(err, context.Canceled) {
		t.Fatalf("backup ignored cancellation: %v", err)
	}
	exists, err := st.BookExists(t.Context(), "/a.epub")
	if err != nil || exists {
		t.Fatalf("canceled backup imported metadata: %v", err)
	}
}

func syntheticBackup(t *testing.T, note string, usedTime int64, position string) string {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "mrbooks.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		CREATE TABLE books(book TEXT,filename TEXT,lowerFilename TEXT,author TEXT,description TEXT,
			category TEXT,favorite TEXT,rate TEXT,addTime INTEGER);
		INSERT INTO books VALUES ('A','/a.epub','/a.epub','Author; Other','description','','shelf','4',123);
		INSERT INTO books VALUES ('B','/b.epub','/b.epub','Author','','','','0',124);
		CREATE TABLE notes(lowerFilename TEXT,lastChapter INTEGER,lastSplitIndex INTEGER,lastPosition INTEGER,
			highlightLength INTEGER,highlightColor INTEGER,time INTEGER,note TEXT,original TEXT,underline INTEGER,strikethrough INTEGER);
		CREATE TABLE statistics(filename TEXT,usedTime INTEGER,readWords INTEGER,dates TEXT);
	`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO notes VALUES ('/a.epub',2,1,100,5,-256,1234,?,'quote',0,0);`, note)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO statistics VALUES ('/a.epub',?,100,'20000|3000@100 #25%');`, usedTime)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	z := zip.NewWriter(&archive)
	files := []struct {
		name string
		data []byte
	}{
		{"_names.list", []byte("/moon/databases/mrbooks.db\n/moon/shared_prefs/positions10.xml\n/moon/shared_prefs/state_filter.xml\n")},
		{"1.tag", data},
		{"2.tag", []byte(`<map><string name="/b.epub">` + position + `</string></map>`)},
		{"3.tag", []byte(`<map><int name="/a.epub" value="1" /></map>`)},
	}
	for _, f := range files {
		w, err := z.Create(f.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(f.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "test.mrpro")
	if err := os.WriteFile(path, archive.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestBackupIncrementalAndNoop(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(t.Context(), dir, logger.New("test"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close() //nolint:errcheck
	bi := NewBackupImporter(st, dir, 0, logger.New("test"))
	backup := syntheticBackup(t, "old note", 5000, "2@1#100:25%")
	if err := bi.Import(t.Context(), backup); err != nil {
		t.Fatal(err)
	}
	first, err := st.Sync(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Books) != 2 || len(first.Annotations) != 1 || first.Books[0].ReadState != 1 ||
		*first.Books[0].ReadingStats.UsedTimeMs != 5000 || first.Books[1].Position.Pct != 25 {
		t.Fatalf("backup response: %+v", first)
	}
	// Advance the persisted clock without arbitrary sleeps so a spurious write
	// on replay cannot hide in the original millisecond.
	if _, err := st.DB().Exec(`UPDATE sync_state SET timestamp_floor=timestamp_floor+1000`); err != nil {
		t.Fatal(err)
	}
	if err := bi.Import(t.Context(), backup); err != nil {
		t.Fatal(err)
	}
	repeat, err := st.Sync(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first.Books, repeat.Books) || !reflect.DeepEqual(first.Annotations, repeat.Annotations) {
		t.Fatal("identical backup manufactured changes")
	}
	at, err := time.Parse(time.RFC3339Nano, repeat.Watermark)
	if err != nil {
		t.Fatal(err)
	}
	// Move past the inclusive boundary before applying the next backup.
	since := at.UnixMilli() + 1
	if _, err := st.DB().Exec(`UPDATE sync_state SET timestamp_floor=?`, since+1000); err != nil {
		t.Fatal(err)
	}
	if err := bi.Import(t.Context(), syntheticBackup(t, "edited old note", 7000, "2@1#100:25%")); err != nil {
		t.Fatal(err)
	}
	changed, err := st.Sync(context.Background(), &since)
	if err != nil {
		t.Fatal(err)
	}
	if len(changed.Books) != 1 || len(changed.Annotations) != 1 ||
		changed.Annotations[0].ID != first.Annotations[0].ID || changed.Annotations[0].CreatedAt != 1234 ||
		changed.Annotations[0].Note != "edited old note" || *changed.Books[0].ReadingStats.UsedTimeMs != 7000 {
		t.Fatalf("changed backup sync: %+v", changed)
	}
}

func TestBackupFailedBookBatchRollsBack(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(t.Context(), dir, logger.New("test"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close() //nolint:errcheck
	_, err = st.DB().Exec(`CREATE TRIGGER reject_totals BEFORE INSERT ON reading_totals BEGIN SELECT RAISE(ABORT,'test failure'); END`)
	if err != nil {
		t.Fatal(err)
	}
	bi := NewBackupImporter(st, dir, 0, logger.New("test"))
	if err := bi.Import(t.Context(), syntheticBackup(t, "note", 5000, "2@1#100:25%")); err == nil {
		t.Fatal("failed totals write reported success")
	}
	exists, err := st.BookExists(t.Context(), "/a.epub")
	if err != nil || exists {
		t.Fatalf("book metadata/children leaked from failed batch: %v", err)
	}
}

// Personal backups are opt-in, never automated test fixtures.
func TestBackupPerformance(t *testing.T) {
	path := os.Getenv("LUNACY_TEST_BACKUP")
	if path == "" {
		t.Skip("set LUNACY_TEST_BACKUP to benchmark an isolated import")
	}
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
	bi := NewBackupImporter(st, dir, 0, logger.New("test"))
	// Keep total_changes on the same SQLite connection for write measurements.
	st.DB().SetMaxOpenConns(1)
	var previous *store.SyncResponse
	for _, name := range []string{"initial", "identical"} {
		var before, after int64
		if err := st.DB().QueryRow(`SELECT total_changes()`).Scan(&before); err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		if err := bi.Import(t.Context(), path); err != nil {
			t.Fatal(err)
		}
		t.Logf("%s import: %s", name, time.Since(start))
		if err := st.DB().QueryRow(`SELECT total_changes()`).Scan(&after); err != nil {
			t.Fatal(err)
		}
		start = time.Now()
		response, err := st.Sync(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		body, err := json.Marshal(response)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("%s: %d changed database rows; sync: %d books, %d annotations, %d bytes, %s",
			name, after-before, len(response.Books), len(response.Annotations), len(body), time.Since(start))
		if previous != nil && (!reflect.DeepEqual(previous.Books, response.Books) ||
			!reflect.DeepEqual(previous.Annotations, response.Annotations)) {
			t.Fatal("identical real backup changed observations")
		}
		previous = &response
	}
	if next := os.Getenv("LUNACY_TEST_NEXT_BACKUP"); next != "" {
		start := time.Now()
		if err := bi.Import(t.Context(), next); err != nil {
			t.Fatal(err)
		}
		t.Logf("next import: %s", time.Since(start))
		at, err := time.Parse(time.RFC3339Nano, previous.Watermark)
		if err != nil {
			t.Fatal(err)
		}
		since := at.UnixMilli()
		response, err := st.Sync(context.Background(), &since)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("next incremental: %d books, %d annotations", len(response.Books), len(response.Annotations))
	}
}

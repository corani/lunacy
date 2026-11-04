package main

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/corani/lunacy/internal/config"
)

const validBook = `{"filename":"/books/a #?.epub","title":"A","authors":[],"annot_count":0,"updated_at":"2026-11-04T04:00:00.000Z","sync_updated_at":"2026-11-04T04:30:00.000Z","position":null,"reading_stats":null,"links":{"ui":"#/books/a","cover":"api/v1/book/cover?filename=a"}}`

const validSync = `{"watermark":"2026-11-04T04:30:00.000Z","baseline_at":"2026-11-04T03:00:00.000Z","books":[` + validBook + `],"annotations":[]}`

const validPosition = `{"chapter":3,"split_index":1,"char_offset":420,"page":null,"pct":27.5,"last_read":1793765400000,"updated_at":"2026-11-04T04:20:00Z"}`

const validAnnotation = `{"id":1,"ann_id":123,"created_at":123,"filename":"/books/a #?.epub","text":"","note":"","updated_at":"2026-11-04T04:20:00Z"}`

func testConfig(t *testing.T, base string) (string, string) {
	t.Helper()
	dir, err := os.MkdirTemp(".", ".client-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	path := filepath.Join(dir, "config.yaml")
	data := "# retain credentials\nclient:\n  base_url: " + strconv.Quote(base) + "\n  username: moon\n  password: private-secret\n  timeout: 1s\n  watermark: '2026-11-04T04:00:00.000Z' # cursor\nserver:\n  password: 'unrelated'\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	return path, data
}

func TestSyncAuthPrefixOutputAndSave(t *testing.T) {
	var gotSince string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if !ok || user != "moon" || password != "private-secret" {
			t.Error("missing Basic Auth")
		}
		if r.URL.Path != "/deployment/api/v1/sync" || r.Header.Get("Accept") != "application/json" {
			t.Errorf("wrong request: %s", r.URL)
		}
		gotSince = r.URL.Query().Get("since")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(validSync))
	}))
	defer server.Close()
	path, _ := testConfig(t, server.URL+"/deployment")
	for _, replay := range [][]string{nil, {"--full"}, {"--since", "2026-11-04T04:10:00Z"}} {
		var stdout, stderr bytes.Buffer
		args := append([]string{"--config", path, "sync"}, replay...)
		if err := run(args, &stdout, &stderr); err != nil {
			t.Fatal(err)
		}
		wantSince := "2026-11-04T04:00:00.000Z"
		if len(replay) == 1 {
			wantSince = ""
		} else if len(replay) == 2 {
			wantSince = replay[1]
		}
		if gotSince != wantSince {
			t.Errorf("since=%q want=%q", gotSince, wantSince)
		}
		if !strings.Contains(stdout.String(), "\n  \"watermark\":") || stderr.Len() != 0 {
			t.Fatal("expected pretty stdout without diagnostics")
		}
		cfg, err := config.Load(path)
		if err != nil || cfg.Client.Watermark != "2026-11-04T04:30:00.000Z" {
			t.Fatal("watermark not saved")
		}
	}
	output := filepath.Join(filepath.Dir(path), "sync.json")
	if err := run([]string{"sync", "--config", path, "--output", output}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(output)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("output missing or not private")
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("failed") }

func TestSyncFailuresDoNotAdvanceOrWrite(t *testing.T) {
	cases := []struct {
		name   string
		body   string
		status int
		output bool
	}{
		{"malformed JSON", "{", 200, false},
		{"missing shape", `{"watermark":"2026-11-04T04:30:00.000Z"}`, 200, false},
		{"null books", strings.Replace(validSync, `"books":[`+validBook+`]`, `"books":null`, 1), 200, false},
		{"bad timestamp", strings.Replace(validSync, "04:30:00.000Z", "04:30:00.0001Z", 1), 200, false},
		{"orphan", strings.Replace(validSync, `"annotations":[]`, `"annotations":[{"filename":"orphan"}]`, 1), 200, false},
		{"invalid metric", strings.Replace(validSync, `"position":null`, `"position":`+strings.Replace(validPosition, `"pct":27.5`, `"pct":"wrong"`, 1), 1), 200, false},
		{"unauthorized", `{"error":{"code":"unauthorized","message":"private-secret"}}`, 401, false},
		{"malformed error", "private-secret " + strings.Repeat("x", 5000), 500, false},
		{"output failure", validSync, 200, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			path, original := testConfig(t, server.URL)
			var stdout, stderr bytes.Buffer
			var err error
			if tc.output {
				err = run([]string{"sync", "--config", path}, failingWriter{}, &stderr)
			} else {
				err = run([]string{"sync", "--config", path}, &stdout, &stderr)
			}
			if err == nil {
				t.Fatal("expected failure")
			}
			if stdout.Len() != 0 || strings.Contains(err.Error(), "private-secret") {
				t.Fatal("invalid output or leaked credentials")
			}
			if tc.name == "malformed error" && !strings.Contains(err.Error(), "truncated") {
				t.Fatal("missing truncation diagnostic")
			}
			data, _ := os.ReadFile(path)
			if string(data) != original {
				t.Fatal("config changed on failure")
			}
		})
	}
}

func TestBookAndCoverPreserveConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("filename") != "/books/a #?.epub" {
			t.Fatal("filename not encoded exactly")
		}
		switch r.URL.Path {
		case "/prefix/api/v1/book":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_, _ = w.Write([]byte(`{"book":` + validBook + `,"annotations":[]}`))
		case "/prefix/api/v1/book/cover":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("image bytes"))
		default:
			t.Error("wrong endpoint")
		}
	}))
	defer server.Close()
	path, original := testConfig(t, server.URL+"/prefix/")
	for _, command := range []string{"book", "cover"} {
		output := filepath.Join(filepath.Dir(path), command+".out")
		var stdout bytes.Buffer
		if err := run([]string{command, "--config", path, "--filename", "/books/a #?.epub", "--output", output}, &stdout, &bytes.Buffer{}); err != nil {
			t.Fatal(err)
		}
		data, _ := os.ReadFile(path)
		if string(data) != original || stdout.Len() != 0 {
			t.Fatal("book/cover mutated config or wrote stdout")
		}
	}
}

func TestRedirectTimeoutAndConfigOutputRefused(t *testing.T) {
	hit := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hit = true }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer redirect.Close()
	path, original := testConfig(t, redirect.URL)
	if err := run([]string{"sync", "--config", path}, &bytes.Buffer{}, &bytes.Buffer{}); err == nil || hit {
		t.Fatal("redirect was followed")
	}
	if err := run([]string{"sync", "--config", path, "--output", path}, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Fatal("allowed config as output")
	}
	data, _ := os.ReadFile(path)
	if string(data) != original {
		t.Fatal("config changed")
	}
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		_, _ = w.Write([]byte(validSync))
	}))
	defer slow.Close()
	if _, err := request(config.ClientConfig{BaseURL: slow.URL, Timeout: time.Millisecond}, "api/v1/sync", nil, false); err == nil {
		t.Fatal("timeout not honored")
	}
}

func TestEmptySyncAndOutputFileFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"watermark":"2026-11-04T04:30:00.000Z","baseline_at":"2026-11-04T03:00:00.000Z","books":[],"annotations":[]}`))
	}))
	defer server.Close()
	path, original := testConfig(t, server.URL)
	if err := run([]string{"sync", "--config", path, "--output", filepath.Dir(path)}, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Fatal("expected output file failure")
	}
	data, _ := os.ReadFile(path)
	if string(data) != original {
		t.Fatal("config changed on output file failure")
	}
	if err := run([]string{"sync", "--config", path}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil || cfg.Client.Watermark != "2026-11-04T04:30:00.000Z" {
		t.Fatal("empty sync did not save watermark")
	}
}

func TestSaveFailureAfterSuccessfulOutput(t *testing.T) {
	var path string
	changed := "# concurrently changed config\nclient: null\npassword: 'untouched'\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := os.WriteFile(path, []byte(changed), 0o600); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(validSync))
	}))
	defer server.Close()
	path, _ = testConfig(t, server.URL)
	var stdout bytes.Buffer
	err := run([]string{"sync", "--config", path}, &stdout, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "watermark save failed") || stdout.Len() == 0 {
		t.Fatal("expected output success followed by reported save failure")
	}
	data, _ := os.ReadFile(path)
	if string(data) != changed {
		t.Fatal("failed save overwrote current config")
	}
}

func TestValidationRejectsInvalidChildren(t *testing.T) {
	stats := `{"used_time_ms":null,"read_words":null,"updated_at":null,"days":[{"day":1,"read_time_ms":3600000,"words_read":12000,"pct":27.5,"updated_at":"2026-11-04T04:20:00Z"}]}`
	valid := strings.Replace(validSync, `"reading_stats":null`, `"reading_stats":`+stats, 1)
	if _, err := validateResponse([]byte(valid), "sync", "", ""); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{
		strings.Replace(validSync, `"authors":[]`, `"authors":null`, 1),
		strings.Replace(validSync, `"position":null`, `"position":{"updated_at":"invalid"}`, 1),
		strings.Replace(valid, `"days":[{"day":1,"read_time_ms":3600000,"words_read":12000,"pct":27.5,"updated_at":"2026-11-04T04:20:00Z"}]`, `"days":null`, 1),
		strings.Replace(valid, `"used_time_ms":null`, `"used_time_ms":10`, 1),
		strings.Replace(valid, "04:20:00Z", "04:40:00Z", 1),
		strings.Replace(validSync, `"annotations":[]`, `"annotations":[{"id":1,"ann_id":123,"created_at":123,"filename":"/books/a #?.epub","text":"","note":"","updated_at":"2026-11-04T04:40:00Z"}]`, 1),
	} {
		if _, err := validateResponse([]byte(invalid), "sync", "", ""); err == nil {
			t.Fatalf("accepted invalid response: %s", invalid)
		}
	}
}

func TestPositionAndReadingDayMetrics(t *testing.T) {
	positionResponse := strings.Replace(validSync, `"position":null`, `"position":`+validPosition, 1)
	if _, err := validateResponse([]byte(positionResponse), "sync", "", ""); err != nil {
		t.Fatal(err)
	}
	nullableResponse := strings.Replace(positionResponse, `"chapter":3,"split_index":1,"char_offset":420`, `"chapter":null,"split_index":null,"char_offset":null`, 1)
	if _, err := validateResponse([]byte(nullableResponse), "sync", "", ""); err != nil {
		t.Fatal(err)
	}
	day := `{"day":1,"read_time_ms":100,"words_read":200,"pct":27.5,"updated_at":"2026-11-04T04:20:00Z"}`
	dayResponse := strings.Replace(validSync, `"reading_stats":null`, `"reading_stats":{"used_time_ms":null,"read_words":null,"updated_at":null,"days":[`+day+`]}`, 1)
	if _, err := validateResponse([]byte(dayResponse), "sync", "", ""); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, source, from, to string
	}{
		{"missing chapter", positionResponse, `"chapter":3,`, ""},
		{"string location", positionResponse, `"split_index":1`, `"split_index":"1"`},
		{"fractional location", positionResponse, `"char_offset":420`, `"char_offset":420.5`},
		{"object location", positionResponse, `"page":null`, `"page":{}`},
		{"missing position percentage", positionResponse, `"pct":27.5,`, ""},
		{"null position percentage", positionResponse, `"pct":27.5`, `"pct":null`},
		{"null last read", positionResponse, `"last_read":1793765400000`, `"last_read":null`},
		{"missing last read", positionResponse, `"last_read":1793765400000,`, ""},
		{"missing reading time", dayResponse, `"read_time_ms":100,`, ""},
		{"null reading time", dayResponse, `"read_time_ms":100`, `"read_time_ms":null`},
		{"string words read", dayResponse, `"words_read":200`, `"words_read":"200"`},
		{"missing words read", dayResponse, `"words_read":200,`, ""},
		{"missing day percentage", dayResponse, `"pct":27.5,`, ""},
		{"string day percentage", dayResponse, `"pct":27.5`, `"pct":"27.5"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := strings.Replace(tc.source, tc.from, tc.to, 1)
			if _, err := validateResponse([]byte(body), "sync", "", ""); err == nil {
				t.Fatal("accepted invalid reading data")
			}
		})
	}
}

func TestCurrentAnnotationCounts(t *testing.T) {
	sync := strings.Replace(validSync, `"annotations":[]`, `"annotations":[`+validAnnotation+`]`, 1)
	detail := `{"book":` + validBook + `,"annotations":[` + validAnnotation + `]}`
	for _, tc := range []struct {
		name, command, source, count string
		wantError                    bool
	}{
		{"sync exact", "sync", sync, "1", false},
		{"sync incremental subset", "sync", sync, "2", false},
		{"sync exceeds current count", "sync", sync, "0", true},
		{"detail exact", "book", detail, "1", false},
		{"detail incomplete", "book", detail, "2", true},
		{"detail exceeds current count", "book", detail, "0", true},
		{"negative count", "sync", sync, "-1", true},
		{"null count", "sync", sync, "null", true},
		{"string count", "sync", sync, `"1"`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := strings.Replace(tc.source, `"annot_count":0`, `"annot_count":`+tc.count, 1)
			_, err := validateResponse([]byte(body), tc.command, "/books/a #?.epub", "")
			if (err != nil) != tc.wantError {
				t.Fatalf("validation err=%v want error=%v", err, tc.wantError)
			}
		})
	}
	body := strings.Replace(validSync, `"annot_count":0,`, "", 1)
	if _, err := validateResponse([]byte(body), "sync", "", ""); err == nil {
		t.Fatal("accepted missing current annotation count")
	}
}

func TestJSONSuccessContentType(t *testing.T) {
	for _, contentType := range []string{"application/json", "application/json; charset=utf-8", "text/plain", "image/png", "", "application/json;broken"} {
		t.Run(contentType, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header()["Content-Type"] = []string{contentType}
				_, _ = w.Write([]byte(validSync))
			}))
			defer server.Close()
			path, original := testConfig(t, server.URL)
			var stdout bytes.Buffer
			err := run([]string{"sync", "--config", path}, &stdout, &bytes.Buffer{})
			valid := contentType == "application/json" || contentType == "application/json; charset=utf-8"
			if (err == nil) != valid {
				t.Fatalf("err=%v valid=%v", err, valid)
			}
			if !valid {
				data, _ := os.ReadFile(path)
				if stdout.Len() != 0 || string(data) != original {
					t.Fatal("invalid content type produced output or changed cursor")
				}
			}
		})
	}
}

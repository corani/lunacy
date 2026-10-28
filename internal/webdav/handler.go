package webdav

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	gwebdav "golang.org/x/net/webdav"

	"github.com/corani/lunacy/internal/importer"
	"github.com/corani/lunacy/internal/logger"
	"github.com/corani/lunacy/internal/store"
)

// Handler wraps golang.org/x/net/webdav.Handler and hooks into PUT events
// to trigger data import.
type Handler struct {
	webdavHandler *gwebdav.Handler
	st            *store.Store
	bi            *importer.BackupImporter
	davRoot       string
	log           *logger.Logger
}

// New creates a WebDAV handler serving files from davRoot.
func New(davRoot string, st *store.Store, bi *importer.BackupImporter, log *logger.Logger) *Handler {
	h := &Handler{st: st, bi: bi, davRoot: davRoot, log: log}
	h.webdavHandler = &gwebdav.Handler{
		Prefix:     "/dav",
		FileSystem: gwebdav.Dir(davRoot),
		LockSystem: gwebdav.NewMemLS(),
		Logger:     h.davLog,
	}
	return h
}

// ServeHTTP implements http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.webdavHandler.ServeHTTP(w, r)
}

func (h *Handler) davLog(r *http.Request, err error) {
	if err != nil {
		h.log.Error("webdav", "method", r.Method, "path", r.URL.Path, "err", err)
		return
	}
	h.log.Info("webdav", "method", r.Method, "path", r.URL.Path)

	if r.Method != "PUT" {
		return
	}
	rel := strings.TrimPrefix(r.URL.Path, "/dav/")
	diskPath := filepath.Join(h.davRoot, filepath.FromSlash(rel))
	h.handleUpload(rel, diskPath)
}

func (h *Handler) handleUpload(webdavPath, diskPath string) {
	lower := strings.ToLower(webdavPath)
	start := time.Now()

	switch {
	case strings.HasSuffix(lower, ".mrpro"):
		if err := h.bi.Import(diskPath); err != nil {
			h.log.Error("backup import", "file", webdavPath, "err", err)
		} else {
			h.log.Info("backup import", "file", webdavPath, "elapsed", time.Since(start).Round(time.Millisecond))
		}

	case strings.HasSuffix(lower, ".po"):
		data, err := os.ReadFile(diskPath)
		if err != nil {
			h.log.Error("position import", "file", webdavPath, "err", err)
			return
		}
		info, err := os.Stat(diskPath)
		if err != nil {
			h.log.Error("position import", "file", webdavPath, "err", err)
			return
		}
		if err := importer.ImportPosition(h.st, string(data), webdavPath, info.ModTime().UnixMilli()); err != nil {
			h.log.Error("position import", "file", webdavPath, "err", err)
		} else {
			h.log.Info("position import", "file", webdavPath, "elapsed", time.Since(start).Round(time.Millisecond))
		}

	case strings.HasSuffix(lower, ".an"):
		data, err := os.ReadFile(diskPath)
		if err != nil {
			h.log.Error("annotation import", "file", webdavPath, "err", err)
			return
		}
		content, err := decompressZlib(data)
		if err != nil {
			h.log.Error("annotation import", "file", webdavPath, "err", err)
			return
		}
		if err := importer.ImportAnnotations(h.st, content, webdavPath); err != nil {
			h.log.Error("annotation import", "file", webdavPath, "err", err)
		} else {
			h.log.Info("annotation import", "file", webdavPath, "elapsed", time.Since(start).Round(time.Millisecond))
		}
	}
}

// LoggingMiddleware logs every HTTP request method, path, status, and duration.
func LoggingMiddleware(log *logger.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		lw := &statusWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(lw, r)
		log.Info("http", "method", r.Method, "path", r.URL.Path, "status", lw.status, "elapsed", time.Since(start).Round(time.Millisecond))
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (sw *statusWriter) WriteHeader(code int) {
	sw.status = code
	sw.ResponseWriter.WriteHeader(code)
}

// BasicAuth wraps an http.Handler with HTTP Basic Authentication.
func BasicAuth(username, password string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != username || p != password {
			w.Header().Set("WWW-Authenticate", `Basic realm="lunacy"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// MkdirAll ensures the WebDAV root directory exists on disk.
func MkdirAll(davRoot string) error {
	return os.MkdirAll(davRoot, 0o755)
}

func decompressZlib(data []byte) (string, error) {
	r, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("zlib: %w", err)
	}
	defer r.Close() //nolint:errcheck
	out, err := io.ReadAll(r)
	if err != nil {
		return "", fmt.Errorf("zlib read: %w", err)
	}
	return string(out), nil
}

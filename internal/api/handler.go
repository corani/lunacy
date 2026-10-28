package api

import (
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/corani/lunacy/internal/cover"
	"github.com/corani/lunacy/internal/logger"
	"github.com/corani/lunacy/internal/store"
)

// Handler holds the mux and store reference.
type Handler struct {
	mux      *http.ServeMux
	st       *store.Store
	coverDir string
	log      *logger.Logger
}

// New creates and wires all API routes onto a ServeMux.
func New(st *store.Store, coverDir string, fs embed.FS, log *logger.Logger) *Handler {
	h := &Handler{mux: http.NewServeMux(), st: st, coverDir: coverDir, log: log}
	h.routes()
	h.serveStatic(fs)
	return h
}

// App returns the handler for mounting in the main mux.
func (h *Handler) App() http.Handler { return h.mux }

func (h *Handler) routes() {
	h.mux.HandleFunc("GET /api/stats", h.getStats)
	h.mux.HandleFunc("GET /api/books", h.listBooks)
	h.mux.HandleFunc("GET /api/book", h.getBook)
	h.mux.HandleFunc("GET /api/book/annotations", h.getAnnotations)
	h.mux.HandleFunc("GET /api/book/cover", h.getCover)
	h.mux.HandleFunc("GET /api/authors", h.listAuthors)
	h.mux.HandleFunc("GET /api/series", h.listSeries)
	h.mux.HandleFunc("GET /api/author/cover", h.getAuthorCover)
	h.mux.HandleFunc("GET /api/series/cover", h.getSeriesCover)
}

func (h *Handler) serveStatic(fs embed.FS) {
	serve := func(path, mime, file string) {
		h.mux.HandleFunc("GET "+path, func(w http.ResponseWriter, r *http.Request) {
			data, err := fs.ReadFile(file)
			if err != nil {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", mime)
			w.Write(data)
		})
	}
	serve("/style.css", "text/css; charset=utf-8", "style.css")
	serve("/app.js", "application/javascript; charset=utf-8", "app.js")
	serve("/", "text/html; charset=utf-8", "index.html")
}

func (h *Handler) getStats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.st.GetStats()
	if err != nil {
		h.jsonError(w, r, http.StatusInternalServerError, err)
		return
	}
	h.writeJSON(w, stats)
}

func (h *Handler) listBooks(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	shelf := q.Get("shelf")
	author := q.Get("author")
	series := q.Get("series")
	search := q.Get("search")
	sortBy := queryStringDef(q, "sort", "recent")
	sortDir := queryStringDef(q, "dir", "desc")
	limit := queryIntDef(q, "limit", 48)
	offset := queryIntDef(q, "offset", 0)

	books, err := h.st.ListBooks(shelf, author, series, search, sortBy, sortDir, limit, offset)
	if err != nil {
		h.jsonError(w, r, http.StatusInternalServerError, err)
		return
	}
	h.writeJSON(w, books)
}

func (h *Handler) getBook(w http.ResponseWriter, r *http.Request) {
	filename := r.URL.Query().Get("f")
	book, err := h.st.GetBook(filename)
	if err != nil {
		h.jsonError(w, r, http.StatusNotFound, err)
		return
	}
	days, err := h.st.GetReadingDays(filename)
	if err != nil {
		h.jsonError(w, r, http.StatusInternalServerError, err)
		return
	}
	h.writeJSON(w, map[string]any{"book": book, "reading_days": days})
}

func (h *Handler) getAnnotations(w http.ResponseWriter, r *http.Request) {
	filename := r.URL.Query().Get("f")
	anns, err := h.st.GetAnnotations(filename)
	if err != nil {
		h.jsonError(w, r, http.StatusInternalServerError, err)
		return
	}
	h.writeJSON(w, anns)
}

func (h *Handler) getCover(w http.ResponseWriter, r *http.Request) {
	filename := r.URL.Query().Get("f")
	book, err := h.st.GetBook(filename)
	if err != nil {
		h.serveSVGCover(w, filename+"\n")
		return
	}
	if book.CoverFile != "" {
		if _, err := os.Stat(book.CoverFile); err == nil {
			http.ServeFile(w, r, book.CoverFile)
			return
		}
	}
	h.serveSVGCover(w, book.Title+"\n"+book.Author)
}

func (h *Handler) listAuthors(w http.ResponseWriter, r *http.Request) {
	authors, err := h.st.ListAuthors()
	if err != nil {
		h.jsonError(w, r, http.StatusInternalServerError, err)
		return
	}
	h.writeJSON(w, authors)
}

func (h *Handler) listSeries(w http.ResponseWriter, r *http.Request) {
	series, err := h.st.ListSeries()
	if err != nil {
		h.jsonError(w, r, http.StatusInternalServerError, err)
		return
	}
	h.writeJSON(w, series)
}

func (h *Handler) getAuthorCover(w http.ResponseWriter, r *http.Request) {
	h.serveSVGCover(w, r.URL.Query().Get("name"))
}

func (h *Handler) getSeriesCover(w http.ResponseWriter, r *http.Request) {
	h.serveSVGCover(w, r.URL.Query().Get("name"))
}

func (h *Handler) serveSVGCover(w http.ResponseWriter, key string) {
	w.Header().Set("Content-Type", "image/svg+xml")
	path, err := cover.Generate(h.coverDir, key)
	if err != nil {
		fmt.Fprint(w, cover.SVG(key))
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprint(w, cover.SVG(key))
		return
	}
	w.Write(data)
}

func (h *Handler) writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		h.log.Error("json encode", "err", err)
	}
}

func (h *Handler) jsonError(w http.ResponseWriter, r *http.Request, code int, err error) {
	h.log.Error("api", "method", r.Method, "path", r.URL.Path, "status", code, "err", err)
	http.Error(w, err.Error(), code)
}

// CoverDir returns the cover directory path.
func CoverDir(dataDir string) string {
	return filepath.Join(dataDir, "covers")
}

func queryStringDef(q interface{ Get(string) string }, key, def string) string {
	if v := q.Get(key); v != "" {
		return v
	}
	return def
}

func queryIntDef(q interface{ Get(string) string }, key string, def int) int {
	v, err := strconv.Atoi(q.Get(key))
	if err != nil {
		return def
	}
	return v
}

package api

import (
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"time"

	"github.com/corani/lunacy/internal/store"
)

var sincePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d{1,3})?(Z|[+-](0\d|1\d|2[0-3]):[0-5]\d)$`)

func (h *Handler) sync(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		h.apiError(w, r, http.StatusBadRequest, "invalid_query", "query parameters must be URL encoded", err)
		return
	}
	var since *int64
	if values, ok := query["since"]; ok {
		if len(values) != 1 || !sincePattern.MatchString(values[0]) {
			h.apiError(w, r, http.StatusBadRequest, "invalid_since",
				"since must be an RFC 3339 timestamp with at most millisecond precision", nil)
			return
		}
		at, err := time.Parse(time.RFC3339Nano, values[0])
		if err != nil {
			h.apiError(w, r, http.StatusBadRequest, "invalid_since",
				"since must be an RFC 3339 timestamp with at most millisecond precision", err)
			return
		}
		ms := at.UnixMilli()
		since = &ms
	}
	response, err := h.st.Sync(r.Context(), since)
	if err != nil {
		if errors.Is(err, store.ErrFutureSince) {
			h.apiError(w, r, http.StatusBadRequest, "invalid_since", err.Error(), err)
		} else {
			h.jsonError(w, r, http.StatusInternalServerError, err)
		}
		return
	}
	h.writeJSON(w, response)
}

func (h *Handler) syncFilename(w http.ResponseWriter, r *http.Request) (string, bool) {
	values := r.URL.Query()["filename"]
	if len(values) != 1 || values[0] == "" {
		h.apiError(w, r, http.StatusBadRequest, "invalid_filename", "filename must be one non-empty canonical identifier", nil)
		return "", false
	}
	return values[0], true
}

func (h *Handler) syncBook(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	filename, ok := h.syncFilename(w, r)
	if !ok {
		return
	}
	response, err := h.st.SyncBook(r.Context(), filename)
	if err != nil {
		h.syncQueryError(w, r, err)
		return
	}
	h.writeJSON(w, response)
}

func (h *Handler) syncQueryError(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusInternalServerError
	if errors.Is(err, sql.ErrNoRows) {
		status = http.StatusNotFound
	}
	h.jsonError(w, r, status, err)
}

func (h *Handler) syncCover(w http.ResponseWriter, r *http.Request) {
	filename, ok := h.syncFilename(w, r)
	if !ok {
		return
	}
	book, err := h.st.ExportedCover(r.Context(), filename)
	if err != nil {
		h.syncQueryError(w, r, err)
		return
	}
	if book.CoverFile != "" {
		if _, err := os.Stat(book.CoverFile); err == nil {
			http.ServeFile(w, r, book.CoverFile)
			return
		} else if !errors.Is(err, os.ErrNotExist) {
			h.jsonError(w, r, http.StatusInternalServerError, err)
			return
		}
		h.log.Error("cover unavailable; generating fallback", "filename", filename, "cover", book.CoverFile)
	}
	h.serveSVGCover(w, book.Title+"\n"+book.Author)
}

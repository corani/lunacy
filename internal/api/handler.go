package api

import (
	"embed"
	"os"
	"path/filepath"
	"strconv"

	"github.com/gofiber/fiber/v2"

	"github.com/corani/lunacy/internal/cover"
	"github.com/corani/lunacy/internal/logger"
	"github.com/corani/lunacy/internal/store"
)

// Handler holds the Fiber app and store reference.
type Handler struct {
	app      *fiber.App
	st       *store.Store
	coverDir string
	log      *logger.Logger
}

// New creates and wires all API routes onto a Fiber app.
func New(st *store.Store, coverDir string, fs embed.FS, log *logger.Logger) *Handler {
	h := &Handler{st: st, coverDir: coverDir, log: log}
	h.app = fiber.New(fiber.Config{
		DisableStartupMessage: true,
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			code := fiber.StatusInternalServerError
			if e, ok := err.(*fiber.Error); ok {
				code = e.Code
			}
			log.Error("api", "method", c.Method(), "path", c.Path(), "status", code, "err", err)
			return c.Status(code).SendString(err.Error())
		},
	})
	h.routes()
	h.serveStatic(fs)
	return h
}

// App returns the underlying Fiber app.
func (h *Handler) App() *fiber.App { return h.app }

func (h *Handler) routes() {
	h.app.Get("/api/stats", h.getStats)
	h.app.Get("/api/books", h.listBooks)
	h.app.Get("/api/book", h.getBook)
	h.app.Get("/api/book/annotations", h.getAnnotations)
	h.app.Get("/api/book/cover", h.getCover)
	h.app.Get("/api/authors", h.listAuthors)
	h.app.Get("/api/series", h.listSeries)
	h.app.Get("/api/author/cover", h.getAuthorCover)
	h.app.Get("/api/series/cover", h.getSeriesCover)
}

func (h *Handler) serveStatic(fs embed.FS) {
	serve := func(path, mime, file string) {
		h.app.Get(path, func(c *fiber.Ctx) error {
			data, err := fs.ReadFile(file)
			if err != nil {
				return fiber.ErrNotFound
			}
			c.Set("Content-Type", mime)
			return c.Send(data)
		})
	}
	serve("/style.css", "text/css; charset=utf-8", "style.css")
	serve("/app.js", "application/javascript; charset=utf-8", "app.js")
	serve("/", "text/html; charset=utf-8", "index.html")
}

func (h *Handler) getStats(c *fiber.Ctx) error {
	stats, err := h.st.GetStats()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(stats)
}

func (h *Handler) listBooks(c *fiber.Ctx) error {
	shelf := c.Query("shelf")
	author := c.Query("author")
	series := c.Query("series")
	search := c.Query("search")
	sortBy := c.Query("sort", "recent")
	sortDir := c.Query("dir", "desc")
	limit := queryInt(c, "limit", 48)
	offset := queryInt(c, "offset", 0)

	books, err := h.st.ListBooks(shelf, author, series, search, sortBy, sortDir, limit, offset)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(books)
}

func (h *Handler) getBook(c *fiber.Ctx) error {
	filename := c.Query("f")
	book, err := h.st.GetBook(filename)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, err.Error())
	}
	days, err := h.st.GetReadingDays(filename)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(fiber.Map{"book": book, "reading_days": days})
}

func (h *Handler) getAnnotations(c *fiber.Ctx) error {
	filename := c.Query("f")
	anns, err := h.st.GetAnnotations(filename)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(anns)
}

func (h *Handler) getCover(c *fiber.Ctx) error {
	filename := c.Query("f")
	book, err := h.st.GetBook(filename)
	if err != nil {
		return h.serveSVGCover(c, filename+"\n")
	}
	if book.CoverFile != "" {
		if _, err := os.Stat(book.CoverFile); err == nil {
			return c.SendFile(book.CoverFile)
		}
	}
	return h.serveSVGCover(c, book.Title+"\n"+book.Author)
}

func (h *Handler) listAuthors(c *fiber.Ctx) error {
	authors, err := h.st.ListAuthors()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(authors)
}

func (h *Handler) listSeries(c *fiber.Ctx) error {
	series, err := h.st.ListSeries()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(series)
}

func (h *Handler) getAuthorCover(c *fiber.Ctx) error {
	return h.serveSVGCover(c, c.Query("name"))
}

func (h *Handler) getSeriesCover(c *fiber.Ctx) error {
	return h.serveSVGCover(c, c.Query("name"))
}

func (h *Handler) serveSVGCover(c *fiber.Ctx, key string) error {
	path, err := cover.Generate(h.coverDir, key)
	if err != nil {
		// Fallback: generate inline without caching.
		c.Set("Content-Type", "image/svg+xml")
		return c.SendString(cover.SVG(key))
	}
	c.Set("Content-Type", "image/svg+xml")
	return c.SendFile(path)
}


func queryInt(c *fiber.Ctx, key string, def int) int {
	v, err := strconv.Atoi(c.Query(key))
	if err != nil {
		return def
	}
	return v
}

// CoverDir returns the cover directory path.
func CoverDir(dataDir string) string {
	return filepath.Join(dataDir, "covers")
}

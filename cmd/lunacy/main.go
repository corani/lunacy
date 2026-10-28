package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/corani/lunacy/internal/api"
	"github.com/corani/lunacy/internal/config"
	"github.com/corani/lunacy/internal/importer"
	"github.com/corani/lunacy/internal/logger"
	"github.com/corani/lunacy/internal/store"
	"github.com/corani/lunacy/internal/webdav"
	"github.com/corani/lunacy/static"
)

func main() {
	cfgPath := flag.String("config", "", "path to config.yaml (default: search standard locations)")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	log := logger.New("lunacy")

	// Ensure data directories exist.
	davRoot := filepath.Join(cfg.DataDir, "webdav")
	coverDir := filepath.Join(cfg.DataDir, "covers")
	for _, dir := range []string{cfg.DataDir, davRoot, coverDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Error("mkdir", "path", dir, "err", err)
			os.Exit(1)
		}
	}
	if err := webdav.MkdirAll(davRoot); err != nil {
		log.Error("webdav mkdir", "err", err)
		os.Exit(1)
	}

	// Open store.
	st, err := store.Open(cfg.DataDir)
	if err != nil {
		log.Error("open store", "err", err)
		os.Exit(1)
	}
	defer st.Close() //nolint:errcheck

	// Build importers.
	backupDir := filepath.Join(davRoot, "Backup")
	bi := importer.NewBackupImporter(st, backupDir, cfg.Backup.MaxCount, logger.New("backup"))

	// Build WebDAV handler.
	davHandler := webdav.New(davRoot, st, bi, logger.New("webdav"))
	authDav := webdav.BasicAuth(cfg.Server.Username, cfg.Server.Password, davHandler)

	// Build API/UI handler.
	apiHandler := api.New(st, coverDir, static.FS, logger.New("api"))

	// Route: /dav/* → WebDAV, everything else → API/UI.
	mux := http.NewServeMux()
	mux.Handle("/dav/", authDav)
	mux.Handle("/", apiHandler.App())

	log.Info("listening", "addr", cfg.Server.Addr)
	if err := http.ListenAndServe(cfg.Server.Addr, webdav.LoggingMiddleware(logger.New("http"), mux)); err != nil {
		log.Error("listen", "err", err)
		os.Exit(1)
	}
}

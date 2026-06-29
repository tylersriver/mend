// Command server is the mend web app: a single binary that opens (and migrates)
// the SQLite database, wires the store + AI service, and serves the HTTP UI.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/tylersriver/mend/internal/config"
	"github.com/tylersriver/mend/internal/db"
	"github.com/tylersriver/mend/internal/store"
	"github.com/tylersriver/mend/internal/web"
)

func main() {
	cfg := config.Load(os.Args[1:])

	// Ensure the DB's parent dir and the blob dir exist.
	if dir := filepath.Dir(cfg.DBPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Fatalf("create db dir: %v", err)
		}
	}
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		log.Fatalf("create data dir: %v", err)
	}

	database, err := db.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer database.Close()

	st := store.New(database)

	// AI/transcription clients are built inside the server from effective config
	// (in-app settings over env defaults) and can be reconfigured at runtime.
	srv := web.NewServer(st, cfg)

	if cfg.AuthEnabled() {
		log.Printf("auth enabled — login required")
	} else {
		log.Printf("auth DISABLED — set AUTH_PASSWORD to require a login")
	}

	httpSrv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Graceful shutdown on SIGINT/SIGTERM.
	idle := make(chan struct{})
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpSrv.Shutdown(ctx); err != nil {
			log.Printf("shutdown: %v", err)
		}
		close(idle)
	}()

	log.Printf("mend listening on %s (db %s)", cfg.Addr, cfg.DBPath)
	if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("serve: %v", err)
	}
	<-idle
}

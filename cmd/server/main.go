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

	// Embed the IANA timezone database so named zones (e.g. TZ=America/Chicago)
	// resolve even on the scratch image, which has no /usr/share/zoneinfo.
	_ "time/tzdata"

	"github.com/tylersriver/mend/internal/config"
	"github.com/tylersriver/mend/internal/db"
	"github.com/tylersriver/mend/internal/store"
	"github.com/tylersriver/mend/internal/web"
)

func main() {
	// `mend -healthcheck` makes one HTTP request to /healthz and exits 0/1. This
	// lets the scratch-based container HEALTHCHECK work without a shell or wget.
	if len(os.Args) > 1 && os.Args[1] == "-healthcheck" {
		os.Exit(healthcheck())
	}

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
	// Ensure a temp dir exists for multipart upload spillover — a scratch image
	// has no /tmp until we make it.
	if err := os.MkdirAll(os.TempDir(), 0o1777); err != nil {
		log.Printf("warning: create temp dir %s: %v", os.TempDir(), err)
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

	log.Printf("timezone: %s (set TZ to change; default UTC)", time.Now().Format("MST -07:00"))
	log.Printf("mend listening on %s (db=%s data=%s)", cfg.Addr, cfg.DBPath, cfg.DataDir)
	if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("serve: %v", err)
	}
	<-idle
}

// healthcheck probes the local /healthz endpoint, honoring the same PORT/ADDR
// resolution as the server. Returns a process exit code (0 = healthy).
func healthcheck() int {
	cfg := config.Load(nil)
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://127.0.0.1" + cfg.Addr + "/healthz")
	if err != nil {
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

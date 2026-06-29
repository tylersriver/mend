// Package db opens the SQLite database and applies embedded migrations. Uses the
// pure-Go modernc.org/sqlite driver so the build stays cgo-free: `go build`
// produces one static binary, matching the "one binary to deploy" design goal.
package db

import (
	"database/sql"
	"fmt"
	"io/fs"
	"sort"

	migrations "github.com/tylersriver/mend/db"
	_ "modernc.org/sqlite"
)

// Open opens (creating if needed) the SQLite database at path, enables the
// pragmas this app relies on, and runs any pending migrations.
func Open(path string) (*sql.DB, error) {
	// _pragma args set busy timeout + foreign keys at connection time. WAL gives
	// better read/write concurrency for the htmx round-trips.
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)&_pragma=journal_mode(WAL)", path)
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// SQLite is a single writer; keep the pool small to avoid lock churn.
	sqlDB.SetMaxOpenConns(1)
	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}
	if err := migrate(sqlDB); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return sqlDB, nil
}

// migrate applies every embedded migration that hasn't been recorded yet, in
// filename order, each in its own transaction. Version = the filename.
func migrate(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version    TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`); err != nil {
		return err
	}

	entries, err := fs.ReadDir(migrations.FS, "migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		var exists int
		if err := db.QueryRow(`SELECT 1 FROM schema_migrations WHERE version = ?`, name).Scan(&exists); err == nil {
			continue // already applied
		} else if err != sql.ErrNoRows {
			return err
		}

		body, err := fs.ReadFile(migrations.FS, "migrations/"+name)
		if err != nil {
			return err
		}

		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(body)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply %s: %w", name, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations (version) VALUES (?)`, name); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record %s: %w", name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit %s: %w", name, err)
		}
	}
	return nil
}

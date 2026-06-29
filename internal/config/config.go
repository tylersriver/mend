// Package config loads runtime configuration. Per the design log, a single-user
// web server needs only a handful of knobs, so this is env-first with flag
// overrides — no viper/cobra. Env vars win for secrets; flags are handy in dev.
package config

import (
	"flag"
	"os"
)

type Config struct {
	Addr    string // listen address, e.g. ":8080"
	DBPath  string // SQLite file path
	DataDir string // on-disk blob storage (uploads, recordings)
	APIKey  string // ANTHROPIC_API_KEY — never stored in the DB
	AIModel string // Anthropic model id used for care-prep flows
}

// Load reads env vars, then applies any explicitly-set flags on top.
func Load(args []string) *Config {
	c := &Config{
		Addr:    envOr("ADDR", ":8080"),
		DBPath:  envOr("DB_PATH", "data/mend.db"),
		DataDir: envOr("DATA_DIR", "data"),
		APIKey:  os.Getenv("ANTHROPIC_API_KEY"),
		AIModel: envOr("AI_MODEL", "claude-opus-4-8"),
	}

	fs := flag.NewFlagSet("mend", flag.ContinueOnError)
	fs.StringVar(&c.Addr, "addr", c.Addr, "listen address")
	fs.StringVar(&c.DBPath, "db", c.DBPath, "sqlite database path")
	fs.StringVar(&c.DataDir, "data", c.DataDir, "directory for uploaded blobs")
	fs.StringVar(&c.AIModel, "model", c.AIModel, "Anthropic model id")
	// API key is intentionally env-only; not a flag (avoids it landing in shell history).
	_ = fs.Parse(args)

	return c
}

// AIEnabled reports whether AI flows can run. Handlers degrade gracefully when false.
func (c *Config) AIEnabled() bool { return c.APIKey != "" }

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

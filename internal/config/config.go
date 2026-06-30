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
	APIKey    string // AI API key (ANTHROPIC_API_KEY) used for care-prep flows
	AIModel   string // model id used for care-prep flows
	AIBaseURL string // empty = Anthropic Messages API; else an OpenAI-compatible base (e.g. Groq)

	// Transcription (a Whisper-class API, kept separate from the Anthropic path).
	// Provider-agnostic: any OpenAI-compatible /audio/transcriptions endpoint works.
	TranscribeAPIKey  string
	TranscribeBaseURL string
	TranscribeModel   string

	// Auth: a single shared password gates the whole app (intended for a personal
	// deploy). When AuthPassword is empty, auth is disabled (local dev).
	AuthPassword  string
	SessionSecret string
}

// Load reads env vars, then applies any explicitly-set flags on top.
func Load(args []string) *Config {
	c := &Config{
		// Honor $PORT (Railway and most PaaS set it) unless ADDR is given explicitly.
		Addr:    envOr("ADDR", defaultAddr()),
		DBPath:  envOr("DB_PATH", "data/mend.db"),
		DataDir: envOr("DATA_DIR", "data"),
		APIKey:    os.Getenv("ANTHROPIC_API_KEY"),
		AIModel:   envOr("AI_MODEL", "claude-opus-4-8"),
		AIBaseURL: os.Getenv("AI_BASE_URL"),

		TranscribeAPIKey:  os.Getenv("TRANSCRIBE_API_KEY"),
		TranscribeBaseURL: envOr("TRANSCRIBE_BASE_URL", "https://api.openai.com/v1"),
		TranscribeModel:   envOr("TRANSCRIBE_MODEL", "whisper-1"),

		AuthPassword:  os.Getenv("AUTH_PASSWORD"),
		SessionSecret: os.Getenv("SESSION_SECRET"),
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

// TranscribeEnabled reports whether audio can be transcribed server-side.
func (c *Config) TranscribeEnabled() bool { return c.TranscribeAPIKey != "" }

// AuthEnabled reports whether the app requires a login.
func (c *Config) AuthEnabled() bool { return c.AuthPassword != "" }

func defaultAddr() string {
	if p := os.Getenv("PORT"); p != "" {
		return ":" + p
	}
	return ":8080"
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

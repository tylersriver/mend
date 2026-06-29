-- 0004_app_settings.sql
-- A single-row settings table so AI/transcription config can be edited in-app
-- (handy for a personal deploy) instead of only via env. Effective config is
-- "DB value if set, else the env default", resolved at read time. Note: this
-- relaxes the original "keep the API key in env, not a SQLite row" stance — an
-- accepted tradeoff for a single-user, login-gated instance.

PRAGMA foreign_keys = ON;

CREATE TABLE app_settings (
    id                  INTEGER PRIMARY KEY CHECK (id = 1),
    anthropic_api_key   TEXT NOT NULL DEFAULT '',
    ai_model            TEXT NOT NULL DEFAULT '',
    transcribe_api_key  TEXT NOT NULL DEFAULT '',
    transcribe_base_url TEXT NOT NULL DEFAULT '',
    transcribe_model    TEXT NOT NULL DEFAULT '',
    updated_at          TEXT NOT NULL DEFAULT (datetime('now'))
);

INSERT INTO app_settings (id) VALUES (1);

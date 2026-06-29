-- 0002_research_care.sql
-- The spine: research library, providers/appointments (prep vs outcome),
-- recordings (transcription pipeline), and AI documents (editable artifacts).

PRAGMA foreign_keys = ON;

-- Unified research library: files, links, and videos are one thing with a `kind`
CREATE TABLE resources (
    id          INTEGER PRIMARY KEY,
    kind        TEXT NOT NULL,    -- 'file'|'link'|'video'
    title       TEXT NOT NULL,
    url         TEXT,             -- link/video; NULL for file
    file_path   TEXT,             -- on-disk path; NULL for link/video
    mime_type   TEXT,
    size_bytes  INTEGER,
    source      TEXT,             -- 'youtube'|'pubmed'|'clinic'|'reddit'
    summary     TEXT,             -- your notes OR an AI-generated summary (reused as AI context)
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE tags (
    id   INTEGER PRIMARY KEY,
    name TEXT UNIQUE NOT NULL
);

CREATE TABLE resource_tags (
    resource_id INTEGER REFERENCES resources(id) ON DELETE CASCADE,
    tag_id      INTEGER REFERENCES tags(id) ON DELETE CASCADE,
    PRIMARY KEY (resource_id, tag_id)
);

-- Stable provider records
CREATE TABLE providers (
    id        INTEGER PRIMARY KEY,
    name      TEXT NOT NULL,
    specialty TEXT,              -- 'orthopedic surgeon'|'physical therapist'|'radiologist'
    clinic    TEXT,
    contact   TEXT,
    notes     TEXT
);

-- Appointments split prep (questions) from outcome (what happened)
CREATE TABLE appointments (
    id           INTEGER PRIMARY KEY,
    provider_id  INTEGER REFERENCES providers(id),
    kind         TEXT NOT NULL,   -- 'consult'|'follow_up'|'pt'|'imaging'|'procedure'
    scheduled_at TEXT NOT NULL,
    status       TEXT NOT NULL DEFAULT 'upcoming',  -- 'upcoming'|'completed'|'cancelled'
    prep_notes   TEXT,            -- questions to raise (often AI-generated)
    outcome      TEXT,            -- what happened, recommendations, dx changes
    follow_up_on TEXT,
    created_at   TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Audio recordings + transcription pipeline state
CREATE TABLE recordings (
    id                INTEGER PRIMARY KEY,
    appointment_id    INTEGER REFERENCES appointments(id) ON DELETE SET NULL,
    audio_path        TEXT,
    duration_sec      INTEGER,
    transcript        TEXT,
    transcript_status TEXT NOT NULL DEFAULT 'pending',  -- pending|processing|done|failed
    created_at        TEXT NOT NULL DEFAULT (datetime('now'))
);

-- AI output stored as editable artifacts, not chat logs.
-- source_kind/source_id is a provenance breadcrumb only (no cascade).
CREATE TABLE ai_documents (
    id          INTEGER PRIMARY KEY,
    doc_type    TEXT NOT NULL,    -- 'doctor_questions'|'plan'|'highlights'|'tasks'|'summary'
    title       TEXT,
    content     TEXT NOT NULL,    -- markdown, editable
    model       TEXT,
    source_kind TEXT,             -- 'recording'|'appointment'|'resource'|'manual'
    source_id   INTEGER,
    is_edited   INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_resources_kind       ON resources(kind);
CREATE INDEX idx_appointments_status  ON appointments(status, scheduled_at);
CREATE INDEX idx_recordings_status    ON recordings(transcript_status);
CREATE INDEX idx_ai_docs_source       ON ai_documents(source_kind, source_id);

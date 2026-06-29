-- 0001_pt_tracking.sql
-- PT tracking: the satellite. Prescription (protocol_exercises) is kept separate
-- from performance (sets); measurements is a tall table for anything charted.

PRAGMA foreign_keys = ON;

-- Reusable exercise definitions
CREATE TABLE exercises (
    id           INTEGER PRIMARY KEY,
    name         TEXT NOT NULL,
    metric_type  TEXT NOT NULL,   -- 'strength'|'band'|'hold'|'mobility' — drives UI inputs
    category     TEXT,            -- 'rotator_cuff'|'scapular'|'mobility'|'compound'
    cues         TEXT,            -- form notes to self
    demo_url     TEXT,            -- YouTube link lives on the exercise
    is_archived  INTEGER NOT NULL DEFAULT 0,
    created_at   TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Each protocol = one rehab phase, effective over a date range
CREATE TABLE protocols (
    id          INTEGER PRIMARY KEY,
    name        TEXT NOT NULL,    -- 'Phase 1 – Conservative'
    notes       TEXT,
    started_on  TEXT NOT NULL,
    ended_on    TEXT,             -- NULL = currently active
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Prescription: what the plan tells you to do
CREATE TABLE protocol_exercises (
    id                  INTEGER PRIMARY KEY,
    protocol_id         INTEGER NOT NULL REFERENCES protocols(id) ON DELETE CASCADE,
    exercise_id         INTEGER NOT NULL REFERENCES exercises(id),
    target_sets         INTEGER,
    target_reps         INTEGER,
    target_load         REAL,
    target_duration_sec INTEGER,
    target_rpe          REAL,
    frequency           TEXT,     -- 'daily','3x/week'
    progression         TEXT,     -- 'add 1 rep when RPE<6'
    sort_order          INTEGER NOT NULL DEFAULT 0
);

-- One PT bout on a date
CREATE TABLE sessions (
    id           INTEGER PRIMARY KEY,
    protocol_id  INTEGER REFERENCES protocols(id),  -- which phase was active
    performed_at TEXT NOT NULL DEFAULT (datetime('now')),
    duration_min INTEGER,
    pain_pre     INTEGER,         -- 0–10
    pain_post    INTEGER,
    notes        TEXT
);

-- Actual performed work (one row per set)
CREATE TABLE sets (
    id           INTEGER PRIMARY KEY,
    session_id   INTEGER NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    exercise_id  INTEGER NOT NULL REFERENCES exercises(id),
    set_number   INTEGER NOT NULL,
    reps         INTEGER,
    load         REAL,
    duration_sec INTEGER,
    rom_degrees  REAL,
    resistance   TEXT,            -- band color/level
    rpe          REAL,
    pain         INTEGER,         -- 0–10 during the movement
    notes        TEXT
);

-- Tall metrics table — the chart backbone
CREATE TABLE measurements (
    id          INTEGER PRIMARY KEY,
    measured_at TEXT NOT NULL DEFAULT (datetime('now')),
    metric      TEXT NOT NULL,    -- 'pain_daily'|'rom_flexion'|'rom_external_rot'|'grip_strength'
    value       REAL NOT NULL,
    unit        TEXT,             -- 'deg'|'lbs'|'0-10'
    side        TEXT,             -- 'left'|'right' — injured vs baseline
    notes       TEXT
);

CREATE INDEX idx_sets_session       ON sets(session_id);
CREATE INDEX idx_sets_exercise      ON sets(exercise_id);
CREATE INDEX idx_sessions_performed ON sessions(performed_at);
CREATE INDEX idx_meas_metric_time   ON measurements(metric, measured_at);

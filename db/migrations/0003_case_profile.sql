-- 0003_case_profile.sql
-- A single-row profile for the one injury this app is organized around. Gives the
-- "working diagnosis" a durable home (read by the AI case-file context) and the
-- dashboard a header. Enforced single-row via a CHECK on the primary key.

PRAGMA foreign_keys = ON;

CREATE TABLE case_profile (
    id                INTEGER PRIMARY KEY CHECK (id = 1),
    injury_name       TEXT NOT NULL DEFAULT '',
    working_diagnosis TEXT NOT NULL DEFAULT '',
    onset_on          TEXT,
    notes             TEXT,
    updated_at        TEXT NOT NULL DEFAULT (datetime('now'))
);

INSERT INTO case_profile (id, injury_name, working_diagnosis)
VALUES (1, 'Shoulder injury', '');

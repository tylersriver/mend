-- 0006_tasks.sql
-- A lightweight personal to-do list ("call to schedule PT", "pick up meds").
-- Distinct from the AI-generated 'tasks' documents — these are quick, checkable items.

CREATE TABLE tasks (
    id         INTEGER PRIMARY KEY,
    title      TEXT NOT NULL,
    done       INTEGER NOT NULL DEFAULT 0,
    done_at    TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_tasks_done ON tasks(done, created_at);

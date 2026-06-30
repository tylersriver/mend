package store

import "context"

// Task is a quick personal to-do item (distinct from AI-generated task documents).
type Task struct {
	ID        int64
	Title     string
	Done      bool
	DoneAt    string
	CreatedAt string
}

const taskCols = `id, title, done, COALESCE(done_at,''), created_at`

func scanTask(sc interface{ Scan(...any) error }) (Task, error) {
	var t Task
	err := sc.Scan(&t.ID, &t.Title, &t.Done, &t.DoneAt, &t.CreatedAt)
	return t, err
}

// ListTasks returns open items first (newest first within each group).
func (s *Store) ListTasks(ctx context.Context) ([]Task, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+taskCols+` FROM tasks ORDER BY done ASC, created_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) Task(ctx context.Context, id int64) (Task, error) {
	return scanTask(s.db.QueryRowContext(ctx, `SELECT `+taskCols+` FROM tasks WHERE id = ?`, id))
}

func (s *Store) AddTask(ctx context.Context, title string) (Task, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO tasks (title) VALUES (?)`, title)
	if err != nil {
		return Task{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Task{}, err
	}
	return s.Task(ctx, id)
}

// ToggleTask flips done, stamping/clearing done_at, and returns the updated task.
func (s *Store) ToggleTask(ctx context.Context, id int64) (Task, error) {
	_, err := s.db.ExecContext(ctx, `
		UPDATE tasks
		SET done = NOT done,
		    done_at = CASE WHEN done = 0 THEN datetime('now') ELSE NULL END
		WHERE id = ?`, id)
	if err != nil {
		return Task{}, err
	}
	return s.Task(ctx, id)
}

func (s *Store) DeleteTask(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM tasks WHERE id = ?`, id)
	return err
}

func (s *Store) OpenTaskCount(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks WHERE done = 0`).Scan(&n)
	return n, err
}

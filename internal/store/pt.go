package store

import (
	"context"
	"database/sql"
)

// PT logging satellite. Per the design log this ships
// exercises → sessions → sets → measurements; the prescription side
// (protocols / protocol_exercises) is deliberately deferred. Different exercises
// track different metrics, so the metric columns on `sets` are wide + nullable and
// modeled here as pointers (nil = not recorded), driven by the exercise metric_type.

type Exercise struct {
	ID         int64
	Name       string
	MetricType string // strength|band|hold|mobility — drives which set inputs show
	Category   string
	Cues       string
	DemoURL    string
	IsArchived bool
	CreatedAt  string
}

type Session struct {
	ID           int64
	ProtocolID   *int64
	ProtocolName string // joined; populated by Session()
	PerformedAt  string
	DurationMin  *int64
	PainPre      *int64
	PainPost     *int64
	Notes        string
	SetCount     int
	Sets         []Set // populated by Session(), not by ListSessions()
}

type Set struct {
	ID           int64
	SessionID    int64
	ExerciseID   int64
	ExerciseName string
	MetricType   string
	SetNumber    int64
	Reps         *int64
	Load         *float64
	DurationSec  *int64
	ROMDegrees   *float64
	Resistance   string
	RPE          *float64
	Pain         *int64
	Notes        string
}

type Measurement struct {
	ID         int64
	MeasuredAt string
	Metric     string
	Value      float64
	Unit       string
	Side       string
	Notes      string
}

// --- Exercises --------------------------------------------------------------

func (s *Store) ListExercises(ctx context.Context, includeArchived bool) ([]Exercise, error) {
	q := `SELECT id, name, metric_type, COALESCE(category,''), COALESCE(cues,''),
	             COALESCE(demo_url,''), is_archived, created_at
	      FROM exercises`
	if !includeArchived {
		q += ` WHERE is_archived = 0`
	}
	q += ` ORDER BY is_archived, name`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Exercise
	for rows.Next() {
		var e Exercise
		if err := rows.Scan(&e.ID, &e.Name, &e.MetricType, &e.Category, &e.Cues,
			&e.DemoURL, &e.IsArchived, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) Exercise(ctx context.Context, id int64) (Exercise, error) {
	var e Exercise
	err := s.db.QueryRowContext(ctx, `
		SELECT id, name, metric_type, COALESCE(category,''), COALESCE(cues,''),
		       COALESCE(demo_url,''), is_archived, created_at
		FROM exercises WHERE id = ?`, id).
		Scan(&e.ID, &e.Name, &e.MetricType, &e.Category, &e.Cues, &e.DemoURL, &e.IsArchived, &e.CreatedAt)
	return e, err
}

func (s *Store) CreateExercise(ctx context.Context, e Exercise) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO exercises (name, metric_type, category, cues, demo_url)
		VALUES (?, ?, ?, ?, ?)`,
		e.Name, e.MetricType, nullify(e.Category), nullify(e.Cues), nullify(e.DemoURL))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) SetExerciseArchived(ctx context.Context, id int64, archived bool) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE exercises SET is_archived = ? WHERE id = ?`, boolInt(archived), id)
	return err
}

// --- Sessions ---------------------------------------------------------------

func (s *Store) ListSessions(ctx context.Context) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT se.id, se.performed_at, se.duration_min, se.pain_pre, se.pain_post,
		       COALESCE(se.notes,''),
		       (SELECT COUNT(*) FROM sets WHERE session_id = se.id)
		FROM sessions se ORDER BY se.performed_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		var se Session
		var dur, pre, post sql.NullInt64
		if err := rows.Scan(&se.ID, &se.PerformedAt, &dur, &pre, &post, &se.Notes,
			&se.SetCount); err != nil {
			return nil, err
		}
		se.DurationMin, se.PainPre, se.PainPost = niptr(dur), niptr(pre), niptr(post)
		out = append(out, se)
	}
	return out, rows.Err()
}

func (s *Store) Session(ctx context.Context, id int64) (Session, error) {
	var se Session
	var dur, pre, post, protoID sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
		SELECT se.id, se.performed_at, se.duration_min, se.pain_pre, se.pain_post,
		       COALESCE(se.notes,''), se.protocol_id, COALESCE(pr.name,'')
		FROM sessions se LEFT JOIN protocols pr ON pr.id = se.protocol_id
		WHERE se.id = ?`, id).
		Scan(&se.ID, &se.PerformedAt, &dur, &pre, &post, &se.Notes, &protoID, &se.ProtocolName)
	if err != nil {
		return se, err
	}
	se.DurationMin, se.PainPre, se.PainPost = niptr(dur), niptr(pre), niptr(post)
	se.ProtocolID = niptr(protoID)
	se.Sets, err = s.SetsForSession(ctx, id)
	return se, err
}

func (s *Store) CreateSession(ctx context.Context, se Session) (int64, error) {
	// Empty performed_at falls back to now() so a quick log doesn't require a date.
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO sessions (performed_at, protocol_id, duration_min, pain_pre, pain_post, notes)
		VALUES (COALESCE(NULLIF(?,''), datetime('now')), ?, ?, ?, ?, ?)`,
		se.PerformedAt, i64val(se.ProtocolID), i64val(se.DurationMin),
		i64val(se.PainPre), i64val(se.PainPost), nullify(se.Notes))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateSession(ctx context.Context, se Session) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE sessions SET duration_min = ?, pain_pre = ?, pain_post = ?, notes = ?
		WHERE id = ?`,
		i64val(se.DurationMin), i64val(se.PainPre), i64val(se.PainPost), nullify(se.Notes), se.ID)
	return err
}

func (s *Store) DeleteSession(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	return err
}

// --- Sets -------------------------------------------------------------------

const setCols = `s.id, s.session_id, s.exercise_id, e.name, e.metric_type, s.set_number,
	s.reps, s.load, s.duration_sec, s.rom_degrees, COALESCE(s.resistance,''),
	s.rpe, s.pain, COALESCE(s.notes,'')`

func scanSet(sc interface{ Scan(...any) error }) (Set, error) {
	var st Set
	var reps, durSec, pain sql.NullInt64
	var load, rom, rpe sql.NullFloat64
	err := sc.Scan(&st.ID, &st.SessionID, &st.ExerciseID, &st.ExerciseName, &st.MetricType,
		&st.SetNumber, &reps, &load, &durSec, &rom, &st.Resistance, &rpe, &pain, &st.Notes)
	st.Reps, st.DurationSec, st.Pain = niptr(reps), niptr(durSec), niptr(pain)
	st.Load, st.ROMDegrees, st.RPE = nfptr(load), nfptr(rom), nfptr(rpe)
	return st, err
}

func (s *Store) SetsForSession(ctx context.Context, sessionID int64) ([]Set, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+setCols+`
		FROM sets s JOIN exercises e ON e.id = s.exercise_id
		WHERE s.session_id = ? ORDER BY s.id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Set
	for rows.Next() {
		st, err := scanSet(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

func (s *Store) Set(ctx context.Context, id int64) (Set, error) {
	return scanSet(s.db.QueryRowContext(ctx, `SELECT `+setCols+`
		FROM sets s JOIN exercises e ON e.id = s.exercise_id WHERE s.id = ?`, id))
}

// AddSet appends a set, auto-numbering it within its (session, exercise) pair.
func (s *Store) AddSet(ctx context.Context, st Set) (int64, error) {
	var next int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(set_number),0)+1 FROM sets WHERE session_id = ? AND exercise_id = ?`,
		st.SessionID, st.ExerciseID).Scan(&next); err != nil {
		return 0, err
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO sets (session_id, exercise_id, set_number, reps, load, duration_sec,
		                  rom_degrees, resistance, rpe, pain, notes)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		st.SessionID, st.ExerciseID, next, i64val(st.Reps), f64val(st.Load),
		i64val(st.DurationSec), f64val(st.ROMDegrees), nullify(st.Resistance),
		f64val(st.RPE), i64val(st.Pain), nullify(st.Notes))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) DeleteSet(ctx context.Context, id int64) (int64, error) {
	var sessionID int64
	if err := s.db.QueryRowContext(ctx, `SELECT session_id FROM sets WHERE id = ?`, id).Scan(&sessionID); err != nil {
		return 0, err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM sets WHERE id = ?`, id)
	return sessionID, err
}

// --- Measurements -----------------------------------------------------------

func (s *Store) ListMeasurements(ctx context.Context) ([]Measurement, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, measured_at, metric, value, COALESCE(unit,''), COALESCE(side,''), COALESCE(notes,'')
		FROM measurements ORDER BY measured_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMeasurements(rows)
}

// MeasurementSeries groups measurements by metric in ascending time order, for charts.
func (s *Store) MeasurementSeries(ctx context.Context) (map[string][]Measurement, []string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, measured_at, metric, value, COALESCE(unit,''), COALESCE(side,''), COALESCE(notes,'')
		FROM measurements ORDER BY metric, measured_at ASC, id ASC`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	all, err := scanMeasurements(rows)
	if err != nil {
		return nil, nil, err
	}
	series := map[string][]Measurement{}
	var order []string
	for _, m := range all {
		if _, ok := series[m.Metric]; !ok {
			order = append(order, m.Metric)
		}
		series[m.Metric] = append(series[m.Metric], m)
	}
	return series, order, nil
}

func (s *Store) CreateMeasurement(ctx context.Context, m Measurement) (int64, error) {
	// Empty measured_at falls back to now() in SQL so the column default semantics hold.
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO measurements (measured_at, metric, value, unit, side, notes)
		VALUES (COALESCE(NULLIF(?,''), datetime('now')), ?, ?, ?, ?, ?)`,
		m.MeasuredAt, m.Metric, m.Value, nullify(m.Unit), nullify(m.Side), nullify(m.Notes))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) DeleteMeasurement(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM measurements WHERE id = ?`, id)
	return err
}

func scanMeasurements(rows *sql.Rows) ([]Measurement, error) {
	var out []Measurement
	for rows.Next() {
		var m Measurement
		if err := rows.Scan(&m.ID, &m.MeasuredAt, &m.Metric, &m.Value, &m.Unit, &m.Side, &m.Notes); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// --- nullable helpers -------------------------------------------------------

func niptr(n sql.NullInt64) *int64 {
	if n.Valid {
		v := n.Int64
		return &v
	}
	return nil
}

func nfptr(n sql.NullFloat64) *float64 {
	if n.Valid {
		v := n.Float64
		return &v
	}
	return nil
}

func i64val(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

func f64val(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

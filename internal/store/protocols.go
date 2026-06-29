package store

import (
	"context"
	"database/sql"
)

// PT prescription: protocols (rehab phases) and their prescribed exercises. This
// is the deferred half of the PT satellite — the plan side, kept separate from
// performance (the `sets` table) so the two can be compared for compliance.

type Protocol struct {
	ID            int64
	Name          string
	Notes         string
	StartedOn     string
	EndedOn       string // "" = currently active
	CreatedAt     string
	ExerciseCount int
}

func (p Protocol) Active() bool { return p.EndedOn == "" }

type ProtocolExercise struct {
	ID                int64
	ProtocolID        int64
	ExerciseID        int64
	ExerciseName      string
	MetricType        string
	TargetSets        *int64
	TargetReps        *int64
	TargetLoad        *float64
	TargetDurationSec *int64
	TargetRPE         *float64
	Frequency         string
	Progression       string
	SortOrder         int64
}

// --- Protocols --------------------------------------------------------------

func (s *Store) ListProtocols(ctx context.Context) ([]Protocol, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT pr.id, pr.name, COALESCE(pr.notes,''), pr.started_on,
		       COALESCE(pr.ended_on,''), pr.created_at,
		       (SELECT COUNT(*) FROM protocol_exercises WHERE protocol_id = pr.id)
		FROM protocols pr
		ORDER BY (pr.ended_on IS NULL) DESC, pr.started_on DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Protocol
	for rows.Next() {
		var p Protocol
		if err := rows.Scan(&p.ID, &p.Name, &p.Notes, &p.StartedOn, &p.EndedOn,
			&p.CreatedAt, &p.ExerciseCount); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) Protocol(ctx context.Context, id int64) (Protocol, error) {
	var p Protocol
	err := s.db.QueryRowContext(ctx, `
		SELECT id, name, COALESCE(notes,''), started_on, COALESCE(ended_on,''), created_at
		FROM protocols WHERE id = ?`, id).
		Scan(&p.ID, &p.Name, &p.Notes, &p.StartedOn, &p.EndedOn, &p.CreatedAt)
	return p, err
}

// ActiveProtocolID returns the most recent open phase, or 0 if none is active.
func (s *Store) ActiveProtocolID(ctx context.Context) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `
		SELECT id FROM protocols WHERE ended_on IS NULL
		ORDER BY started_on DESC, id DESC LIMIT 1`).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return id, err
}

func (s *Store) CreateProtocol(ctx context.Context, p Protocol) (int64, error) {
	// Empty started_on defaults to today.
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO protocols (name, notes, started_on)
		VALUES (?, ?, COALESCE(NULLIF(?,''), date('now')))`,
		p.Name, nullify(p.Notes), p.StartedOn)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// EndProtocol closes a phase (sets ended_on to today) if it isn't already ended.
func (s *Store) EndProtocol(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE protocols SET ended_on = date('now') WHERE id = ? AND ended_on IS NULL`, id)
	return err
}

func (s *Store) DeleteProtocol(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM protocols WHERE id = ?`, id)
	return err
}

// --- Protocol exercises (the prescription) ----------------------------------

const protoExCols = `pe.id, pe.protocol_id, pe.exercise_id, e.name, e.metric_type,
	pe.target_sets, pe.target_reps, pe.target_load, pe.target_duration_sec,
	pe.target_rpe, COALESCE(pe.frequency,''), COALESCE(pe.progression,''), pe.sort_order`

func scanProtoEx(sc interface{ Scan(...any) error }) (ProtocolExercise, error) {
	var pe ProtocolExercise
	var tSets, tReps, tDur sql.NullInt64
	var tLoad, tRPE sql.NullFloat64
	err := sc.Scan(&pe.ID, &pe.ProtocolID, &pe.ExerciseID, &pe.ExerciseName, &pe.MetricType,
		&tSets, &tReps, &tLoad, &tDur, &tRPE, &pe.Frequency, &pe.Progression, &pe.SortOrder)
	pe.TargetSets, pe.TargetReps, pe.TargetDurationSec = niptr(tSets), niptr(tReps), niptr(tDur)
	pe.TargetLoad, pe.TargetRPE = nfptr(tLoad), nfptr(tRPE)
	return pe, err
}

func (s *Store) ProtocolExercises(ctx context.Context, protocolID int64) ([]ProtocolExercise, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+protoExCols+`
		FROM protocol_exercises pe JOIN exercises e ON e.id = pe.exercise_id
		WHERE pe.protocol_id = ? ORDER BY pe.sort_order, pe.id`, protocolID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProtocolExercise
	for rows.Next() {
		pe, err := scanProtoEx(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, pe)
	}
	return out, rows.Err()
}

func (s *Store) AddProtocolExercise(ctx context.Context, pe ProtocolExercise) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO protocol_exercises
		  (protocol_id, exercise_id, target_sets, target_reps, target_load,
		   target_duration_sec, target_rpe, frequency, progression, sort_order)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		pe.ProtocolID, pe.ExerciseID, i64val(pe.TargetSets), i64val(pe.TargetReps),
		f64val(pe.TargetLoad), i64val(pe.TargetDurationSec), f64val(pe.TargetRPE),
		nullify(pe.Frequency), nullify(pe.Progression), pe.SortOrder)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// DeleteProtocolExercise removes a prescription row and returns its protocol id
// (for redirecting back to the protocol).
func (s *Store) DeleteProtocolExercise(ctx context.Context, id int64) (int64, error) {
	var protocolID int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT protocol_id FROM protocol_exercises WHERE id = ?`, id).Scan(&protocolID); err != nil {
		return 0, err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM protocol_exercises WHERE id = ?`, id)
	return protocolID, err
}

// LastSetForExercise returns the most recent set logged for an exercise within a
// given protocol's sessions — the "performance" side of a compliance comparison.
// Returns ok=false when nothing has been logged yet.
func (s *Store) LastSetForExercise(ctx context.Context, protocolID, exerciseID int64) (Set, bool, error) {
	st, err := scanSet(s.db.QueryRowContext(ctx, `SELECT `+setCols+`
		FROM sets s
		JOIN exercises e ON e.id = s.exercise_id
		JOIN sessions se ON se.id = s.session_id
		WHERE se.protocol_id = ? AND s.exercise_id = ?
		ORDER BY se.performed_at DESC, s.id DESC LIMIT 1`, protocolID, exerciseID))
	if err == sql.ErrNoRows {
		return Set{}, false, nil
	}
	if err != nil {
		return Set{}, false, err
	}
	return st, true, nil
}

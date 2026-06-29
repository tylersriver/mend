// Package store is the persistence layer over SQLite. It exposes typed CRUD for
// the web handlers and implements ai.Repo so the AI flows can read the case file
// and write back editable artifacts. Nullable columns are normalized to ” / 0
// via COALESCE on read, keeping the Go types free of sql.Null* noise.
package store

import (
	"context"
	"database/sql"
	"log"
	"strings"

	"github.com/tylersriver/mend/internal/ai"
)

type Store struct{ db *sql.DB }

func New(db *sql.DB) *Store { return &Store{db: db} }

// --- Domain types -----------------------------------------------------------

type CaseProfile struct {
	InjuryName       string
	WorkingDiagnosis string
	OnsetOn          string
	Notes            string
	UpdatedAt        string
}

type Resource struct {
	ID        int64
	Kind      string // file|link|video
	Title     string
	URL       string
	FilePath  string
	MimeType  string
	SizeBytes int64
	Source    string
	Summary   string
	CreatedAt string
}

type Provider struct {
	ID        int64
	Name      string
	Specialty string
	Clinic    string
	Contact   string
	Notes     string
}

type Appointment struct {
	ID           int64
	ProviderID   int64
	ProviderName string
	Kind         string
	ScheduledAt  string
	Status       string
	PrepNotes    string
	Outcome      string
	FollowUpOn   string
	CreatedAt    string
}

type AIDoc struct {
	ID         int64
	DocType    string
	Title      string
	Content    string
	Model      string
	SourceKind string
	SourceID   int64
	IsEdited   bool
	CreatedAt  string
}

type Recording struct {
	ID               int64
	AppointmentID    int64
	AppointmentLabel string // joined provider + kind, for display
	AudioPath        string
	DurationSec      int64
	Transcript       string
	TranscriptStatus string // pending|processing|done|failed
	CreatedAt        string
}

// --- Case profile -----------------------------------------------------------

func (s *Store) Profile(ctx context.Context) (CaseProfile, error) {
	var p CaseProfile
	err := s.db.QueryRowContext(ctx, `
		SELECT injury_name, working_diagnosis, COALESCE(onset_on,''),
		       COALESCE(notes,''), updated_at
		FROM case_profile WHERE id = 1`).
		Scan(&p.InjuryName, &p.WorkingDiagnosis, &p.OnsetOn, &p.Notes, &p.UpdatedAt)
	return p, err
}

func (s *Store) UpdateProfile(ctx context.Context, p CaseProfile) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE case_profile
		SET injury_name = ?, working_diagnosis = ?, onset_on = ?, notes = ?,
		    updated_at = datetime('now')
		WHERE id = 1`,
		p.InjuryName, p.WorkingDiagnosis, nullify(p.OnsetOn), nullify(p.Notes))
	return err
}

// --- Resources --------------------------------------------------------------

const resourceCols = `id, kind, title, COALESCE(url,''), COALESCE(file_path,''),
	COALESCE(mime_type,''), COALESCE(size_bytes,0), COALESCE(source,''),
	COALESCE(summary,''), created_at`

func scanResource(sc interface{ Scan(...any) error }) (Resource, error) {
	var r Resource
	err := sc.Scan(&r.ID, &r.Kind, &r.Title, &r.URL, &r.FilePath, &r.MimeType,
		&r.SizeBytes, &r.Source, &r.Summary, &r.CreatedAt)
	return r, err
}

func (s *Store) Resources(ctx context.Context) ([]Resource, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+resourceCols+` FROM resources ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Resource
	for rows.Next() {
		r, err := scanResource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) RecentResources(ctx context.Context, limit int) ([]Resource, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+resourceCols+` FROM resources ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Resource
	for rows.Next() {
		r, err := scanResource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) Resource(ctx context.Context, id int64) (Resource, error) {
	return scanResource(s.db.QueryRowContext(ctx, `SELECT `+resourceCols+` FROM resources WHERE id = ?`, id))
}

func (s *Store) CreateResource(ctx context.Context, r Resource) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO resources (kind, title, url, file_path, mime_type, size_bytes, source, summary)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		r.Kind, r.Title, nullify(r.URL), nullify(r.FilePath), nullify(r.MimeType),
		nullZero(r.SizeBytes), nullify(r.Source), nullify(r.Summary))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) DeleteResource(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM resources WHERE id = ?`, id)
	return err
}

// --- Providers --------------------------------------------------------------

func (s *Store) Providers(ctx context.Context) ([]Provider, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, COALESCE(specialty,''), COALESCE(clinic,''),
		       COALESCE(contact,''), COALESCE(notes,'')
		FROM providers ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Provider
	for rows.Next() {
		var p Provider
		if err := rows.Scan(&p.ID, &p.Name, &p.Specialty, &p.Clinic, &p.Contact, &p.Notes); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) CreateProvider(ctx context.Context, p Provider) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO providers (name, specialty, clinic, contact, notes)
		VALUES (?, ?, ?, ?, ?)`,
		p.Name, nullify(p.Specialty), nullify(p.Clinic), nullify(p.Contact), nullify(p.Notes))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// --- Appointments -----------------------------------------------------------

const apptCols = `a.id, COALESCE(a.provider_id,0), COALESCE(p.name,''), a.kind,
	a.scheduled_at, a.status, COALESCE(a.prep_notes,''), COALESCE(a.outcome,''),
	COALESCE(a.follow_up_on,''), a.created_at`

func scanAppt(sc interface{ Scan(...any) error }) (Appointment, error) {
	var a Appointment
	err := sc.Scan(&a.ID, &a.ProviderID, &a.ProviderName, &a.Kind, &a.ScheduledAt,
		&a.Status, &a.PrepNotes, &a.Outcome, &a.FollowUpOn, &a.CreatedAt)
	return a, err
}

func (s *Store) Appointments(ctx context.Context) ([]Appointment, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+apptCols+`
		FROM appointments a LEFT JOIN providers p ON p.id = a.provider_id
		ORDER BY a.scheduled_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Appointment
	for rows.Next() {
		a, err := scanAppt(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) UpcomingAppointments(ctx context.Context, limit int) ([]Appointment, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+apptCols+`
		FROM appointments a LEFT JOIN providers p ON p.id = a.provider_id
		WHERE a.status = 'upcoming'
		ORDER BY a.scheduled_at ASC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Appointment
	for rows.Next() {
		a, err := scanAppt(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) GetAppointment(ctx context.Context, id int64) (Appointment, error) {
	return scanAppt(s.db.QueryRowContext(ctx, `
		SELECT `+apptCols+`
		FROM appointments a LEFT JOIN providers p ON p.id = a.provider_id
		WHERE a.id = ?`, id))
}

func (s *Store) CreateAppointment(ctx context.Context, a Appointment) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO appointments (provider_id, kind, scheduled_at, status, prep_notes, outcome, follow_up_on)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		nullZero(a.ProviderID), a.Kind, a.ScheduledAt, statusOr(a.Status),
		nullify(a.PrepNotes), nullify(a.Outcome), nullify(a.FollowUpOn))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateAppointment saves the editable fields (prep, outcome, status, follow-up).
func (s *Store) UpdateAppointment(ctx context.Context, a Appointment) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE appointments
		SET prep_notes = ?, outcome = ?, status = ?, follow_up_on = ?
		WHERE id = ?`,
		nullify(a.PrepNotes), nullify(a.Outcome), statusOr(a.Status), nullify(a.FollowUpOn), a.ID)
	return err
}

// --- Recordings -------------------------------------------------------------

const recordingCols = `r.id, COALESCE(r.appointment_id,0), COALESCE(a.kind,''),
	COALESCE(p.name,''), COALESCE(r.audio_path,''), COALESCE(r.duration_sec,0),
	COALESCE(r.transcript,''), r.transcript_status, r.created_at`

func scanRecording(sc interface{ Scan(...any) error }) (Recording, error) {
	var r Recording
	var apptKind, providerName string
	err := sc.Scan(&r.ID, &r.AppointmentID, &apptKind, &providerName, &r.AudioPath,
		&r.DurationSec, &r.Transcript, &r.TranscriptStatus, &r.CreatedAt)
	if r.AppointmentID != 0 {
		parts := make([]string, 0, 2)
		if k := strings.ReplaceAll(apptKind, "_", " "); k != "" {
			parts = append(parts, k)
		}
		if providerName != "" {
			parts = append(parts, providerName)
		}
		r.AppointmentLabel = strings.Join(parts, " · ")
	}
	return r, err
}

const recordingJoin = `FROM recordings r
	LEFT JOIN appointments a ON a.id = r.appointment_id
	LEFT JOIN providers p ON p.id = a.provider_id`

func (s *Store) Recordings(ctx context.Context) ([]Recording, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+recordingCols+` `+recordingJoin+` ORDER BY r.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Recording
	for rows.Next() {
		rec, err := scanRecording(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (s *Store) RecordingsForAppointment(ctx context.Context, apptID int64) ([]Recording, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+recordingCols+` `+recordingJoin+`
		WHERE r.appointment_id = ? ORDER BY r.created_at DESC`, apptID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Recording
	for rows.Next() {
		rec, err := scanRecording(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (s *Store) GetRecording(ctx context.Context, id int64) (Recording, error) {
	return scanRecording(s.db.QueryRowContext(ctx, `SELECT `+recordingCols+` `+recordingJoin+` WHERE r.id = ?`, id))
}

func (s *Store) CreateRecording(ctx context.Context, r Recording) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO recordings (appointment_id, audio_path, duration_sec, transcript_status)
		VALUES (?, ?, ?, ?)`,
		nullZero(r.AppointmentID), nullify(r.AudioPath), nullZero(r.DurationSec),
		recStatusOr(r.TranscriptStatus))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// SetTranscriptStatus moves a recording through the pipeline states.
func (s *Store) SetTranscriptStatus(ctx context.Context, id int64, status string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE recordings SET transcript_status = ? WHERE id = ?`, status, id)
	return err
}

// SaveTranscript stores the transcript text and marks the recording done.
func (s *Store) SaveTranscript(ctx context.Context, id int64, transcript string, durationSec int64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE recordings
		SET transcript = ?, transcript_status = 'done', duration_sec = COALESCE(NULLIF(?,0), duration_sec)
		WHERE id = ?`, transcript, durationSec, id)
	return err
}

// DeleteAudio removes the on-disk blob and clears audio_path, keeping the
// transcript — audio is the most sensitive data, so it can be purged once
// transcribed (per the design's data-handling note).
func (s *Store) DeleteAudio(ctx context.Context, id int64) (string, error) {
	var path string
	if err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(audio_path,'') FROM recordings WHERE id = ?`, id).Scan(&path); err != nil {
		return "", err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE recordings SET audio_path = NULL WHERE id = ?`, id)
	return path, err
}

func (s *Store) DeleteRecording(ctx context.Context, id int64) (string, error) {
	var path string
	if err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(audio_path,'') FROM recordings WHERE id = ?`, id).Scan(&path); err != nil {
		return "", err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM recordings WHERE id = ?`, id)
	return path, err
}

// --- AI documents -----------------------------------------------------------

const aiDocCols = `id, doc_type, COALESCE(title,''), content, COALESCE(model,''),
	COALESCE(source_kind,''), COALESCE(source_id,0), is_edited, created_at`

func scanAIDoc(sc interface{ Scan(...any) error }) (AIDoc, error) {
	var d AIDoc
	err := sc.Scan(&d.ID, &d.DocType, &d.Title, &d.Content, &d.Model,
		&d.SourceKind, &d.SourceID, &d.IsEdited, &d.CreatedAt)
	return d, err
}

func (s *Store) AIDoc(ctx context.Context, id int64) (AIDoc, error) {
	return scanAIDoc(s.db.QueryRowContext(ctx, `SELECT `+aiDocCols+` FROM ai_documents WHERE id = ?`, id))
}

func (s *Store) AIDocsForSource(ctx context.Context, kind string, id int64) ([]AIDoc, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+aiDocCols+` FROM ai_documents
		WHERE source_kind = ? AND source_id = ? ORDER BY created_at DESC`, kind, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AIDoc
	for rows.Next() {
		d, err := scanAIDoc(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// UpdateAIDocContent saves a user edit and flips is_edited so provenance is honest.
func (s *Store) UpdateAIDocContent(ctx context.Context, id int64, content string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE ai_documents SET content = ?, is_edited = 1 WHERE id = ?`, content, id)
	return err
}

// --- ai.Repo implementation -------------------------------------------------
// These satisfy ai.Repo. The read methods have no error in their signatures, so
// they log and degrade to zero values — a missing case-file row should weaken the
// prompt, not crash a generation.

var _ ai.Repo = (*Store)(nil)

func (s *Store) LatestDiagnosis(ctx context.Context) string {
	var dx string
	err := s.db.QueryRowContext(ctx,
		`SELECT working_diagnosis FROM case_profile WHERE id = 1`).Scan(&dx)
	if err != nil {
		log.Printf("store: LatestDiagnosis: %v", err)
		return ""
	}
	if dx == "" {
		return "No working diagnosis recorded yet."
	}
	return dx
}

func (s *Store) RecentOutcomes(ctx context.Context, limit int) []ai.Outcome {
	rows, err := s.db.QueryContext(ctx, `
		SELECT a.scheduled_at, COALESCE(p.name,'Unknown provider'), a.outcome
		FROM appointments a LEFT JOIN providers p ON p.id = a.provider_id
		WHERE a.outcome IS NOT NULL AND a.outcome <> ''
		ORDER BY a.scheduled_at DESC LIMIT ?`, limit)
	if err != nil {
		log.Printf("store: RecentOutcomes: %v", err)
		return nil
	}
	defer rows.Close()
	var out []ai.Outcome
	for rows.Next() {
		var o ai.Outcome
		if err := rows.Scan(&o.Date, &o.Provider, &o.Outcome); err != nil {
			log.Printf("store: RecentOutcomes scan: %v", err)
			return out
		}
		out = append(out, o)
	}
	return out
}

func (s *Store) ResourceSummaries(ctx context.Context) []ai.ResourceSummary {
	rows, err := s.db.QueryContext(ctx, `
		SELECT title, summary FROM resources
		WHERE summary IS NOT NULL AND summary <> '' ORDER BY created_at DESC`)
	if err != nil {
		log.Printf("store: ResourceSummaries: %v", err)
		return nil
	}
	defer rows.Close()
	var out []ai.ResourceSummary
	for rows.Next() {
		var r ai.ResourceSummary
		if err := rows.Scan(&r.Title, &r.Summary); err != nil {
			log.Printf("store: ResourceSummaries scan: %v", err)
			return out
		}
		out = append(out, r)
	}
	return out
}

func (s *Store) Appointment(ctx context.Context, id int64) ai.Appointment {
	var a ai.Appointment
	err := s.db.QueryRowContext(ctx, `
		SELECT a.id, a.kind, COALESCE(p.name,'your provider'), a.scheduled_at
		FROM appointments a LEFT JOIN providers p ON p.id = a.provider_id
		WHERE a.id = ?`, id).Scan(&a.ID, &a.Kind, &a.Provider, &a.ScheduledAt)
	if err != nil {
		log.Printf("store: Appointment(%d): %v", id, err)
	}
	return a
}

func (s *Store) Recording(ctx context.Context, id int64) ai.Recording {
	var r ai.Recording
	err := s.db.QueryRowContext(ctx,
		`SELECT id, COALESCE(transcript,'') FROM recordings WHERE id = ?`, id).
		Scan(&r.ID, &r.Transcript)
	if err != nil {
		log.Printf("store: Recording(%d): %v", id, err)
	}
	return r
}

func (s *Store) SaveAIDoc(ctx context.Context, d ai.AIDoc) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO ai_documents (doc_type, title, content, model, source_kind, source_id)
		VALUES (?, ?, ?, ?, ?, ?)`,
		d.DocType, nullify(d.Title), d.Content, nullify(d.Model),
		nullify(d.SourceKind), nullZero(d.SourceID))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) SaveResourceSummary(ctx context.Context, resourceID int64, summary string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE resources SET summary = ? WHERE id = ?`, summary, resourceID)
	return err
}

// --- helpers ----------------------------------------------------------------

// nullify maps "" to a NULL so empty optional fields don't store as ” inconsistently.
func nullify(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullZero(n int64) any {
	if n == 0 {
		return nil
	}
	return n
}

func statusOr(s string) string {
	if s == "" {
		return "upcoming"
	}
	return s
}

func recStatusOr(s string) string {
	if s == "" {
		return "pending"
	}
	return s
}

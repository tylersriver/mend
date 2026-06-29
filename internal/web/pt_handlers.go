package web

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/tylersriver/mend/internal/store"
	"github.com/tylersriver/mend/internal/web/view"
)

// --- PT hub -----------------------------------------------------------------

func (s *Server) ptHub(w http.ResponseWriter, r *http.Request) {
	sessions, err := s.store.ListSessions(r.Context())
	if err != nil {
		s.fail(w, "list sessions", err)
		return
	}
	if len(sessions) > 5 {
		sessions = sessions[:5]
	}
	exercises, err := s.store.ListExercises(r.Context(), false)
	if err != nil {
		s.fail(w, "list exercises", err)
		return
	}
	s.render(w, r, view.PTHub(s.profile(r.Context()), sessions, len(exercises)))
}

// --- Exercises --------------------------------------------------------------

func (s *Server) exerciseList(w http.ResponseWriter, r *http.Request) {
	exs, err := s.store.ListExercises(r.Context(), true) // include archived (shown greyed)
	if err != nil {
		s.fail(w, "list exercises", err)
		return
	}
	s.render(w, r, view.ExerciseList(s.profile(r.Context()), exs))
}

func (s *Server) exerciseNew(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, view.ExerciseForm(s.profile(r.Context())))
}

func (s *Server) exerciseCreate(w http.ResponseWriter, r *http.Request) {
	e := store.Exercise{
		Name:       strings.TrimSpace(r.FormValue("name")),
		MetricType: r.FormValue("metric_type"),
		Category:   r.FormValue("category"),
		Cues:       strings.TrimSpace(r.FormValue("cues")),
		DemoURL:    strings.TrimSpace(r.FormValue("demo_url")),
	}
	if e.Name == "" || e.MetricType == "" {
		http.Error(w, "name and metric type are required", http.StatusBadRequest)
		return
	}
	if _, err := s.store.CreateExercise(r.Context(), e); err != nil {
		s.fail(w, "create exercise", err)
		return
	}
	http.Redirect(w, r, "/pt/exercises", http.StatusSeeOther)
}

func (s *Server) exerciseArchive(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	archived := r.FormValue("archived") == "1"
	if err := s.store.SetExerciseArchived(r.Context(), id, archived); err != nil {
		s.fail(w, "archive exercise", err)
		return
	}
	http.Redirect(w, r, "/pt/exercises", http.StatusSeeOther)
}

// --- Sessions ---------------------------------------------------------------

func (s *Server) sessionList(w http.ResponseWriter, r *http.Request) {
	sessions, err := s.store.ListSessions(r.Context())
	if err != nil {
		s.fail(w, "list sessions", err)
		return
	}
	s.render(w, r, view.SessionList(s.profile(r.Context()), sessions))
}

func (s *Server) sessionNew(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, view.SessionForm(s.profile(r.Context())))
}

func (s *Server) sessionCreate(w http.ResponseWriter, r *http.Request) {
	se := store.Session{
		PerformedAt: strings.TrimSpace(r.FormValue("performed_at")),
		DurationMin: parseIntPtr(r.FormValue("duration_min")),
		PainPre:     parseIntPtr(r.FormValue("pain_pre")),
		PainPost:    parseIntPtr(r.FormValue("pain_post")),
		Notes:       strings.TrimSpace(r.FormValue("notes")),
	}
	id, err := s.store.CreateSession(r.Context(), se)
	if err != nil {
		s.fail(w, "create session", err)
		return
	}
	http.Redirect(w, r, "/pt/sessions/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (s *Server) sessionDetail(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	se, err := s.store.Session(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	exs, err := s.store.ListExercises(r.Context(), false)
	if err != nil {
		s.fail(w, "list exercises", err)
		return
	}
	s.render(w, r, view.SessionDetail(s.profile(r.Context()), se, exs))
}

func (s *Server) sessionUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	se := store.Session{
		ID:          id,
		DurationMin: parseIntPtr(r.FormValue("duration_min")),
		PainPre:     parseIntPtr(r.FormValue("pain_pre")),
		PainPost:    parseIntPtr(r.FormValue("pain_post")),
		Notes:       strings.TrimSpace(r.FormValue("notes")),
	}
	if err := s.store.UpdateSession(r.Context(), se); err != nil {
		s.fail(w, "update session", err)
		return
	}
	http.Redirect(w, r, "/pt/sessions/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (s *Server) sessionDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if err := s.store.DeleteSession(r.Context(), id); err != nil {
		s.fail(w, "delete session", err)
		return
	}
	http.Redirect(w, r, "/pt/sessions", http.StatusSeeOther)
}

// addSet logs a set and returns the new row as an htmx fragment.
func (s *Server) addSet(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	exerciseID, _ := strconv.ParseInt(r.FormValue("exercise_id"), 10, 64)
	if exerciseID == 0 {
		http.Error(w, "exercise is required", http.StatusBadRequest)
		return
	}
	st := store.Set{
		SessionID:   id,
		ExerciseID:  exerciseID,
		Reps:        parseIntPtr(r.FormValue("reps")),
		Load:        parseFloatPtr(r.FormValue("load")),
		DurationSec: parseIntPtr(r.FormValue("duration_sec")),
		ROMDegrees:  parseFloatPtr(r.FormValue("rom_degrees")),
		Resistance:  strings.TrimSpace(r.FormValue("resistance")),
		RPE:         parseFloatPtr(r.FormValue("rpe")),
		Pain:        parseIntPtr(r.FormValue("pain")),
		Notes:       strings.TrimSpace(r.FormValue("notes")),
	}
	setID, err := s.store.AddSet(r.Context(), st)
	if err != nil {
		s.fail(w, "add set", err)
		return
	}
	saved, err := s.store.Set(r.Context(), setID)
	if err != nil {
		s.fail(w, "load set", err)
		return
	}
	s.render(w, r, view.SetRow(saved))
}

// deleteSet removes a set; htmx swaps the row out, so an empty 200 suffices.
func (s *Server) deleteSet(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if _, err := s.store.DeleteSet(r.Context(), id); err != nil {
		s.fail(w, "delete set", err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// --- Measurements -----------------------------------------------------------

func (s *Server) measurementList(w http.ResponseWriter, r *http.Request) {
	series, order, err := s.store.MeasurementSeries(r.Context())
	if err != nil {
		s.fail(w, "measurement series", err)
		return
	}
	recent, err := s.store.ListMeasurements(r.Context())
	if err != nil {
		s.fail(w, "list measurements", err)
		return
	}
	s.render(w, r, view.MeasurementList(s.profile(r.Context()), series, order, recent))
}

func (s *Server) measurementCreate(w http.ResponseWriter, r *http.Request) {
	value, err := strconv.ParseFloat(strings.TrimSpace(r.FormValue("value")), 64)
	if err != nil {
		http.Error(w, "value must be a number", http.StatusBadRequest)
		return
	}
	m := store.Measurement{
		MeasuredAt: strings.TrimSpace(r.FormValue("measured_at")),
		Metric:     strings.TrimSpace(r.FormValue("metric")),
		Value:      value,
		Unit:       strings.TrimSpace(r.FormValue("unit")),
		Side:       r.FormValue("side"),
		Notes:      strings.TrimSpace(r.FormValue("notes")),
	}
	if m.Metric == "" {
		http.Error(w, "metric is required", http.StatusBadRequest)
		return
	}
	if _, err := s.store.CreateMeasurement(r.Context(), m); err != nil {
		s.fail(w, "create measurement", err)
		return
	}
	http.Redirect(w, r, "/pt/measurements", http.StatusSeeOther)
}

func (s *Server) measurementDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if err := s.store.DeleteMeasurement(r.Context(), id); err != nil {
		s.fail(w, "delete measurement", err)
		return
	}
	http.Redirect(w, r, "/pt/measurements", http.StatusSeeOther)
}

// --- form parsing helpers ---------------------------------------------------

// parseIntPtr returns nil for blank/invalid input so the column stays NULL.
func parseIntPtr(s string) *int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return nil
	}
	return &v
}

func parseFloatPtr(s string) *float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil
	}
	return &v
}

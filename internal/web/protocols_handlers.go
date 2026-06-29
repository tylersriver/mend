package web

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/tylersriver/mend/internal/store"
	"github.com/tylersriver/mend/internal/web/view"
)

func (s *Server) protocolList(w http.ResponseWriter, r *http.Request) {
	ps, err := s.store.ListProtocols(r.Context())
	if err != nil {
		s.fail(w, "list protocols", err)
		return
	}
	s.render(w, r, view.ProtocolList(s.profile(r.Context()), ps))
}

func (s *Server) protocolNew(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, view.ProtocolForm(s.profile(r.Context())))
}

func (s *Server) protocolCreate(w http.ResponseWriter, r *http.Request) {
	p := store.Protocol{
		Name:      strings.TrimSpace(r.FormValue("name")),
		StartedOn: strings.TrimSpace(r.FormValue("started_on")),
		Notes:     strings.TrimSpace(r.FormValue("notes")),
	}
	if p.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	id, err := s.store.CreateProtocol(r.Context(), p)
	if err != nil {
		s.fail(w, "create protocol", err)
		return
	}
	http.Redirect(w, r, "/pt/protocols/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (s *Server) protocolDetail(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	p, err := s.store.Protocol(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	pes, err := s.store.ProtocolExercises(r.Context(), id)
	if err != nil {
		s.fail(w, "load prescription", err)
		return
	}
	rows := make([]view.ComplianceRow, 0, len(pes))
	for _, pe := range pes {
		last, logged, err := s.store.LastSetForExercise(r.Context(), id, pe.ExerciseID)
		if err != nil {
			s.fail(w, "load compliance", err)
			return
		}
		rows = append(rows, view.ComplianceRow{PE: pe, Last: last, Logged: logged})
	}
	exs, err := s.store.ListExercises(r.Context(), false)
	if err != nil {
		s.fail(w, "list exercises", err)
		return
	}
	s.render(w, r, view.ProtocolDetail(s.profile(r.Context()), p, rows, exs))
}

func (s *Server) protocolEnd(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if err := s.store.EndProtocol(r.Context(), id); err != nil {
		s.fail(w, "end protocol", err)
		return
	}
	http.Redirect(w, r, "/pt/protocols/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (s *Server) protocolDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if err := s.store.DeleteProtocol(r.Context(), id); err != nil {
		s.fail(w, "delete protocol", err)
		return
	}
	http.Redirect(w, r, "/pt/protocols", http.StatusSeeOther)
}

func (s *Server) addPrescription(w http.ResponseWriter, r *http.Request) {
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
	sortOrder, _ := strconv.ParseInt(r.FormValue("sort_order"), 10, 64)
	pe := store.ProtocolExercise{
		ProtocolID:        id,
		ExerciseID:        exerciseID,
		TargetSets:        parseIntPtr(r.FormValue("target_sets")),
		TargetReps:        parseIntPtr(r.FormValue("target_reps")),
		TargetLoad:        parseFloatPtr(r.FormValue("target_load")),
		TargetDurationSec: parseIntPtr(r.FormValue("target_duration_sec")),
		TargetRPE:         parseFloatPtr(r.FormValue("target_rpe")),
		Frequency:         strings.TrimSpace(r.FormValue("frequency")),
		Progression:       strings.TrimSpace(r.FormValue("progression")),
		SortOrder:         sortOrder,
	}
	if _, err := s.store.AddProtocolExercise(r.Context(), pe); err != nil {
		s.fail(w, "add prescription", err)
		return
	}
	http.Redirect(w, r, "/pt/protocols/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (s *Server) deletePrescription(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	protocolID, err := s.store.DeleteProtocolExercise(r.Context(), id)
	if err != nil {
		s.fail(w, "delete prescription", err)
		return
	}
	http.Redirect(w, r, "/pt/protocols/"+strconv.FormatInt(protocolID, 10), http.StatusSeeOther)
}

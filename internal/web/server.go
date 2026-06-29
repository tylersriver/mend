// Package web wires the HTTP routes for the core loop: case profile, research
// resources, providers, appointments, and the AI care-prep flows. Routing uses
// the stdlib net/http 1.22 method+path mux — no third-party router, per the
// design log.
package web

import (
	"context"
	"log"
	"net/http"
	"strconv"

	"github.com/a-h/templ"

	"github.com/tylersriver/mend/internal/ai"
	"github.com/tylersriver/mend/internal/recordings"
	"github.com/tylersriver/mend/internal/store"
)

type Server struct {
	store   *store.Store
	ai      *ai.Service           // nil when no API key is configured
	proc    *recordings.Processor // transcription/summary pipeline
	dataDir string
}

func NewServer(st *store.Store, aiSvc *ai.Service, proc *recordings.Processor, dataDir string) *Server {
	return &Server{store: st, ai: aiSvc, proc: proc, dataDir: dataDir}
}

func (s *Server) aiEnabled() bool         { return s.ai != nil }
func (s *Server) transcribeEnabled() bool { return s.proc != nil && s.proc.TranscriptionEnabled() }

// Routes returns the configured mux.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /", s.dashboard)
	mux.HandleFunc("POST /profile", s.updateProfile)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })

	// PWA shell assets (manifest, service worker, icons, offline page).
	s.staticAssets(mux)

	// Resources (research library)
	mux.HandleFunc("GET /resources", s.resourceList)
	mux.HandleFunc("GET /resources/new", s.resourceNew)
	mux.HandleFunc("POST /resources", s.resourceCreate)
	mux.HandleFunc("GET /resources/{id}", s.resourceDetail)
	mux.HandleFunc("GET /resources/{id}/file", s.resourceFile)
	mux.HandleFunc("POST /resources/{id}/delete", s.resourceDelete)
	mux.HandleFunc("POST /resources/{id}/summarize", s.resourceSummarize)

	// Recordings (audio → transcript → AI highlights/tasks)
	mux.HandleFunc("GET /recordings", s.recordingList)
	mux.HandleFunc("GET /recordings/new", s.recordingNew)
	mux.HandleFunc("POST /recordings", s.recordingCreate)
	mux.HandleFunc("GET /recordings/{id}", s.recordingDetail)
	mux.HandleFunc("GET /recordings/{id}/status", s.recordingStatus)
	mux.HandleFunc("GET /recordings/{id}/audio", s.recordingAudio)
	mux.HandleFunc("POST /recordings/{id}/transcribe", s.recordingTranscribe)
	mux.HandleFunc("POST /recordings/{id}/summarize", s.recordingSummarize)
	mux.HandleFunc("POST /recordings/{id}/audio/delete", s.recordingAudioDelete)
	mux.HandleFunc("POST /recordings/{id}/delete", s.recordingDelete)

	// Providers
	mux.HandleFunc("GET /providers", s.providerList)
	mux.HandleFunc("GET /providers/new", s.providerNew)
	mux.HandleFunc("POST /providers", s.providerCreate)

	// Appointments
	mux.HandleFunc("GET /appointments", s.appointmentList)
	mux.HandleFunc("GET /appointments/new", s.appointmentNew)
	mux.HandleFunc("POST /appointments", s.appointmentCreate)
	mux.HandleFunc("GET /appointments/{id}", s.appointmentDetail)
	mux.HandleFunc("POST /appointments/{id}", s.appointmentUpdate)
	mux.HandleFunc("POST /appointments/{id}/questions", s.appointmentQuestions)

	// AI documents
	mux.HandleFunc("GET /ai/docs/{id}", s.aiDocEdit)
	mux.HandleFunc("POST /ai/docs/{id}", s.aiDocUpdate)

	// PT logging satellite
	mux.HandleFunc("GET /pt", s.ptHub)
	mux.HandleFunc("GET /pt/exercises", s.exerciseList)
	mux.HandleFunc("GET /pt/exercises/new", s.exerciseNew)
	mux.HandleFunc("POST /pt/exercises", s.exerciseCreate)
	mux.HandleFunc("POST /pt/exercises/{id}/archive", s.exerciseArchive)
	mux.HandleFunc("GET /pt/sessions", s.sessionList)
	mux.HandleFunc("GET /pt/sessions/new", s.sessionNew)
	mux.HandleFunc("POST /pt/sessions", s.sessionCreate)
	mux.HandleFunc("GET /pt/sessions/{id}", s.sessionDetail)
	mux.HandleFunc("POST /pt/sessions/{id}", s.sessionUpdate)
	mux.HandleFunc("POST /pt/sessions/{id}/sets", s.addSet)
	mux.HandleFunc("POST /pt/sessions/{id}/delete", s.sessionDelete)
	mux.HandleFunc("POST /pt/sets/{id}/delete", s.deleteSet)
	mux.HandleFunc("GET /pt/measurements", s.measurementList)
	mux.HandleFunc("POST /pt/measurements", s.measurementCreate)
	mux.HandleFunc("POST /pt/measurements/{id}/delete", s.measurementDelete)

	return logRequests(mux)
}

// --- shared helpers ---------------------------------------------------------

func (s *Server) render(w http.ResponseWriter, r *http.Request, c templ.Component) {
	if err := c.Render(r.Context(), w); err != nil {
		log.Printf("web: render: %v", err)
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}

// profile fetches the case profile for the layout header; failures degrade to an
// empty profile rather than failing the whole page.
func (s *Server) profile(ctx context.Context) store.CaseProfile {
	p, err := s.store.Profile(ctx)
	if err != nil {
		log.Printf("web: profile: %v", err)
	}
	return p
}

func pathID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}

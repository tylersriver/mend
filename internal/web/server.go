// Package web wires the HTTP routes for the core loop: case profile, research
// resources, providers, appointments, and the AI care-prep flows. Routing uses
// the stdlib net/http 1.22 method+path mux — no third-party router, per the
// design log.
package web

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"runtime/debug"
	"strconv"
	"sync"
	"time"

	"github.com/a-h/templ"

	"github.com/tylersriver/mend/internal/ai"
	"github.com/tylersriver/mend/internal/config"
	"github.com/tylersriver/mend/internal/recordings"
	"github.com/tylersriver/mend/internal/store"
	"github.com/tylersriver/mend/internal/transcribe"
)

type Server struct {
	store   *store.Store
	env     *config.Config
	dataDir string

	// ai and proc are rebuilt from effective config whenever settings change, so
	// they're guarded for concurrent reads (request handlers) vs. a settings save.
	mu   sync.RWMutex
	ai   *ai.Service           // nil when no API key is configured
	proc *recordings.Processor // transcription/summary pipeline
}

func NewServer(st *store.Store, env *config.Config) *Server {
	s := &Server{store: st, env: env, dataDir: env.DataDir}
	if err := s.reconfigure(context.Background()); err != nil {
		log.Printf("web: initial AI config: %v", err)
	}
	return s
}

func (s *Server) aiSvc() *ai.Service {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ai
}

func (s *Server) processor() *recordings.Processor {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.proc
}

func (s *Server) aiEnabled() bool { return s.aiSvc() != nil }

func (s *Server) transcribeEnabled() bool {
	p := s.processor()
	return p != nil && p.TranscriptionEnabled()
}

// effectiveAI is the resolved config used to build the clients: a DB setting wins
// over the env default, which wins over nothing.
type effectiveAI struct {
	anthropicKey      string
	aiModel           string
	transcribeKey     string
	transcribeBaseURL string
	transcribeModel   string
}

func (s *Server) effective(set store.Settings) effectiveAI {
	return effectiveAI{
		anthropicKey:      orStr(set.AnthropicAPIKey, s.env.APIKey),
		aiModel:           orStr(set.AIModel, s.env.AIModel),
		transcribeKey:     orStr(set.TranscribeAPIKey, s.env.TranscribeAPIKey),
		transcribeBaseURL: orStr(set.TranscribeBaseURL, s.env.TranscribeBaseURL),
		transcribeModel:   orStr(set.TranscribeModel, s.env.TranscribeModel),
	}
}

// reconfigure rebuilds the AI service and transcription pipeline from the current
// effective config. Called at startup and after any settings change.
func (s *Server) reconfigure(ctx context.Context) error {
	set, err := s.store.Settings(ctx)
	if err != nil {
		return err
	}
	eff := s.effective(set)

	var aiSvc *ai.Service
	if eff.anthropicKey != "" {
		aiSvc = ai.NewService(s.store, ai.New(eff.anthropicKey, eff.aiModel))
	}
	var tr transcribe.Transcriber
	if eff.transcribeKey != "" {
		tr = transcribe.New(eff.transcribeKey, eff.transcribeBaseURL, eff.transcribeModel)
	}

	s.mu.Lock()
	s.ai = aiSvc
	s.proc = recordings.NewProcessor(s.store, tr, aiSvc)
	s.mu.Unlock()

	log.Printf("config: ai=%t transcription=%t (ai_model=%s, transcribe_base=%s, transcribe_model=%s)",
		aiSvc != nil, tr != nil, eff.aiModel, eff.transcribeBaseURL, eff.transcribeModel)

	// If transcription is (now) enabled, pick up any recordings stuck in 'pending'
	// — e.g. uploaded before the key was configured. The claim in Process keeps
	// this from double-running anything already in flight.
	if tr != nil {
		go s.requeuePendingTranscriptions()
	}
	return nil
}

// requeuePendingTranscriptions kicks the pipeline for every recording that still
// has audio but no transcript.
func (s *Server) requeuePendingTranscriptions() {
	proc := s.processor()
	if proc == nil || !proc.TranscriptionEnabled() {
		return
	}
	ids, err := s.store.PendingTranscriptionIDs(context.Background())
	if err != nil {
		log.Printf("web: list pending transcriptions: %v", err)
		return
	}
	if len(ids) > 0 {
		log.Printf("recordings: re-queuing %d pending transcription(s)", len(ids))
	}
	for _, id := range ids {
		proc.Process(id)
	}
}

func orStr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

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

	// Auth + settings
	mux.HandleFunc("GET /login", s.loginPage)
	mux.HandleFunc("POST /login", s.login)
	mux.HandleFunc("POST /logout", s.logout)
	mux.HandleFunc("GET /settings", s.settingsPage)
	mux.HandleFunc("POST /settings", s.updateSettings)

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
	mux.HandleFunc("GET /pt/protocols", s.protocolList)
	mux.HandleFunc("GET /pt/protocols/new", s.protocolNew)
	mux.HandleFunc("POST /pt/protocols", s.protocolCreate)
	mux.HandleFunc("GET /pt/protocols/{id}", s.protocolDetail)
	mux.HandleFunc("POST /pt/protocols/{id}/end", s.protocolEnd)
	mux.HandleFunc("POST /pt/protocols/{id}/delete", s.protocolDelete)
	mux.HandleFunc("POST /pt/protocols/{id}/exercises", s.addPrescription)
	mux.HandleFunc("POST /pt/protocol-exercises/{id}/delete", s.deletePrescription)

	return logRequests(s.requireAuth(mux))
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

// statusWriter captures the response status and byte count for access logging.
type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}

// Flush passes through so htmx/streaming responses keep working when wrapped.
func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// logRequests logs each request's method, path, status, size, and duration, and
// recovers panics (logging a stack) so a crash in one handler can't take the
// process down silently. The completion line is the diagnostic signal: uploads
// log their start in saveAudio, so a "recordings: upload ..." with no matching
// completion line means the request died mid-flight (e.g. an OOM kill).
func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}

		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("PANIC %s %s: %v\n%s", r.Method, r.URL.Path, rec, debug.Stack())
				if sw.status == 0 {
					http.Error(sw, "internal error", http.StatusInternalServerError)
				}
			}
			extra := ""
			if r.ContentLength > 0 {
				extra = fmt.Sprintf(" in=%dB", r.ContentLength)
			}
			log.Printf("%s %s -> %d %dB%s %s",
				r.Method, r.URL.Path, sw.status, sw.bytes, extra, time.Since(start).Round(time.Millisecond))
		}()

		next.ServeHTTP(sw, r)
	})
}

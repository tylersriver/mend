package web

import (
	"context"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/tylersriver/mend/internal/research"
	"github.com/tylersriver/mend/internal/store"
	"github.com/tylersriver/mend/internal/web/view"
)

// --- Dashboard & profile ----------------------------------------------------

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" { // GET / is a catch-all in the 1.22 mux
		http.NotFound(w, r)
		return
	}
	ctx := r.Context()
	upcoming, err := s.store.UpcomingAppointments(ctx, 5)
	if err != nil {
		s.fail(w, "load appointments", err)
		return
	}
	recent, err := s.store.RecentResources(ctx, 5)
	if err != nil {
		s.fail(w, "load resources", err)
		return
	}
	s.render(w, r, view.Dashboard(s.profile(ctx), upcoming, recent, s.aiEnabled()))
}

func (s *Server) updateProfile(w http.ResponseWriter, r *http.Request) {
	p := store.CaseProfile{
		InjuryName:       strings.TrimSpace(r.FormValue("injury_name")),
		WorkingDiagnosis: strings.TrimSpace(r.FormValue("working_diagnosis")),
		OnsetOn:          strings.TrimSpace(r.FormValue("onset_on")),
		Notes:            strings.TrimSpace(r.FormValue("notes")),
	}
	if err := s.store.UpdateProfile(r.Context(), p); err != nil {
		s.fail(w, "save profile", err)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// --- Resources --------------------------------------------------------------

func (s *Server) resourceList(w http.ResponseWriter, r *http.Request) {
	rs, err := s.store.Resources(r.Context())
	if err != nil {
		s.fail(w, "list resources", err)
		return
	}
	s.render(w, r, view.ResourceList(s.profile(r.Context()), rs))
}

func (s *Server) resourceNew(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, view.ResourceForm(s.profile(r.Context())))
}

func (s *Server) resourceCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(8 << 20); err != nil && err != http.ErrNotMultipart {
		s.fail(w, "parse form", err)
		return
	}
	res := store.Resource{
		Kind:    r.FormValue("kind"),
		Title:   strings.TrimSpace(r.FormValue("title")),
		URL:     strings.TrimSpace(r.FormValue("url")),
		Source:  strings.TrimSpace(r.FormValue("source")),
		Summary: strings.TrimSpace(r.FormValue("summary")),
	}
	if res.Title == "" {
		http.Error(w, "title is required", http.StatusBadRequest)
		return
	}
	if res.Kind == "file" {
		if err := s.saveUpload(r, &res); err != nil {
			s.fail(w, "save upload", err)
			return
		}
	}
	id, err := s.store.CreateResource(r.Context(), res)
	if err != nil {
		s.fail(w, "create resource", err)
		return
	}
	http.Redirect(w, r, "/resources/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

// saveUpload stores an uploaded blob on disk and fills file metadata on res.
func (s *Server) saveUpload(r *http.Request, res *store.Resource) error {
	file, hdr, err := r.FormFile("file")
	if err == http.ErrMissingFile {
		return nil // file kind but no file attached — allow, file_path stays empty
	}
	if err != nil {
		return err
	}
	defer file.Close()

	dir := filepath.Join(s.dataDir, "uploads")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	name := strconv.FormatInt(time.Now().UnixNano(), 10) + "_" + filepath.Base(hdr.Filename)
	dst := filepath.Join(dir, name)
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	n, err := io.Copy(out, file)
	if err != nil {
		return err
	}
	res.FilePath = dst
	res.MimeType = hdr.Header.Get("Content-Type")
	res.SizeBytes = n
	return nil
}

func (s *Server) resourceDetail(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	res, err := s.store.Resource(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	s.render(w, r, view.ResourceDetail(s.profile(r.Context()), res, s.aiEnabled()))
}

func (s *Server) resourceFile(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	res, err := s.store.Resource(r.Context(), id)
	if err != nil || res.FilePath == "" {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, res.FilePath)
}

// resourceUpdate saves edits to a resource's title, source, URL, and notes/summary.
func (s *Server) resourceUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	res, err := s.store.Resource(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if t := strings.TrimSpace(r.FormValue("title")); t != "" {
		res.Title = t
	}
	res.URL = strings.TrimSpace(r.FormValue("url"))
	res.Source = strings.TrimSpace(r.FormValue("source"))
	res.Summary = strings.TrimSpace(r.FormValue("summary"))
	if err := s.store.UpdateResource(r.Context(), res); err != nil {
		s.fail(w, "update resource", err)
		return
	}
	http.Redirect(w, r, resourceURLpath(id), http.StatusSeeOther)
}

func (s *Server) resourceDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if err := s.store.DeleteResource(r.Context(), id); err != nil {
		s.fail(w, "delete resource", err)
		return
	}
	http.Redirect(w, r, "/resources", http.StatusSeeOther)
}

func resourceURLpath(id int64) string { return "/resources/" + strconv.FormatInt(id, 10) }

// resourceSummarize runs the AI summary flow and swaps in the updated summary.
func (s *Server) resourceSummarize(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if !s.aiEnabled() {
		s.render(w, r, view.SummaryBody("_AI is disabled — set ANTHROPIC_API_KEY._"))
		return
	}
	res, err := s.store.Resource(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	body, note := s.summarizeSource(r.Context(), res)
	if note != "" {
		s.render(w, r, view.SummaryBody(note))
		return
	}
	if err := s.aiSvc().SummarizeResource(r.Context(), id, body); err != nil {
		log.Printf("web: summarize resource %d: %v", id, err)
		s.render(w, r, view.SummaryBody("_Summary failed: "+err.Error()+"_"))
		return
	}
	res, _ = s.store.Resource(r.Context(), id)
	s.render(w, r, view.SummaryUpdated(res.Summary))
}

// researchClient fetches resource URLs for AI summarization (video transcripts,
// article text). Generous timeout: a transcript is a second request.
var researchClient = &http.Client{Timeout: 25 * time.Second}

// summarizeSource picks the best text to summarize for a resource. For a link or
// video with a URL it fetches the page/transcript (a YouTube link alone gives the
// model nothing); on failure it falls back to any notes, then the title. The
// returned note (if non-empty) is a ready-to-render message to show instead of
// summarizing — used when there's genuinely nothing to work with.
func (s *Server) summarizeSource(ctx context.Context, res store.Resource) (body, note string) {
	if res.URL != "" && (res.Kind == "video" || res.Kind == "link") {
		text, err := research.SourceText(ctx, researchClient, res.URL)
		if err == nil && strings.TrimSpace(text) != "" {
			return text, ""
		}
		log.Printf("web: fetch source for resource %d (%s): %v", res.ID, res.URL, err)
		if strings.TrimSpace(res.Summary) == "" {
			kind := "page"
			if res.Kind == "video" {
				kind = "video (it may have no captions)"
			}
			return "", "_Couldn't read the " + kind + " automatically. Paste the transcript " +
				"or a description into Notes, then summarize again._"
		}
	}
	if strings.TrimSpace(res.Summary) != "" {
		return res.Summary, ""
	}
	return res.Title, ""
}

// --- Providers --------------------------------------------------------------

func (s *Server) providerList(w http.ResponseWriter, r *http.Request) {
	ps, err := s.store.Providers(r.Context())
	if err != nil {
		s.fail(w, "list providers", err)
		return
	}
	s.render(w, r, view.ProviderList(s.profile(r.Context()), ps))
}

func (s *Server) providerNew(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, view.ProviderForm(s.profile(r.Context())))
}

func (s *Server) providerCreate(w http.ResponseWriter, r *http.Request) {
	p := store.Provider{
		Name:      strings.TrimSpace(r.FormValue("name")),
		Specialty: strings.TrimSpace(r.FormValue("specialty")),
		Clinic:    strings.TrimSpace(r.FormValue("clinic")),
		Contact:   strings.TrimSpace(r.FormValue("contact")),
		Notes:     strings.TrimSpace(r.FormValue("notes")),
	}
	if p.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	if _, err := s.store.CreateProvider(r.Context(), p); err != nil {
		s.fail(w, "create provider", err)
		return
	}
	http.Redirect(w, r, "/providers", http.StatusSeeOther)
}

// --- Appointments -----------------------------------------------------------

func (s *Server) appointmentList(w http.ResponseWriter, r *http.Request) {
	as, err := s.store.Appointments(r.Context())
	if err != nil {
		s.fail(w, "list appointments", err)
		return
	}
	s.render(w, r, view.AppointmentList(s.profile(r.Context()), as))
}

func (s *Server) appointmentNew(w http.ResponseWriter, r *http.Request) {
	ps, err := s.store.Providers(r.Context())
	if err != nil {
		s.fail(w, "load providers", err)
		return
	}
	s.render(w, r, view.AppointmentForm(s.profile(r.Context()), ps))
}

func (s *Server) appointmentCreate(w http.ResponseWriter, r *http.Request) {
	providerID, _ := strconv.ParseInt(r.FormValue("provider_id"), 10, 64)
	a := store.Appointment{
		ProviderID:  providerID,
		Kind:        r.FormValue("kind"),
		ScheduledAt: strings.TrimSpace(r.FormValue("scheduled_at")),
		Location:    strings.TrimSpace(r.FormValue("location")),
		Status:      "upcoming",
	}
	if a.ScheduledAt == "" {
		http.Error(w, "scheduled time is required", http.StatusBadRequest)
		return
	}
	id, err := s.store.CreateAppointment(r.Context(), a)
	if err != nil {
		s.fail(w, "create appointment", err)
		return
	}
	http.Redirect(w, r, "/appointments/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (s *Server) appointmentDetail(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	a, err := s.store.GetAppointment(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	docs, err := s.store.AIDocsForSource(r.Context(), "appointment", id)
	if err != nil {
		s.fail(w, "load ai docs", err)
		return
	}
	recs, err := s.store.RecordingsForAppointment(r.Context(), id)
	if err != nil {
		s.fail(w, "load recordings", err)
		return
	}
	s.render(w, r, view.AppointmentDetail(s.profile(r.Context()), a, docs, recs, s.aiEnabled()))
}

func (s *Server) appointmentUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	a := store.Appointment{
		ID:         id,
		PrepNotes:  strings.TrimSpace(r.FormValue("prep_notes")),
		Outcome:    strings.TrimSpace(r.FormValue("outcome")),
		Status:     r.FormValue("status"),
		FollowUpOn: strings.TrimSpace(r.FormValue("follow_up_on")),
		Location:   strings.TrimSpace(r.FormValue("location")),
	}
	if err := s.store.UpdateAppointment(r.Context(), a); err != nil {
		s.fail(w, "update appointment", err)
		return
	}
	http.Redirect(w, r, "/appointments/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

// appointmentQuestions runs the DoctorQuestions flow and returns the new AI doc
// card as an htmx fragment (swapped into the appointment detail page).
func (s *Server) appointmentQuestions(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if !s.aiEnabled() {
		http.Error(w, "AI disabled — set ANTHROPIC_API_KEY", http.StatusServiceUnavailable)
		return
	}
	docID, err := s.aiSvc().DoctorQuestions(r.Context(), id)
	if err != nil {
		log.Printf("web: doctor questions for appt %d: %v", id, err)
		http.Error(w, "Question generation failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	doc, err := s.store.AIDoc(r.Context(), docID)
	if err != nil {
		s.fail(w, "load generated doc", err)
		return
	}
	s.render(w, r, view.AIDocCard(doc))
}

// --- AI documents -----------------------------------------------------------

func (s *Server) aiDocEdit(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	doc, err := s.store.AIDoc(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	s.render(w, r, view.AIDocEdit(s.profile(r.Context()), doc))
}

func (s *Server) aiDocUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	content := r.FormValue("content")
	if err := s.store.UpdateAIDocContent(r.Context(), id, content); err != nil {
		s.fail(w, "save ai doc", err)
		return
	}
	doc, err := s.store.AIDoc(r.Context(), id)
	if err == nil {
		if back := backFor(doc.SourceKind, doc.SourceID); back != "" {
			http.Redirect(w, r, back, http.StatusSeeOther)
			return
		}
	}
	http.Redirect(w, r, "/ai/docs/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func backFor(kind string, id int64) string {
	switch kind {
	case "appointment":
		return "/appointments/" + strconv.FormatInt(id, 10)
	case "resource":
		return "/resources/" + strconv.FormatInt(id, 10)
	default:
		return ""
	}
}

// fail logs an internal error and returns a 500. Used for unexpected store errors.
func (s *Server) fail(w http.ResponseWriter, what string, err error) {
	log.Printf("web: %s: %v", what, err)
	http.Error(w, "internal error: "+what, http.StatusInternalServerError)
}

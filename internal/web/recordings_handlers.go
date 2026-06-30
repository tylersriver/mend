package web

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/tylersriver/mend/internal/store"
	"github.com/tylersriver/mend/internal/web/view"
)

func (s *Server) recordingList(w http.ResponseWriter, r *http.Request) {
	recs, err := s.store.Recordings(r.Context())
	if err != nil {
		s.fail(w, "list recordings", err)
		return
	}
	s.render(w, r, view.RecordingList(s.profile(r.Context()), recs, s.transcribeEnabled()))
}

func (s *Server) recordingNew(w http.ResponseWriter, r *http.Request) {
	appts, err := s.store.Appointments(r.Context())
	if err != nil {
		s.fail(w, "load appointments", err)
		return
	}
	selected, _ := strconv.ParseInt(r.URL.Query().Get("appointment_id"), 10, 64)
	s.render(w, r, view.RecordingForm(s.profile(r.Context()), appts, selected, s.transcribeEnabled()))
}

func (s *Server) recordingCreate(w http.ResponseWriter, r *http.Request) {
	// Keep only a small part of the upload in memory; anything larger streams to a
	// temp file on disk. This keeps memory flat for big audio files so we don't
	// OOM on small (256–512MB) hosts — a likely cause of "upload failed".
	if err := r.ParseMultipartForm(8 << 20); err != nil && err != http.ErrNotMultipart {
		s.fail(w, "parse form", err)
		return
	}
	apptID, _ := strconv.ParseInt(r.FormValue("appointment_id"), 10, 64)
	durationSec, _ := strconv.ParseInt(r.FormValue("duration_sec"), 10, 64)
	rec := store.Recording{
		AppointmentID:    apptID,
		DurationSec:      durationSec,
		TranscriptStatus: "pending",
	}
	if err := s.saveAudio(r, &rec); err != nil {
		s.fail(w, "save audio", err)
		return
	}
	id, err := s.store.CreateRecording(r.Context(), rec)
	if err != nil {
		s.fail(w, "create recording", err)
		return
	}
	// Kick off the pipeline off the request goroutine when we have audio + a transcriber.
	if rec.AudioPath != "" && s.transcribeEnabled() {
		go s.processor().Process(id)
	}
	http.Redirect(w, r, "/recordings/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

// saveAudio stores an uploaded audio blob under <dataDir>/recordings.
func (s *Server) saveAudio(r *http.Request, rec *store.Recording) error {
	file, hdr, err := r.FormFile("audio")
	if err == http.ErrMissingFile {
		log.Printf("recordings: no audio file attached (empty upload)")
		return nil // allow a recording with no audio yet
	}
	if err != nil {
		return fmt.Errorf("read form file: %w", err)
	}
	defer file.Close()
	log.Printf("recordings: upload %q type=%q size=%dB", hdr.Filename, hdr.Header.Get("Content-Type"), hdr.Size)

	dir := filepath.Join(s.dataDir, "recordings")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	name := strconv.FormatInt(time.Now().UnixNano(), 10) + "_" + filepath.Base(hdr.Filename)
	dst := filepath.Join(dir, name)
	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("create %s: %w", dst, err)
	}
	defer out.Close()
	n, err := io.Copy(out, file)
	if err != nil {
		return fmt.Errorf("write %s (%dB written): %w", dst, n, err)
	}
	log.Printf("recordings: saved %s (%dB)", dst, n)
	rec.AudioPath = dst
	return nil
}

func (s *Server) recordingDetail(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	rec, err := s.store.GetRecording(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	s.maybeStartTranscription(rec)
	docs, err := s.store.AIDocsForSource(r.Context(), "recording", id)
	if err != nil {
		s.fail(w, "load ai docs", err)
		return
	}
	s.render(w, r, view.RecordingDetail(s.profile(r.Context()), rec, docs, s.transcribeEnabled(), s.aiEnabled()))
}

// maybeStartTranscription kicks the pipeline for a recording that's still pending
// with audio, whenever transcription is enabled. Safe to call on every view: the
// atomic claim in Process means only the first kick actually runs.
func (s *Server) maybeStartTranscription(rec store.Recording) {
	if rec.TranscriptStatus == "pending" && rec.AudioPath != "" && s.transcribeEnabled() {
		go s.processor().Process(rec.ID)
	}
}

// recordingStatus returns just the live status region (htmx polls this).
func (s *Server) recordingStatus(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	rec, err := s.store.GetRecording(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	s.maybeStartTranscription(rec)
	docs, err := s.store.AIDocsForSource(r.Context(), "recording", id)
	if err != nil {
		s.fail(w, "load ai docs", err)
		return
	}
	s.render(w, r, view.RecordingStatus(rec, docs, s.transcribeEnabled(), s.aiEnabled()))
}

func (s *Server) recordingAudio(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	rec, err := s.store.GetRecording(r.Context(), id)
	if err != nil || rec.AudioPath == "" {
		http.NotFound(w, r)
		return
	}
	// ServeFile would otherwise sniff many audio containers (m4a/mp4/webm) as
	// application/octet-stream, which Safari refuses to play. Set an explicit
	// audio type from the extension; ServeContent honors a pre-set Content-Type.
	if ct := audioContentType(rec.AudioPath); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	http.ServeFile(w, r, rec.AudioPath)
}

// audioContentType maps a saved audio file's extension to a playable MIME type.
func audioContentType(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".m4a", ".mp4", ".aac", ".m4b":
		return "audio/mp4"
	case ".webm":
		return "audio/webm"
	case ".ogg", ".oga", ".opus":
		return "audio/ogg"
	case ".wav":
		return "audio/wav"
	case ".mp3":
		return "audio/mpeg"
	case ".caf":
		return "audio/x-caf"
	default:
		return "" // let ServeFile detect
	}
}

func (s *Server) recordingTranscribe(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if !s.transcribeEnabled() {
		http.Error(w, "transcription not configured", http.StatusServiceUnavailable)
		return
	}
	if err := s.store.SetTranscriptStatus(r.Context(), id, "pending"); err != nil {
		s.fail(w, "reset status", err)
		return
	}
	go s.processor().Process(id)
	http.Redirect(w, r, "/recordings/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (s *Server) recordingSummarize(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if !s.aiEnabled() {
		http.Error(w, "AI disabled — set ANTHROPIC_API_KEY", http.StatusServiceUnavailable)
		return
	}
	// Run synchronously: the user clicked Generate and expects the docs on return.
	s.processor().Summarize(r.Context(), id)
	http.Redirect(w, r, "/recordings/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (s *Server) recordingAudioDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	path, err := s.store.DeleteAudio(r.Context(), id)
	if err != nil {
		s.fail(w, "delete audio", err)
		return
	}
	removeBlob(path)
	http.Redirect(w, r, "/recordings/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (s *Server) recordingDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	path, err := s.store.DeleteRecording(r.Context(), id)
	if err != nil {
		s.fail(w, "delete recording", err)
		return
	}
	removeBlob(path)
	http.Redirect(w, r, "/recordings", http.StatusSeeOther)
}

// removeBlob deletes an on-disk file best-effort (logs but doesn't fail the request).
func removeBlob(path string) {
	if path == "" {
		return
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		log.Printf("web: remove blob %s: %v", path, err)
	}
}

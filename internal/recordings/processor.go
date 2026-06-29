// Package recordings owns the transcription pipeline that runs after an audio
// upload: pending → processing → done|failed, then (best-effort) an AI pass that
// turns the transcript into highlights + a task list. It runs off the request
// goroutine so the upload returns immediately and the UI polls status over htmx.
package recordings

import (
	"context"
	"log"
	"time"

	"github.com/tylersriver/mend/internal/ai"
	"github.com/tylersriver/mend/internal/store"
	"github.com/tylersriver/mend/internal/transcribe"
)

type Processor struct {
	store       *store.Store
	transcriber transcribe.Transcriber // nil when transcription is unconfigured
	ai          *ai.Service            // nil when no Anthropic key is set
}

func NewProcessor(st *store.Store, t transcribe.Transcriber, aiSvc *ai.Service) *Processor {
	return &Processor{store: st, transcriber: t, ai: aiSvc}
}

func (p *Processor) TranscriptionEnabled() bool { return p.transcriber != nil }
func (p *Processor) AIEnabled() bool            { return p.ai != nil }

// Process runs the full pipeline for one recording. Intended to be launched in a
// goroutine; it uses its own timeout-bounded context, not the request's.
func (p *Processor) Process(recID int64) {
	if p.transcriber == nil {
		return // nothing to do; recording stays 'pending' and the UI explains
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	rec, err := p.store.GetRecording(ctx, recID)
	if err != nil {
		log.Printf("recordings: load %d: %v", recID, err)
		return
	}
	if rec.AudioPath == "" {
		log.Printf("recordings: %d has no audio to transcribe", recID)
		_ = p.store.SetTranscriptStatus(ctx, recID, "failed")
		return
	}

	if err := p.store.SetTranscriptStatus(ctx, recID, "processing"); err != nil {
		log.Printf("recordings: mark processing %d: %v", recID, err)
	}

	text, err := p.transcriber.Transcribe(ctx, rec.AudioPath)
	if err != nil {
		log.Printf("recordings: transcribe %d: %v", recID, err)
		_ = p.store.SetTranscriptStatus(ctx, recID, "failed")
		return
	}
	if err := p.store.SaveTranscript(ctx, recID, text, 0); err != nil {
		log.Printf("recordings: save transcript %d: %v", recID, err)
		return
	}

	// Transcript is safely persisted; the AI pass is best-effort and must not
	// flip the recording back to failed if it errors.
	p.Summarize(ctx, recID)
}

// Summarize runs the AI highlights+tasks pass for an already-transcribed recording.
// Safe to call again to regenerate. No-op when AI is disabled.
func (p *Processor) Summarize(ctx context.Context, recID int64) {
	if p.ai == nil {
		return
	}
	if _, _, err := p.ai.SummarizeTranscript(ctx, recID); err != nil {
		log.Printf("recordings: summarize %d: %v", recID, err)
	}
}

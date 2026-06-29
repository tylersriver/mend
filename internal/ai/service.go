package ai

import (
	"context"
	"fmt"
	"strings"
)

// role frames the AI as a care-prep assistant, not a diagnostician.
const role = `You are a medical-research and care-prep assistant for a single
patient tracking one injury. You help them become an informed, organized
patient: drafting questions for their clinicians, organizing what they've
gathered, and summarizing what was discussed. You do NOT diagnose, you do NOT
recommend specific treatments as if you were their physician, and you always
frame medical decisions as belonging to their care team. Output clean Markdown.`

// Repo is the persistence surface the flows need. Implement it over SQLite.
// (This package intentionally knows nothing about the DB driver.)
type Repo interface {
	LatestDiagnosis(ctx context.Context) string
	RecentOutcomes(ctx context.Context, limit int) []Outcome
	ResourceSummaries(ctx context.Context) []ResourceSummary
	Appointment(ctx context.Context, id int64) Appointment
	Recording(ctx context.Context, id int64) Recording
	SaveAIDoc(ctx context.Context, d AIDoc) (int64, error)
	SaveResourceSummary(ctx context.Context, resourceID int64, summary string) error
}

type Outcome struct {
	Date     string
	Provider string
	Outcome  string
}

type ResourceSummary struct {
	Title   string
	Summary string
}

type Appointment struct {
	ID          int64
	Kind        string
	Provider    string
	ScheduledAt string
}

type Recording struct {
	ID         int64
	Transcript string
}

type AIDoc struct {
	DocType    string
	Title      string
	Content    string
	Model      string
	SourceKind string
	SourceID   int64
}

// Service wires a Repo to an AI client. Use two clients if you want a strong
// model for reasoning and a cheaper one for summarization.
type Service struct {
	repo Repo
	ai   *Client
}

func NewService(repo Repo, client *Client) *Service {
	return &Service{repo: repo, ai: client}
}

// caseContext gathers the durable medical picture once and marks it for caching,
// since it's reused across flows. It uses stored summaries, not full files.
func (s *Service) caseContext(ctx context.Context) SystemBlock {
	var sb strings.Builder
	sb.WriteString("# Patient case file\n\n## Working diagnosis\n")
	sb.WriteString(s.repo.LatestDiagnosis(ctx) + "\n\n## Recent appointment outcomes\n")
	for _, o := range s.repo.RecentOutcomes(ctx, 10) {
		fmt.Fprintf(&sb, "- %s (%s): %s\n", o.Date, o.Provider, o.Outcome)
	}
	sb.WriteString("\n## Research gathered\n")
	for _, r := range s.repo.ResourceSummaries(ctx) {
		fmt.Fprintf(&sb, "- %s: %s\n", r.Title, r.Summary)
	}
	return SystemBlock{
		Type:         "text",
		Text:         sb.String(),
		CacheControl: &CacheControl{Type: "ephemeral"},
	}
}

// DoctorQuestions drafts questions for an upcoming appointment, grounded in the
// case file, and saves them as an editable artifact linked to the appointment.
func (s *Service) DoctorQuestions(ctx context.Context, apptID int64) (int64, error) {
	appt := s.repo.Appointment(ctx, apptID)
	system := []SystemBlock{{Type: "text", Text: role}, s.caseContext(ctx)}
	msg := []Message{{Role: "user", Content: fmt.Sprintf(
		"I have a %s appointment with %s on %s. Draft a focused list of "+
			"questions I should ask, grounded in my case file. Flag anything "+
			"that looks inconsistent or worth clarifying.",
		appt.Kind, appt.Provider, appt.ScheduledAt)}}

	out, err := s.ai.Complete(ctx, system, msg, 2048)
	if err != nil {
		return 0, err
	}
	return s.repo.SaveAIDoc(ctx, AIDoc{
		DocType: "doctor_questions", Content: out, Model: s.ai.Model,
		SourceKind: "appointment", SourceID: appt.ID,
	})
}

// DraftPlan produces an editable, phased plan grounded in the case file.
func (s *Service) DraftPlan(ctx context.Context) (int64, error) {
	system := []SystemBlock{{Type: "text", Text: role}, s.caseContext(ctx)}
	msg := []Message{{Role: "user", Content: "Draft a phased, conservative " +
		"recovery plan grounded in my case file. Mark what to discuss with my " +
		"care team before acting on it."}}

	out, err := s.ai.Complete(ctx, system, msg, 3072)
	if err != nil {
		return 0, err
	}
	return s.repo.SaveAIDoc(ctx, AIDoc{
		DocType: "plan", Content: out, Model: s.ai.Model, SourceKind: "manual",
	})
}

// SummarizeTranscript turns a recording transcript into highlights + a task list,
// saved as two artifacts. (The case file is not needed here.)
func (s *Service) SummarizeTranscript(ctx context.Context, recordingID int64) (highlights, tasks int64, err error) {
	rec := s.repo.Recording(ctx, recordingID)
	system := []SystemBlock{{Type: "text", Text: role}}

	hl, err := s.ai.Complete(ctx, system, []Message{{Role: "user",
		Content: "Summarize the key points of this appointment transcript as " +
			"Markdown bullet highlights:\n\n" + rec.Transcript}}, 1500)
	if err != nil {
		return 0, 0, err
	}
	highlights, err = s.repo.SaveAIDoc(ctx, AIDoc{
		DocType: "highlights", Content: hl, Model: s.ai.Model,
		SourceKind: "recording", SourceID: rec.ID,
	})
	if err != nil {
		return 0, 0, err
	}

	tl, err := s.ai.Complete(ctx, system, []Message{{Role: "user",
		Content: "Extract a concrete, checkbox-style task list from this " +
			"transcript (follow-ups, things to schedule, things to ask next " +
			"time):\n\n" + rec.Transcript}}, 1500)
	if err != nil {
		return highlights, 0, err
	}
	tasks, err = s.repo.SaveAIDoc(ctx, AIDoc{
		DocType: "tasks", Content: tl, Model: s.ai.Model,
		SourceKind: "recording", SourceID: rec.ID,
	})
	return highlights, tasks, err
}

// SummarizeResource summarizes one resource and writes the result back to
// resources.summary, so it becomes reusable context for caseContext.
func (s *Service) SummarizeResource(ctx context.Context, resourceID int64, body string) error {
	system := []SystemBlock{{Type: "text", Text: role}}
	out, err := s.ai.Complete(ctx, system, []Message{{Role: "user",
		Content: "Summarize this research item in 3-5 sentences for later " +
			"reference:\n\n" + body}}, 700)
	if err != nil {
		return err
	}
	return s.repo.SaveResourceSummary(ctx, resourceID, out)
}

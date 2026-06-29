package view

import (
	"bytes"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/tylersriver/mend/internal/store"
	"github.com/yuin/goldmark"
)

// inputCls is the shared Tailwind class set for text inputs/textareas/selects.
const inputCls = "mt-1 block w-full rounded-md border border-slate-300 px-3 py-2 text-sm focus:border-teal-500 focus:ring-teal-500"

func apptURL(id int64) string     { return "/appointments/" + strconv.FormatInt(id, 10) }
func resourceURL(id int64) string { return "/resources/" + strconv.FormatInt(id, 10) }
func aiDocURL(id int64) string    { return "/ai/docs/" + strconv.FormatInt(id, 10) }

// providerOr gives a non-empty label for an appointment's provider.
func providerOr(a store.Appointment) string {
	if a.ProviderName == "" {
		return "Unspecified provider"
	}
	return a.ProviderName
}

func itoa(id int64) string { return strconv.FormatInt(id, 10) }

// backURL points an AI doc back to whatever it was generated from.
func backURL(d store.AIDoc) string {
	switch d.SourceKind {
	case "appointment":
		return apptURL(d.SourceID)
	case "resource":
		return resourceURL(d.SourceID)
	case "recording":
		return recordingURL(d.SourceID)
	default:
		return ""
	}
}

// recordingLabel gives a recording a human title from its linked appointment.
func recordingLabel(r store.Recording) string {
	if r.AppointmentLabel != "" {
		return r.AppointmentLabel
	}
	return "Unlinked recording"
}

// apptOption renders an appointment as a one-line <option> label.
func apptOption(a store.Appointment) string {
	return label(a.Kind) + " · " + providerOr(a) + " · " + humanTime(a.ScheduledAt)
}

func sourceSuffix(r store.Resource) string {
	if r.Source == "" {
		return ""
	}
	return " · " + r.Source
}

// docTitle prefers a stored title, falling back to a humanized doc type.
func docTitle(d store.AIDoc) string {
	if d.Title != "" {
		return d.Title
	}
	switch d.DocType {
	case "doctor_questions":
		return "Questions for your provider"
	case "highlights":
		return "Appointment highlights"
	case "tasks":
		return "Follow-up tasks"
	case "plan":
		return "Draft recovery plan"
	case "summary":
		return "Summary"
	default:
		return label(d.DocType)
	}
}

// md renders trusted-but-AI-authored Markdown to HTML. goldmark escapes raw HTML
// by default, so wrapping the result in templ.Raw is safe here.
var md = goldmark.New()

// Markdown converts a Markdown string into a renderable component.
func Markdown(s string) templ.Component {
	var buf bytes.Buffer
	if err := md.Convert([]byte(s), &buf); err != nil {
		// Fall back to escaped plain text on any rendering error.
		return templ.Raw(templ.EscapeString(s))
	}
	return templ.Raw(buf.String())
}

// humanTime renders the app's stored timestamps (SQLite "YYYY-MM-DD HH:MM:SS" or a
// date-only "YYYY-MM-DD") in a friendlier form, falling back to the raw value.
func humanTime(s string) string {
	if s == "" {
		return ""
	}
	for _, layout := range []string{"2006-01-02 15:04:05", time.RFC3339, "2006-01-02T15:04", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			if t.Hour() == 0 && t.Minute() == 0 {
				return t.Format("Mon Jan 2, 2006")
			}
			return t.Format("Mon Jan 2, 2006 · 3:04 PM")
		}
	}
	return s
}

// title-cases a snake/lower token for display, e.g. "follow_up" -> "Follow Up".
func label(s string) string {
	s = strings.ReplaceAll(s, "_", " ")
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// statusClasses maps an appointment status to Tailwind badge colors.
func statusClasses(status string) string {
	switch status {
	case "completed":
		return "bg-green-100 text-green-800"
	case "cancelled":
		return "bg-gray-100 text-gray-600"
	default: // upcoming
		return "bg-blue-100 text-blue-800"
	}
}

// recStatusClasses maps a transcript pipeline status to Tailwind badge colors.
func recStatusClasses(status string) string {
	switch status {
	case "done":
		return "bg-green-100 text-green-800"
	case "failed":
		return "bg-red-100 text-red-700"
	case "processing":
		return "bg-amber-100 text-amber-800"
	default: // pending
		return "bg-slate-100 text-slate-600"
	}
}

// recActive reports whether the pipeline is still working (so the UI keeps polling).
func recActive(status string) bool {
	return status == "pending" || status == "processing"
}

func recordingURL(id int64) string { return "/recordings/" + strconv.FormatInt(id, 10) }

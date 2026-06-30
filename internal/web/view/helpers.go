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
// min-w-0 + max-w-full keep native date/datetime/select controls from overflowing
// their (often flex) container on iOS Safari, where they have a wide intrinsic size.
const inputCls = "mt-1 block w-full min-w-0 max-w-full rounded-xl border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-800 px-3.5 py-2.5 text-sm text-slate-900 dark:text-slate-100 shadow-sm placeholder:text-slate-400 dark:placeholder:text-slate-500 focus:border-teal-500 focus:ring-2 focus:ring-teal-500/30 focus:outline-none transition"

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

// keyPlaceholder hints at a secret field's state without revealing the secret.
func keyPlaceholder(dbValue string, envSet bool) string {
	switch {
	case dbValue != "":
		return "•••••••• saved — leave blank to keep"
	case envSet:
		return "•••••••• provided by environment"
	default:
		return "not set"
	}
}

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
	const out = "Mon Jan 2, 2006 · 3:04 PM"
	// SQLite datetime('now') stamps are UTC ("2006-01-02 15:04:05"); render them in
	// the server's local zone (set via the TZ env var; defaults to UTC).
	if t, err := time.ParseInLocation("2006-01-02 15:04:05", s, time.UTC); err == nil {
		return t.In(time.Local).Format(out)
	}
	// RFC3339 carries its own offset — convert to local for display.
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.In(time.Local).Format(out)
	}
	// Form-entered "datetime-local" values are wall-clock with no zone; the user
	// typed them in their own time, so keep them as-is (no shift).
	if t, err := time.ParseInLocation("2006-01-02T15:04", s, time.Local); err == nil {
		return t.Format(out)
	}
	// Date-only.
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t.Format("Mon Jan 2, 2006")
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

// statusClasses maps an appointment status to Tailwind badge colors (light + dark).
func statusClasses(status string) string {
	switch status {
	case "completed":
		return "bg-green-100 text-green-800 dark:bg-green-900/40 dark:text-green-300"
	case "cancelled":
		return "bg-gray-100 text-gray-600 dark:bg-slate-800 dark:text-slate-400"
	default: // upcoming
		return "bg-blue-100 text-blue-800 dark:bg-blue-900/40 dark:text-blue-300"
	}
}

// recStatusClasses maps a transcript pipeline status to Tailwind badge colors.
func recStatusClasses(status string) string {
	switch status {
	case "done":
		return "bg-green-100 text-green-800 dark:bg-green-900/40 dark:text-green-300"
	case "failed":
		return "bg-red-100 text-red-700 dark:bg-red-900/40 dark:text-red-300"
	case "processing":
		return "bg-amber-100 text-amber-800 dark:bg-amber-900/40 dark:text-amber-300"
	default: // pending
		return "bg-slate-100 text-slate-600 dark:bg-slate-800 dark:text-slate-300"
	}
}

// recActive reports whether the pipeline is still working (so the UI keeps polling).
func recActive(status string) bool {
	return status == "pending" || status == "processing"
}

func recordingURL(id int64) string { return "/recordings/" + strconv.FormatInt(id, 10) }

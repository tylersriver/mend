package web

import (
	"net/http"
	"strings"

	"github.com/tylersriver/mend/internal/web/view"
)

func (s *Server) settingsPage(w http.ResponseWriter, r *http.Request) {
	set, err := s.store.Settings(r.Context())
	if err != nil {
		s.fail(w, "load settings", err)
		return
	}
	st := view.SettingsStatus{
		AIEnabled:                s.aiEnabled(),
		TranscribeEnabled:        s.transcribeEnabled(),
		AuthEnabled:              s.env.AuthEnabled(),
		EnvAIKey:                 s.env.APIKey != "",
		EnvTranscribeKey:         s.env.TranscribeAPIKey != "",
		AIModelDefault:           s.env.AIModel,
		AIBaseURLDefault:         s.env.AIBaseURL,
		TranscribeBaseURLDefault: s.env.TranscribeBaseURL,
		TranscribeModelDefault:   s.env.TranscribeModel,
		Saved:                    r.URL.Query().Get("saved") == "1",
	}
	s.render(w, r, view.SettingsPage(s.profile(r.Context()), set, st))
}

func (s *Server) updateSettings(w http.ResponseWriter, r *http.Request) {
	set, err := s.store.Settings(r.Context())
	if err != nil {
		s.fail(w, "load settings", err)
		return
	}

	// Secret fields: an explicit clear wins; otherwise a non-blank value updates,
	// and a blank value leaves the stored key untouched.
	if r.FormValue("clear_anthropic") == "1" {
		set.AnthropicAPIKey = ""
	} else if v := strings.TrimSpace(r.FormValue("anthropic_api_key")); v != "" {
		set.AnthropicAPIKey = v
	}
	if r.FormValue("clear_transcribe") == "1" {
		set.TranscribeAPIKey = ""
	} else if v := strings.TrimSpace(r.FormValue("transcribe_api_key")); v != "" {
		set.TranscribeAPIKey = v
	}

	// Non-secret fields are set verbatim; blank means "fall back to the env default".
	set.AIModel = strings.TrimSpace(r.FormValue("ai_model"))
	set.AIBaseURL = strings.TrimSpace(r.FormValue("ai_base_url"))
	set.TranscribeBaseURL = strings.TrimSpace(r.FormValue("transcribe_base_url"))
	set.TranscribeModel = strings.TrimSpace(r.FormValue("transcribe_model"))

	if err := s.store.UpdateSettings(r.Context(), set); err != nil {
		s.fail(w, "save settings", err)
		return
	}
	if err := s.reconfigure(r.Context()); err != nil {
		s.fail(w, "reconfigure", err)
		return
	}
	http.Redirect(w, r, "/settings?saved=1", http.StatusSeeOther)
}

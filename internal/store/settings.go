package store

import "context"

// Settings is the in-app, DB-backed configuration row. Empty fields mean "fall
// back to the env default" — resolution happens in the caller, not here.
type Settings struct {
	AnthropicAPIKey   string
	AIModel           string
	TranscribeAPIKey  string
	TranscribeBaseURL string
	TranscribeModel   string
	UpdatedAt         string
}

func (s *Store) Settings(ctx context.Context) (Settings, error) {
	var set Settings
	err := s.db.QueryRowContext(ctx, `
		SELECT anthropic_api_key, ai_model, transcribe_api_key,
		       transcribe_base_url, transcribe_model, updated_at
		FROM app_settings WHERE id = 1`).
		Scan(&set.AnthropicAPIKey, &set.AIModel, &set.TranscribeAPIKey,
			&set.TranscribeBaseURL, &set.TranscribeModel, &set.UpdatedAt)
	return set, err
}

func (s *Store) UpdateSettings(ctx context.Context, set Settings) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE app_settings
		SET anthropic_api_key = ?, ai_model = ?, transcribe_api_key = ?,
		    transcribe_base_url = ?, transcribe_model = ?, updated_at = datetime('now')
		WHERE id = 1`,
		set.AnthropicAPIKey, set.AIModel, set.TranscribeAPIKey,
		set.TranscribeBaseURL, set.TranscribeModel)
	return err
}

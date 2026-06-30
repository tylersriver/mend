-- 0007_ai_base_url.sql
-- Let the AI flows target any OpenAI-compatible chat-completions endpoint (e.g.
-- Groq), not just Anthropic. When ai_base_url points at a non-Anthropic host, the
-- app talks the OpenAI /chat/completions dialect using the same "AI API key".
-- Empty means the default Anthropic Messages API.

ALTER TABLE app_settings ADD COLUMN ai_base_url TEXT NOT NULL DEFAULT '';

package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// OpenAIClient talks to any OpenAI-compatible /chat/completions endpoint (OpenAI,
// Groq, Together, etc.). It satisfies the same Completer interface as the Anthropic
// client, so the care-prep flows don't care which provider is configured.
//
// Differences from the Anthropic path: there's no prompt-caching dialect, so the
// SystemBlocks are flattened into a single leading "system" message and any
// CacheControl is ignored.
type OpenAIClient struct {
	http     *http.Client
	apiKey   string
	endpoint string
	Model    string
}

// NewOpenAI builds a client for an OpenAI-compatible API. baseURL is the API root
// (e.g. "https://api.groq.com/openai/v1"); "/chat/completions" is appended.
func NewOpenAI(apiKey, baseURL, model string) *OpenAIClient {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	return &OpenAIClient{
		http:     &http.Client{Timeout: 120 * time.Second},
		apiKey:   apiKey,
		endpoint: base + "/chat/completions",
		Model:    model,
	}
}

// ModelID reports the model this client is configured to use.
func (c *OpenAIClient) ModelID() string { return c.Model }

type oaiMessage struct {
	Role    string `json:"role"` // "system" | "user" | "assistant"
	Content string `json:"content"`
}

type oaiRequest struct {
	Model     string       `json:"model"`
	MaxTokens int          `json:"max_tokens"`
	Messages  []oaiMessage `json:"messages"`
}

type oaiResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Complete sends one chat-completions request and returns the message content.
func (c *OpenAIClient) Complete(ctx context.Context, system []SystemBlock, msgs []Message, maxTokens int) (string, error) {
	var sys strings.Builder
	for _, b := range system {
		sys.WriteString(b.Text)
		sys.WriteString("\n\n")
	}
	out := make([]oaiMessage, 0, len(msgs)+1)
	if s := strings.TrimSpace(sys.String()); s != "" {
		out = append(out, oaiMessage{Role: "system", Content: s})
	}
	for _, m := range msgs {
		out = append(out, oaiMessage{Role: m.Role, Content: m.Content})
	}

	body, err := json.Marshal(oaiRequest{Model: c.Model, MaxTokens: maxTokens, Messages: out})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var b bytes.Buffer
		_, _ = b.ReadFrom(resp.Body)
		return "", fmt.Errorf("chat completions %d: %s", resp.StatusCode, b.String())
	}

	var r oaiResponse
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", err
	}
	if r.Error != nil {
		return "", fmt.Errorf("chat completions: %s", r.Error.Message)
	}
	if len(r.Choices) == 0 {
		return "", fmt.Errorf("chat completions: no choices returned")
	}
	return r.Choices[0].Message.Content, nil
}

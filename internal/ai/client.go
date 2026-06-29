// Package ai is a minimal, dependency-free client for the Anthropic Messages API
// plus the care-prep flows the app needs. Single provider, one code path.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const (
	endpoint   = "https://api.anthropic.com/v1/messages"
	apiVersion = "2023-06-01"
)

// Client is a thin transport over POST /v1/messages.
type Client struct {
	http   *http.Client
	apiKey string
	Model  string
}

// New builds a client. Pick a model per use: a stronger model for reasoning-heavy
// flows (questions, plans), a cheaper one for high-volume summarization.
// Verify exact model IDs on the models page before shipping.
func New(apiKey, model string) *Client {
	return &Client{
		http:   &http.Client{Timeout: 120 * time.Second}, // generation can be slow
		apiKey: apiKey,
		Model:  model,
	}
}

// SystemBlock lets us mark the large, reused case-file context for prompt caching.
type SystemBlock struct {
	Type         string        `json:"type"`
	Text         string        `json:"text"`
	CacheControl *CacheControl `json:"cache_control,omitempty"`
}

type CacheControl struct {
	Type string `json:"type"` // "ephemeral"
}

type Message struct {
	Role    string `json:"role"` // "user" | "assistant"
	Content string `json:"content"`
}

// NOTE: no Temperature/TopP/TopK fields — Opus 4.7+ rejects non-default sampling
// params with a 400. Omit them entirely and steer behavior via prompting.
type request struct {
	Model     string        `json:"model"`
	MaxTokens int           `json:"max_tokens"` // required
	System    []SystemBlock `json:"system,omitempty"`
	Messages  []Message     `json:"messages"`
}

type response struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
}

// Complete sends one request and returns the concatenated text content.
func (c *Client) Complete(ctx context.Context, system []SystemBlock, msgs []Message, maxTokens int) (string, error) {
	body, err := json.Marshal(request{
		Model: c.Model, MaxTokens: maxTokens, System: system, Messages: msgs,
	})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", apiVersion)
	req.Header.Set("content-type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var b bytes.Buffer
		_, _ = b.ReadFrom(resp.Body)
		return "", fmt.Errorf("anthropic %d: %s", resp.StatusCode, b.String())
	}

	var out response
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	// Opus 4.7+ can return a refusal; don't assume content[0].text exists.
	if out.StopReason == "refusal" {
		return "", fmt.Errorf("model declined to respond")
	}

	var text string
	for _, blk := range out.Content {
		if blk.Type == "text" {
			text += blk.Text
		}
	}
	return text, nil
}

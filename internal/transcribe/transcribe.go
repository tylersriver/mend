// Package transcribe is a minimal, dependency-free client for an OpenAI-compatible
// /audio/transcriptions endpoint (Whisper-class). Per the design log, speech-to-text
// is done server-side — the browser only records and uploads — and this is kept
// separate from the Anthropic care-prep path. Provider-agnostic: point BaseURL at
// any compatible API (OpenAI, a self-hosted whisper.cpp server, etc.).
package transcribe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// Transcriber turns an on-disk audio file into text. The web layer depends on this
// interface, so transcription stays swappable (and nil-able when unconfigured).
type Transcriber interface {
	Transcribe(ctx context.Context, audioPath string) (string, error)
}

// Client calls a Whisper-compatible transcription endpoint.
type Client struct {
	http    *http.Client
	apiKey  string
	baseURL string
	model   string
}

func New(apiKey, baseURL, model string) *Client {
	return &Client{
		http:    &http.Client{Timeout: 5 * time.Minute}, // audio can be long
		apiKey:  apiKey,
		baseURL: baseURL,
		model:   model,
	}
}

// Transcribe uploads the file as multipart/form-data and returns the transcript.
func (c *Client) Transcribe(ctx context.Context, audioPath string) (string, error) {
	f, err := os.Open(audioPath)
	if err != nil {
		return "", fmt.Errorf("open audio: %w", err)
	}
	defer f.Close()

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("file", filepath.Base(audioPath))
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(part, f); err != nil {
		return "", err
	}
	if err := mw.WriteField("model", c.model); err != nil {
		return "", err
	}
	// Ask for plain JSON ({"text": "..."}).
	if err := mw.WriteField("response_format", "json"); err != nil {
		return "", err
	}
	if err := mw.Close(); err != nil {
		return "", err
	}

	url := c.baseURL + "/audio/transcriptions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", mw.FormDataContentType())

	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var b bytes.Buffer
		_, _ = b.ReadFrom(io.LimitReader(resp.Body, 4<<10))
		return "", fmt.Errorf("transcribe %d: %s", resp.StatusCode, b.String())
	}

	var out struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.Text, nil
}

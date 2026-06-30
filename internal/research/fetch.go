// Package research fetches readable source text for a saved resource so the AI
// can summarize what's actually in it — most importantly, the transcript of a
// YouTube video, where the URL alone gives the model nothing to work with.
//
// Everything here is best-effort: callers fall back to whatever notes/title they
// already have when SourceText returns an error. The page-parsing helpers are
// pure functions (no network) so they can be unit-tested against fixtures.
package research

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

const (
	// Bound how much we pull back so a long transcript or page can't blow up the
	// prompt (and our memory). ~16k chars is plenty for a 3-5 sentence summary.
	maxSourceChars = 16000
	// A desktop UA so YouTube serves the full watch page with the player config.
	userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 " +
		"(KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
)

// SourceText returns text to summarize for a URL: a YouTube transcript (or the
// video description as a fallback) for YouTube links, otherwise the page's
// visible text. Returns an error when nothing usable could be fetched.
func SourceText(ctx context.Context, client *http.Client, rawURL string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "", fmt.Errorf("parse url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("unsupported url scheme %q", u.Scheme)
	}
	if id, ok := youTubeID(u); ok {
		return youTubeText(ctx, client, id)
	}
	html, err := fetch(ctx, client, u.String())
	if err != nil {
		return "", err
	}
	text := htmlToText(html)
	if text == "" {
		return "", fmt.Errorf("no readable text at %s", u.Host)
	}
	return clip(text), nil
}

// fetch GETs a URL and returns the body as a string.
func fetch(ctx context.Context, client *http.Client, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20)) // 4MB ceiling
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// youTubeID extracts a video id from the common YouTube URL shapes.
func youTubeID(u *url.URL) (string, bool) {
	host := strings.ToLower(u.Host)
	host = strings.TrimPrefix(host, "www.")
	host = strings.TrimPrefix(host, "m.")
	switch host {
	case "youtu.be":
		if id := strings.Trim(u.Path, "/"); id != "" {
			return id, true
		}
	case "youtube.com", "music.youtube.com":
		if v := u.Query().Get("v"); v != "" {
			return v, true
		}
		// /shorts/{id} and /embed/{id}
		for _, p := range []string{"/shorts/", "/embed/", "/live/"} {
			if strings.HasPrefix(u.Path, p) {
				if id := strings.Trim(strings.TrimPrefix(u.Path, p), "/"); id != "" {
					return id, true
				}
			}
		}
	}
	return "", false
}

// youTubeText fetches the watch page, then the first caption track's text; if
// there are no captions it falls back to the video's description.
func youTubeText(ctx context.Context, client *http.Client, id string) (string, error) {
	page, err := fetch(ctx, client, "https://www.youtube.com/watch?v="+url.QueryEscape(id)+"&hl=en")
	if err != nil {
		return "", err
	}
	if capURL, ok := firstCaptionBaseURL(page); ok {
		body, err := fetch(ctx, client, capURL+"&fmt=json3")
		if err == nil {
			if t := parseJSON3(body); t != "" {
				return clip(t), nil
			}
		}
	}
	if desc, ok := jsonStringAfter(page, `"shortDescription":"`); ok && strings.TrimSpace(desc) != "" {
		return clip(desc), nil
	}
	return "", fmt.Errorf("no captions or description found for video %s", id)
}

// firstCaptionBaseURL pulls the first caption track's baseUrl out of the player
// config embedded in a YouTube watch page.
func firstCaptionBaseURL(page string) (string, bool) {
	i := strings.Index(page, `"captionTracks":`)
	if i < 0 {
		return "", false
	}
	return jsonStringAfter(page[i:], `"baseUrl":"`)
}

// jsonStringAfter finds marker (which must end at the opening quote of a JSON
// string value) and decodes the string literal that follows it. Decoding via
// encoding/json handles &, \/, and friends.
func jsonStringAfter(s, marker string) (string, bool) {
	i := strings.Index(s, marker)
	if i < 0 {
		return "", false
	}
	start := i + len(marker) - 1 // index of the opening quote
	j := start + 1
	for j < len(s) {
		if s[j] == '\\' {
			j += 2
			continue
		}
		if s[j] == '"' {
			break
		}
		j++
	}
	if j >= len(s) {
		return "", false
	}
	var out string
	if err := json.Unmarshal([]byte(s[start:j+1]), &out); err != nil {
		return "", false
	}
	return out, true
}

// json3 is YouTube's timedtext JSON format (fmt=json3).
type json3 struct {
	Events []struct {
		Segs []struct {
			Utf8 string `json:"utf8"`
		} `json:"segs"`
	} `json:"events"`
}

// parseJSON3 flattens a json3 caption document into a single transcript string.
func parseJSON3(body string) string {
	var doc json3
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		return ""
	}
	var sb strings.Builder
	for _, e := range doc.Events {
		for _, seg := range e.Segs {
			sb.WriteString(seg.Utf8)
		}
	}
	return collapseWS(sb.String())
}

var (
	tagRe     = regexp.MustCompile(`(?is)<(script|style)[^>]*>.*?</(script|style)>`)
	anyTagRe  = regexp.MustCompile(`(?s)<[^>]+>`)
	entityMap = strings.NewReplacer(
		"&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`,
		"&#39;", "'", "&#x27;", "'", "&nbsp;", " ",
	)
)

// htmlToText strips scripts/styles/tags and returns collapsed visible text.
func htmlToText(html string) string {
	html = tagRe.ReplaceAllString(html, " ")
	html = anyTagRe.ReplaceAllString(html, " ")
	html = entityMap.Replace(html)
	return collapseWS(html)
}

// collapseWS trims and collapses runs of whitespace to single spaces.
func collapseWS(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// clip truncates to maxSourceChars on a rune boundary.
func clip(s string) string {
	if len(s) <= maxSourceChars {
		return s
	}
	r := []rune(s)
	if len(r) <= maxSourceChars {
		return s
	}
	return string(r[:maxSourceChars])
}

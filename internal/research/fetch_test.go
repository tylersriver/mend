package research

import (
	"net/url"
	"testing"
)

func TestYouTubeID(t *testing.T) {
	cases := map[string]string{
		"https://www.youtube.com/watch?v=aircAruvnKk":         "aircAruvnKk",
		"https://youtube.com/watch?v=abc123&t=10s":            "abc123",
		"https://youtu.be/dQw4w9WgXcQ":                        "dQw4w9WgXcQ",
		"https://m.youtube.com/watch?v=xyz":                   "xyz",
		"https://www.youtube.com/shorts/SHORT123":             "SHORT123",
		"https://www.youtube.com/embed/EMBED99":               "EMBED99",
		"https://music.youtube.com/watch?v=song1":             "song1",
	}
	for in, want := range cases {
		u, _ := url.Parse(in)
		got, ok := youTubeID(u)
		if !ok || got != want {
			t.Errorf("youTubeID(%q) = %q,%v; want %q,true", in, got, ok, want)
		}
	}

	for _, in := range []string{
		"https://example.com/watch?v=nope",
		"https://vimeo.com/12345",
		"https://www.youtube.com/feed/subscriptions",
	} {
		u, _ := url.Parse(in)
		if _, ok := youTubeID(u); ok {
			t.Errorf("youTubeID(%q) unexpectedly matched", in)
		}
	}
}

func TestFirstCaptionBaseURL(t *testing.T) {
	// baseUrl as it really appears in the watch page: & escaped as &.
	page := `...,"captionTracks":[{"baseUrl":"https://www.youtube.com/api/timedtext?v=abc&lang=en&fmt=srv3","name":{}}],...`
	got, ok := firstCaptionBaseURL(page)
	if !ok {
		t.Fatal("expected a caption baseUrl")
	}
	want := "https://www.youtube.com/api/timedtext?v=abc&lang=en&fmt=srv3"
	if got != want {
		t.Errorf("baseUrl = %q; want %q", got, want)
	}

	if _, ok := firstCaptionBaseURL(`{"no":"captions here"}`); ok {
		t.Error("expected no caption baseUrl")
	}
}

func TestJSONStringAfterShortDescription(t *testing.T) {
	page := `x,"shortDescription":"Line one.\nLine two with \"quotes\" and a URL https://x.y","y":1`
	got, ok := jsonStringAfter(page, `"shortDescription":"`)
	if !ok {
		t.Fatal("expected a description")
	}
	want := "Line one.\nLine two with \"quotes\" and a URL https://x.y"
	if got != want {
		t.Errorf("desc = %q; want %q", got, want)
	}
}

func TestParseJSON3(t *testing.T) {
	body := `{"events":[{"segs":[{"utf8":"Hello"},{"utf8":" world"}]},{"segs":[{"utf8":"\n"}]},{"segs":[{"utf8":"second line"}]}]}`
	got := parseJSON3(body)
	want := "Hello world second line"
	if got != want {
		t.Errorf("parseJSON3 = %q; want %q", got, want)
	}
	if parseJSON3("not json") != "" {
		t.Error("expected empty string on bad json")
	}
}

func TestHTMLToText(t *testing.T) {
	html := `<html><head><style>.a{color:red}</style><script>var x=1<2;</script></head>` +
		`<body><h1>Title</h1><p>Hello &amp; welcome &lt;here&gt;</p></body></html>`
	got := htmlToText(html)
	want := "Title Hello & welcome <here>"
	if got != want {
		t.Errorf("htmlToText = %q; want %q", got, want)
	}
}

func TestClip(t *testing.T) {
	long := make([]rune, maxSourceChars+50)
	for i := range long {
		long[i] = 'a'
	}
	if got := clip(string(long)); len([]rune(got)) != maxSourceChars {
		t.Errorf("clip length = %d; want %d", len([]rune(got)), maxSourceChars)
	}
	if got := clip("short"); got != "short" {
		t.Errorf("clip(short) = %q", got)
	}
}

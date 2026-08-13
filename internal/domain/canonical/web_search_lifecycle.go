package canonical

import (
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"
)

// WebSearchAction identifies an observed action inside one provider-executed
// search lifecycle. Open-page and find-in-page do not declare fetch tools.
type WebSearchAction string

const (
	WebSearchActionSearch     WebSearchAction = "search"
	WebSearchActionOpenPage   WebSearchAction = "open_page"
	WebSearchActionFindInPage WebSearchAction = "find_in_page"
)

// WebSearchCall records the provider action and only its portable observed
// arguments. Providers may omit undisclosed queries.
type WebSearchCall struct {
	Action             WebSearchAction
	Queries            []string
	URL                Specified[WebURL]
	Match              Specified[string]
	interactionsReplay []byte
}

// NewInteractionsWebSearchCall attaches one opaque Interactions replay
// candidate to its portable semantic occurrence. The owning Interactions
// adapter validates private grammar and correlation before use.
func NewInteractionsWebSearchCall(call WebSearchCall, replay []byte) (WebSearchCall, error) {
	if err := call.Validate(); err != nil {
		return WebSearchCall{}, err
	}
	if len(replay) == 0 {
		return WebSearchCall{}, BadRequest("interactions web-search call replay is empty")
	}
	call.interactionsReplay = append([]byte(nil), replay...)
	return call.Clone(), nil
}

func (c WebSearchCall) InteractionsReplay() ([]byte, bool) {
	if len(c.interactionsReplay) == 0 {
		return nil, false
	}
	return append([]byte(nil), c.interactionsReplay...), true
}

func (c WebSearchCall) Validate() error {
	for _, query := range c.Queries {
		if strings.TrimSpace(query) == "" { // swobu:io-string source=domain
			return BadRequest("canonical web-search query is empty")
		}
	}
	webURL, hasURL := c.URL.Get()
	if hasURL && webURL.rawURL == "" {
		return BadRequest("canonical web-search call URL is invalid")
	}
	match, hasMatch := c.Match.Get()
	if hasMatch && strings.TrimSpace(match) == "" { // swobu:io-string source=domain
		return BadRequest("canonical web-search find match is empty")
	}
	switch c.Action {
	case WebSearchActionSearch:
		if hasURL || hasMatch {
			return BadRequest("canonical web-search search action cannot carry URL or match")
		}
	case WebSearchActionOpenPage:
		if !hasURL || len(c.Queries) > 0 || hasMatch {
			return BadRequest("canonical web-search open-page action has invalid fields")
		}
	case WebSearchActionFindInPage:
		if !hasURL || !hasMatch || len(c.Queries) > 0 {
			return BadRequest("canonical web-search find-in-page action has invalid fields")
		}
	default:
		return BadRequest("canonical web-search action is invalid")
	}
	return nil
}

func (c WebSearchCall) Clone() WebSearchCall {
	return WebSearchCall{
		Action:             c.Action,
		Queries:            append([]string(nil), c.Queries...),
		URL:                cloneSpecified(c.URL, func(value WebURL) WebURL { return value }),
		Match:              cloneSpecified(c.Match, func(value string) string { return value }),
		interactionsReplay: append([]byte(nil), c.interactionsReplay...),
	}
}

// WebURL is an exact validated absolute HTTP(S) source URL without credentials.
// Fragments are retained because providers may cite them.
type WebURL struct{ rawURL string }

func NewWebURL(raw string) (WebURL, error) {
	if raw == "" || strings.TrimSpace(raw) != raw { // swobu:io-string source=domain
		return WebURL{}, BadRequest("canonical web URL is invalid")
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.Hostname() == "" || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return WebURL{}, BadRequest("canonical web URL must be absolute HTTP(S) without credentials")
	}
	return WebURL{rawURL: raw}, nil
}

func (u WebURL) String() string { return u.rawURL }

// WebSource is one trusted URL-backed source record. Omitted metadata is not
// synthesized, and repeated provider records remain distinct.
type WebSource struct {
	URL            WebURL
	Title          Specified[string]
	messagesReplay []byte
}

func NewWebSource(url WebURL, title Specified[string]) (WebSource, error) {
	if url.rawURL == "" {
		return WebSource{}, BadRequest("canonical web source requires a URL")
	}
	if value, ok := title.Get(); ok && strings.TrimSpace(value) == "" { // swobu:io-string source=domain
		return WebSource{}, BadRequest("canonical web source title is empty")
	}
	return WebSource{URL: url, Title: cloneSpecified(title, func(value string) string { return value })}, nil
}

// NewMessagesWebSource retains one complete Messages-native hosted-search
// result while exposing its portable URL and title to the canonical model.
func NewMessagesWebSource(url WebURL, title Specified[string], replay []byte) (WebSource, error) {
	source, err := NewWebSource(url, title)
	if err != nil {
		return WebSource{}, err
	}
	if len(replay) == 0 {
		return WebSource{}, BadRequest("messages web source replay is empty")
	}
	source.messagesReplay = append([]byte(nil), replay...)
	return source, nil
}

// MessagesReplay returns independent bytes only when this source originated
// as one complete Messages-native hosted-search result.
func (s WebSource) MessagesReplay() ([]byte, bool) {
	if len(s.messagesReplay) == 0 {
		return nil, false
	}
	return append([]byte(nil), s.messagesReplay...), true
}

func (s WebSource) validate() error {
	_, err := NewWebSource(s.URL, s.Title)
	return err
}

func (s WebSource) Clone() WebSource {
	return WebSource{
		URL:            s.URL,
		Title:          cloneSpecified(s.Title, func(value string) string { return value }),
		messagesReplay: append([]byte(nil), s.messagesReplay...),
	}
}

// WebSearchResult is either a successful ordered source list or a typed
// failure. Success with zero sources is valid.
type WebSearchResult struct {
	sources            []WebSource
	failure            *string
	interactionsReplay []byte
}

// WithInteractionsReplay attaches one opaque Interactions replay candidate
// without changing its portable success/failure meaning. The owning
// Interactions adapter validates private grammar and correlation before use.
func (r WebSearchResult) WithInteractionsReplay(replay []byte) (WebSearchResult, error) {
	if !r.valid() {
		return WebSearchResult{}, BadRequest("canonical web-search result is invalid")
	}
	if len(replay) == 0 {
		return WebSearchResult{}, BadRequest("interactions web-search result replay is empty")
	}
	r = r.Clone()
	r.interactionsReplay = append([]byte(nil), replay...)
	return r, nil
}

func (r WebSearchResult) InteractionsReplay() ([]byte, bool) {
	if len(r.interactionsReplay) == 0 {
		return nil, false
	}
	return append([]byte(nil), r.interactionsReplay...), true
}

func NewWebSearchResult(sources []WebSource) (WebSearchResult, error) {
	out := make([]WebSource, len(sources))
	for index, source := range sources {
		if err := source.validate(); err != nil {
			return WebSearchResult{}, fmt.Errorf("canonical web-search source %d: %w", index, err)
		}
		out[index] = source.Clone()
	}
	return WebSearchResult{sources: out}, nil
}

func NewWebSearchFailureResult(message string) (WebSearchResult, error) {
	if strings.TrimSpace(message) == "" { // swobu:io-string source=domain
		return WebSearchResult{}, BadRequest("canonical web-search failure message is empty")
	}
	copy := message
	return WebSearchResult{failure: &copy}, nil
}

func (r WebSearchResult) Sources() []WebSource {
	if r.failure != nil {
		return nil
	}
	out := make([]WebSource, len(r.sources))
	for index := range r.sources {
		out[index] = r.sources[index].Clone()
	}
	return out
}

func (r WebSearchResult) Failure() (string, bool) {
	if r.failure == nil || len(r.sources) > 0 {
		return "", false
	}
	return *r.failure, true
}

func (r WebSearchResult) valid() bool {
	if r.failure != nil {
		return len(r.sources) == 0 && strings.TrimSpace(*r.failure) != ""
	}
	for _, source := range r.sources {
		if source.validate() != nil {
			return false
		}
	}
	return true
}

func (r WebSearchResult) Clone() WebSearchResult {
	if failure, ok := r.Failure(); ok {
		cloned, _ := NewWebSearchFailureResult(failure)
		cloned.interactionsReplay = append([]byte(nil), r.interactionsReplay...)
		return cloned
	}
	cloned, _ := NewWebSearchResult(r.sources)
	cloned.interactionsReplay = append([]byte(nil), r.interactionsReplay...)
	return cloned
}

// WebCitation attaches one trusted source, optional cited evidence, and an
// optional UTF-8 byte span to a text message part.
type WebCitation struct {
	Source  WebSource
	Excerpt Specified[string]
	Start   Specified[uint32]
	End     Specified[uint32]
}

func (c WebCitation) validate(text string) error {
	if err := c.Source.validate(); err != nil {
		return err
	}
	if excerpt, ok := c.Excerpt.Get(); ok && strings.TrimSpace(excerpt) == "" { // swobu:io-string source=domain
		return BadRequest("canonical web citation excerpt is empty")
	}
	start, hasStart := c.Start.Get()
	end, hasEnd := c.End.Get()
	if hasStart != hasEnd {
		return BadRequest("canonical web citation offsets must both be present or absent")
	}
	if !hasStart {
		return nil
	}
	if start >= end || uint64(end) > uint64(len(text)) || !utf8Boundary(text, int(start)) || !utf8Boundary(text, int(end)) {
		return BadRequest("canonical web citation offsets are invalid UTF-8 byte boundaries")
	}
	return nil
}

func utf8Boundary(text string, offset int) bool {
	return offset == 0 || offset == len(text) || offset > 0 && offset < len(text) && utf8.RuneStart(text[offset])
}

func (c WebCitation) Clone() WebCitation {
	return WebCitation{Source: c.Source.Clone(), Excerpt: cloneSpecified(c.Excerpt, func(value string) string { return value }), Start: cloneSpecified(c.Start, func(value uint32) uint32 { return value }), End: cloneSpecified(c.End, func(value uint32) uint32 { return value })}
}

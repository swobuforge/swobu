package search

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultMaxResponseBytes int64 = 1 << 20

// JSONSource is a bounded adapter for a qualified JSON search endpoint (for
// example, a SearXNG JSON surface). Endpoint configuration is trusted runtime
// wiring; no credential or transport detail enters Search results.
type JSONSource struct {
	Endpoint         *url.URL
	Client           *http.Client
	MaxResponseBytes int64
	Authorization    string
}

func (s JSONSource) WithBearer(token string) JSONSource {
	s.Authorization = strings.TrimSpace(token)
	return s
}

// NewJSONSource validates one fixed search endpoint. Query text is always
// encoded as a URL parameter; callers cannot supply a callback URL or scheme.
func NewJSONSource(rawEndpoint string, client *http.Client) (JSONSource, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawEndpoint))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil {
		return JSONSource{}, errors.New("search endpoint must be an absolute URL without userinfo")
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return JSONSource{}, errors.New("search endpoint must use HTTP or HTTPS")
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return JSONSource{Endpoint: parsed, Client: client, MaxResponseBytes: defaultMaxResponseBytes}, nil
}

type jsonSearchResponse struct {
	Results []jsonSearchResult `json:"results"`
}

type jsonSearchResult struct {
	URL     string `json:"url"`
	Title   string `json:"title"`
	Content string `json:"content"`
	Snippet string `json:"snippet"`
}

// Search executes one fixed-endpoint query and rejects malformed or oversized
// upstream responses rather than fabricating partial results.
func (s JSONSource) Search(ctx context.Context, q Query) ([]Result, error) {
	if s.Endpoint == nil || s.Client == nil {
		return nil, ErrNotConfigured
	}
	endpoint := *s.Endpoint
	params := endpoint.Query()
	params.Set("q", q.Text)
	params.Set("format", "json")
	if q.Limit > 0 {
		params.Set("number_of_results", fmt.Sprintf("%d", q.Limit))
	}
	endpoint.RawQuery = params.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	if s.Authorization != "" {
		request.Header.Set("Authorization", "Bearer "+s.Authorization)
	}
	response, err := s.Client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("search source returned HTTP %d", response.StatusCode)
	}
	limit := s.MaxResponseBytes
	if limit <= 0 {
		limit = defaultMaxResponseBytes
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, errors.New("search source response exceeds the byte limit")
	}
	var decoded jsonSearchResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, fmt.Errorf("search source returned invalid JSON: %w", err)
	}
	results := make([]Result, 0, len(decoded.Results))
	for _, item := range decoded.Results {
		snippet := item.Snippet
		if snippet == "" {
			snippet = item.Content
		}
		if strings.TrimSpace(item.URL) == "" || strings.TrimSpace(item.Title) == "" {
			return nil, errors.New("search source returned an incomplete result")
		}
		results = append(results, Result{URL: item.URL, Title: item.Title, Snippet: snippet, Source: endpoint.Host})
		if len(results) == q.Limit {
			break
		}
	}
	return results, nil
}

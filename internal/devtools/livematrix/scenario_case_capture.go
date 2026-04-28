package livematrix

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type ScenarioCase struct {
	ID         string `json:"id"`
	Provider   string `json:"provider"`
	Protocol   string `json:"protocol"`
	Transport  string `json:"transport"`
	Scenario   string `json:"scenario"`
	BaseURL    string `json:"base_url,omitempty"`
	BaseURLEnv string `json:"base_url_env,omitempty"`
	APIKeyEnv  string `json:"api_key_env,omitempty"`
	Model      string `json:"model,omitempty"`
	ModelEnv   string `json:"model_env,omitempty"`
}

type Capture struct {
	CapturedAt   time.Time            `json:"captured_at"`
	ScenarioCase ScenarioCase         `json:"scenario_case"`
	Request      CapturedWire         `json:"request"`
	Response     CapturedWire         `json:"response"`
	Client       CapturedFlow         `json:"client,omitempty"`
	Session      *SessionTraceCapture `json:"session,omitempty"`
	Error        string               `json:"error,omitempty"`
	DurationMS   int                  `json:"duration_ms"`
}

type CapturedWire struct {
	Method     string            `json:"method,omitempty"`
	URL        string            `json:"url,omitempty"`
	Path       string            `json:"path,omitempty"`
	Headers    map[string]string `json:"headers,omitempty"`
	StatusCode int               `json:"status_code,omitempty"`
	Body       string            `json:"body,omitempty"`
}

type CapturedFlow struct {
	Request  CapturedWire `json:"request"`
	Response CapturedWire `json:"response"`
}

type SessionTraceCapture struct {
	TraceID   string       `json:"session_id"`
	RequestID string       `json:"request_id"`
	Events    []TraceEvent `json:"events"`
}

type TraceEvent struct {
	Seq          int          `json:"seq"`
	Direction    string       `json:"direction"`
	AttemptIndex int          `json:"attempt_index,omitempty"`
	At           time.Time    `json:"at"`
	Wire         CapturedWire `json:"wire"`
}

func (c *Capture) UnmarshalJSON(data []byte) error {
	type captureAlias struct {
		CapturedAt         time.Time            `json:"captured_at"`
		ScenarioCase       *ScenarioCase        `json:"scenario_case"`
		LegacyScenarioCase *ScenarioCase        `json:"tuple"`
		Request            CapturedWire         `json:"request"`
		Response           CapturedWire         `json:"response"`
		Client             CapturedFlow         `json:"client,omitempty"`
		Session            *SessionTraceCapture `json:"session,omitempty"`
		Error              string               `json:"error,omitempty"`
		DurationMS         int                  `json:"duration_ms"`
	}
	var decoded captureAlias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	c.CapturedAt = decoded.CapturedAt
	c.Request = decoded.Request
	c.Response = decoded.Response
	c.Client = decoded.Client
	c.Session = decoded.Session
	c.Error = decoded.Error
	c.DurationMS = decoded.DurationMS
	switch {
	case decoded.ScenarioCase != nil:
		c.ScenarioCase = *decoded.ScenarioCase
	case decoded.LegacyScenarioCase != nil:
		c.ScenarioCase = *decoded.LegacyScenarioCase
	default:
		c.ScenarioCase = ScenarioCase{}
	}
	return nil
}

func (c Capture) MarshalJSON() ([]byte, error) {
	type captureAlias Capture
	return json.Marshal(captureAlias(c))
}

func LoadScenarioCases(path string) ([]ScenarioCase, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var scenarioCases []ScenarioCase
	if err := json.Unmarshal(raw, &scenarioCases); err != nil {
		return nil, err
	}
	for i := range scenarioCases {
		scenarioCases[i].ID = strings.TrimSpace(scenarioCases[i].ID)
		if scenarioCases[i].ID == "" {
			return nil, fmt.Errorf("scenario_case[%d] missing id", i)
		}
	}
	return scenarioCases, nil
}

func ResolveScenarioCase(scenarioCase ScenarioCase) (ScenarioCase, error) {
	resolved := scenarioCase
	if resolved.BaseURL == "" && strings.TrimSpace(resolved.BaseURLEnv) != "" {
		resolved.BaseURL = strings.TrimSpace(os.Getenv(strings.TrimSpace(resolved.BaseURLEnv)))
	}
	if resolved.BaseURL == "" {
		resolved.BaseURL = defaultBaseURLForProvider(resolved.Provider)
	}
	if resolved.BaseURL == "" {
		return ScenarioCase{}, fmt.Errorf("scenario_case %q missing base url", scenarioCase.ID)
	}
	if resolved.Model == "" && strings.TrimSpace(resolved.ModelEnv) != "" {
		resolved.Model = strings.TrimSpace(os.Getenv(strings.TrimSpace(resolved.ModelEnv)))
	}
	if resolved.Model == "" {
		resolved.Model = "gpt-4.1-mini"
	}
	return resolved, nil
}

func CaptureScenarioCase(ctx context.Context, httpClient *http.Client, scenarioCase ScenarioCase) (Capture, error) {
	start := time.Now()
	cap := Capture{CapturedAt: start, ScenarioCase: scenarioCase}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}
	method, path, payload, err := buildScenarioRequest(scenarioCase)
	if err != nil {
		cap.Error = err.Error()
		cap.DurationMS = int(time.Since(start).Milliseconds())
		return cap, err
	}
	url := strings.TrimRight(scenarioCase.BaseURL, "/") + path
	rawBody, err := json.Marshal(payload)
	if err != nil {
		cap.Error = err.Error()
		cap.DurationMS = int(time.Since(start).Milliseconds())
		return cap, err
	}
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(rawBody))
	if err != nil {
		cap.Error = err.Error()
		cap.DurationMS = int(time.Since(start).Milliseconds())
		return cap, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "swobu-live-matrix/1")
	if strings.TrimSpace(scenarioCase.APIKeyEnv) != "" {
		key := strings.TrimSpace(os.Getenv(strings.TrimSpace(scenarioCase.APIKeyEnv)))
		if key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
	}

	cap.Request = CapturedWire{
		Method:  method,
		URL:     url,
		Path:    path,
		Headers: pickHeaders(req.Header, "Content-Type", "User-Agent"),
		Body:    string(rawBody),
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		cap.Error = err.Error()
		cap.DurationMS = int(time.Since(start).Milliseconds())
		return cap, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	cap.Response = CapturedWire{
		StatusCode: resp.StatusCode,
		Headers:    pickHeaders(resp.Header, "Content-Type", "X-Request-Id", "Retry-After"),
		Body:       string(body),
	}
	cap.DurationMS = int(time.Since(start).Milliseconds())
	if resp.StatusCode >= 400 {
		cap.Error = fmt.Sprintf("http %d", resp.StatusCode)
		return cap, fmt.Errorf("scenario_case %q failed with status %d", scenarioCase.ID, resp.StatusCode)
	}
	return cap, nil
}

func SaveCapture(outDir string, capture Capture) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(capture, "", "  ")
	if err != nil {
		return err
	}
	file := filepath.Join(outDir, capture.ScenarioCase.ID+".json")
	return os.WriteFile(file, raw, 0o644)
}

func defaultBaseURLForProvider(provider string) string {
	switch strings.TrimSpace(strings.ToLower(provider)) {
	case "openai":
		return "https://api.openai.com/v1"
	case "openrouter":
		return "https://openrouter.ai/api/v1"
	case "ollama":
		return "http://127.0.0.1:11434/v1"
	case "anthropic":
		return "https://api.anthropic.com/v1"
	default:
		return ""
	}
}

func pickHeaders(headers http.Header, keys ...string) map[string]string {
	out := map[string]string{}
	for _, key := range keys {
		value := strings.TrimSpace(headers.Get(key))
		if value != "" {
			out[key] = value
		}
	}
	return out
}

func buildScenarioRequest(scenarioCase ScenarioCase) (string, string, map[string]any, error) {
	model := scenarioCase.Model
	if model == "" {
		model = "gpt-4.1-mini"
	}
	scenario := strings.TrimSpace(strings.ToLower(scenarioCase.Scenario))
	transport := strings.TrimSpace(strings.ToLower(scenarioCase.Transport))
	stream := transport == "sse_streaming"
	switch strings.TrimSpace(strings.ToLower(scenarioCase.Protocol)) {
	case "chat_completions":
		if scenario == "tool_min" {
			return http.MethodPost, "/chat/completions", map[string]any{
				"model":      model,
				"stream":     stream,
				"max_tokens": 16,
				"messages": []map[string]any{{
					"role":    "user",
					"content": "Call tool noop with x=1 and stop.",
				}},
				"tools": []map[string]any{{
					"type": "function",
					"function": map[string]any{
						"name":        "noop",
						"description": "No-op tool",
						"parameters": map[string]any{
							"type":       "object",
							"properties": map[string]any{"x": map[string]any{"type": "integer"}},
							"required":   []string{"x"},
						},
					},
				}},
				"tool_choice": map[string]any{"type": "function", "function": map[string]any{"name": "noop"}},
			}, nil
		}
		return http.MethodPost, "/chat/completions", map[string]any{
			"model":      model,
			"stream":     stream,
			"max_tokens": 16,
			"messages": []map[string]any{{
				"role":    "user",
				"content": "Reply with OK",
			}},
		}, nil
	case "responses":
		return http.MethodPost, "/responses", map[string]any{
			"model":             model,
			"stream":            stream,
			"max_output_tokens": 32,
			"input":             "Reply with OK",
		}, nil
	case "completions":
		return http.MethodPost, "/completions", map[string]any{
			"model":      model,
			"stream":     stream,
			"max_tokens": 16,
			"prompt":     "Reply with OK",
		}, nil
	case "messages":
		return http.MethodPost, "/messages", map[string]any{
			"model":      model,
			"max_tokens": 32,
			"messages": []map[string]any{{
				"role":    "user",
				"content": "Reply with OK",
			}},
		}, nil
	default:
		return "", "", nil, fmt.Errorf("unsupported protocol %q", scenarioCase.Protocol)
	}
}

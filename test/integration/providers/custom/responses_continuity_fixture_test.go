package custom

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	customadapter "github.com/metrofun/swobu/internal/adapters/outbound/providers/custom"
	"github.com/metrofun/swobu/internal/domain/compatibility"
	"github.com/metrofun/swobu/internal/ports"
)

func TestCustomAdapter_DecodesRealBufferedResponsesPayloadFixture(t *testing.T) {
	t.Parallel()

	fixture := mustReadResponsesContinuityFixture(t, "openrouter_buffered_ok.json")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture)
	}))
	defer upstream.Close()

	executor := customadapter.NewExecutor(upstream.Client(), nil)
	resp, err := executor.Execute(context.Background(), ports.NewExecuteRequest(
		compatibility.NewGenerationRequest(compatibility.GenerationRequestParams{Model: "m", InputText: "hi"}), ports.NewExecutionContract(compatibility.DeliveryModeBuffered),
		ports.NewRoutableTarget("backend-a", "custom", upstream.URL+"/v1", "", "responses", "", ""),
	))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	output, ok := resp.Output().(compatibility.CanonicalOutputValue)
	if !ok {
		t.Fatalf("output type = %T, want compatibility.CanonicalOutputValue", resp.Output())
	}
	expectedID := mustExtractResponsesJSONID(t, fixture)
	if got := output.ResultID(); got != expectedID {
		t.Fatalf("result id = %q, want %q", got, expectedID)
	}
	if got := output.Text(); !strings.EqualFold(got, "ok") {
		t.Fatalf("output text = %q, want case-insensitive %q", got, "ok")
	}
}

func TestCustomAdapter_DecodesRealStreamingResponsesPayloadFixture(t *testing.T) {
	t.Parallel()

	fixture := mustReadResponsesContinuityFixture(t, "openrouter_stream_ok.sse")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write(fixture)
	}))
	defer upstream.Close()

	executor := customadapter.NewExecutor(upstream.Client(), nil)
	resp, err := executor.Execute(context.Background(), ports.NewExecuteRequest(
		compatibility.NewGenerationRequest(compatibility.GenerationRequestParams{Model: "m", InputText: "hi"}), ports.NewExecutionContract(compatibility.DeliveryModeStreaming),
		ports.NewRoutableTarget("backend-a", "custom", upstream.URL+"/v1", "", "responses", "", ""),
	))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	defer func() { _ = resp.Close() }()

	var (
		startedID string
		text      strings.Builder
		sawText   bool
		sawDone   bool
	)
	for {
		event, err := resp.Stream().Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("stream next returned error: %v", err)
		}
		switch event.Kind {
		case compatibility.OutputEventStarted:
			startedID = event.ResultID
		case compatibility.OutputEventTextDelta:
			text.WriteString(event.TextDelta)
		case compatibility.OutputEventItemStarted:
			if event.ItemKind == compatibility.OutputItemText {
				sawText = true
			}
		case compatibility.OutputEventCompleted:
			if event.FinishReason == "completed" {
				sawDone = true
			}
		}
	}

	expectedID := mustExtractResponsesSSEStartedID(t, fixture)
	if startedID != expectedID {
		t.Fatalf("started id = %q, want %q", startedID, expectedID)
	}
	if !strings.EqualFold(text.String(), "ok") {
		t.Fatalf("stream text = %q, want case-insensitive %q", text.String(), "ok")
	}
	if !sawText || !sawDone {
		t.Fatalf("sawText=%v sawDone=%v, want both true", sawText, sawDone)
	}
}

func TestCustomAdapter_PreservesRealPreviousResponseNotFoundErrorFixture(t *testing.T) {
	t.Parallel()

	fixture := mustReadResponsesContinuityFixture(t, "openai_previous_response_not_found.json")
	tests := []compatibility.DeliveryMode{
		compatibility.DeliveryModeBuffered, compatibility.DeliveryModeStreaming,
	}

	for _, deliveryMode := range tests {
		t.Run(string(deliveryMode), func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write(fixture)
			}))
			defer upstream.Close()

			executor := customadapter.NewExecutor(upstream.Client(), nil)
			_, err := executor.Execute(context.Background(), ports.NewExecuteRequest(
				compatibility.NewGenerationRequest(compatibility.GenerationRequestParams{
					Model:              "m",
					PreviousResponseID: "resp_does_not_exist",
					InputText:          "missing",
				}),
				ports.NewExecutionContract(deliveryMode),
				ports.NewRoutableTarget("backend-a", "custom", upstream.URL+"/v1", "", "responses", "", ""),
			))
			if err == nil {
				t.Fatal("expected error, got nil")
			}

			var backendErr compatibility.BackendError
			if !errors.As(err, &backendErr) {
				t.Fatalf("error type = %T, want compatibility.BackendError", err)
			}
			if got := backendErr.StatusCode; got != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", got, http.StatusBadRequest)
			}
			if !strings.Contains(backendErr.Message, `"code": "previous_response_not_found"`) {
				t.Fatalf("backend message = %q, want previous_response_not_found payload", backendErr.Message)
			}
			if !strings.Contains(backendErr.Message, `"param": "previous_response_id"`) {
				t.Fatalf("backend message = %q, want previous_response_id param", backendErr.Message)
			}
		})
	}
}

func TestCustomAdapter_DecodesRealBufferedResponsesToolCallFixture(t *testing.T) {
	t.Parallel()

	fixture := mustReadResponsesContinuityFixture(t, "openrouter_buffered_tool_call.json")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture)
	}))
	defer upstream.Close()

	executor := customadapter.NewExecutor(upstream.Client(), nil)
	resp, err := executor.Execute(context.Background(), ports.NewExecuteRequest(
		compatibility.NewGenerationRequest(compatibility.GenerationRequestParams{Model: "m", InputText: "hi"}), ports.NewExecutionContract(compatibility.DeliveryModeBuffered),
		ports.NewRoutableTarget("backend-a", "custom", upstream.URL+"/v1", "", "responses", "", ""),
	))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	output, ok := resp.Output().(compatibility.CanonicalOutputValue)
	if !ok {
		t.Fatalf("output type = %T, want compatibility.CanonicalOutputValue", resp.Output())
	}
	items := output.Items()
	if len(items) != 1 {
		t.Fatalf("output items = %d, want 1", len(items))
	}
	if got := items[0].Kind; got != compatibility.ItemKindToolUse {
		t.Fatalf("item kind = %q, want %q", got, compatibility.ItemKindToolUse)
	}
	if got := items[0].Name; got != "grep" {
		t.Fatalf("tool name = %q, want %q", got, "grep")
	}
	if got := items[0].Input["pattern"]; got != "TODO" {
		t.Fatalf("tool pattern = %#v, want %q", got, "TODO")
	}
}

func TestCustomAdapter_DecodesRealStreamingResponsesToolCallFixture(t *testing.T) {
	t.Parallel()

	fixture := mustReadResponsesContinuityFixture(t, "openrouter_stream_tool_call.sse")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write(fixture)
	}))
	defer upstream.Close()

	executor := customadapter.NewExecutor(upstream.Client(), nil)
	resp, err := executor.Execute(context.Background(), ports.NewExecuteRequest(
		compatibility.NewGenerationRequest(compatibility.GenerationRequestParams{Model: "m", InputText: "hi"}), ports.NewExecutionContract(compatibility.DeliveryModeStreaming),
		ports.NewRoutableTarget("backend-a", "custom", upstream.URL+"/v1", "", "responses", "", ""),
	))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	defer func() { _ = resp.Close() }()

	var (
		sawStarted bool
		sawDone    bool
		args       strings.Builder
	)
	for {
		event, err := resp.Stream().Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("stream next returned error: %v", err)
		}
		switch event.Kind {
		case compatibility.OutputEventItemStarted:
			if event.ItemKind == compatibility.OutputItemToolUse {
				sawStarted = true
			}
		case compatibility.OutputEventToolUseArgumentsDelta:
			args.WriteString(event.ArgumentsDelta)
		case compatibility.OutputEventItemCompleted:
			if event.ItemKind == compatibility.OutputItemToolUse {
				sawDone = true
			}
		}
	}
	if !sawStarted || !sawDone {
		t.Fatalf("sawStarted=%v sawDone=%v, want both true", sawStarted, sawDone)
	}
	if got := args.String(); got != "{\"pattern\":\"TODO\"}" {
		t.Fatalf("arguments delta = %q, want %q", got, "{\"pattern\":\"TODO\"}")
	}
}

func mustReadResponsesContinuityFixture(t *testing.T, name string) []byte {
	t.Helper()

	path := filepath.Join("..", "..", "..", "fixtures", "responses_continuity", name)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	return raw
}

func mustExtractResponsesJSONID(t *testing.T, fixture []byte) string {
	t.Helper()
	var body struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(fixture, &body); err != nil {
		t.Fatalf("decode responses json fixture id: %v", err)
	}
	if strings.TrimSpace(body.ID) == "" {
		t.Fatal("responses json fixture missing id")
	}
	return body.ID
}

func mustExtractResponsesSSEStartedID(t *testing.T, fixture []byte) string {
	t.Helper()
	for _, line := range strings.Split(string(fixture), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		var frame struct {
			Type     string `json:"type"`
			Response struct {
				ID string `json:"id"`
			} `json:"response"`
		}
		if err := json.Unmarshal([]byte(payload), &frame); err != nil {
			continue
		}
		if frame.Type == "response.created" && strings.TrimSpace(frame.Response.ID) != "" {
			return frame.Response.ID
		}
	}
	t.Fatal("responses sse fixture missing response.created id")
	return ""
}

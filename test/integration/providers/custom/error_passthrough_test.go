package custom

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	customadapter "github.com/metrofun/swobu/internal/adapters/outbound/providers/custom"
	"github.com/metrofun/swobu/internal/domain/compatibility"
	"github.com/metrofun/swobu/internal/ports"
)

func TestCustomAdapter_PreservesBackendErrorOrigin(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		http.Error(w, `{"error":"rate limited"}`, http.StatusTooManyRequests)
	}))
	defer upstream.Close()

	executor := customadapter.NewExecutor(upstream.Client(), staticCredentialResolver("token-123"))
	_, err := executor.Execute(context.Background(), ports.NewExecuteRequest(
		compatibility.NewDialogRequest("m", []compatibility.CanonicalItem{compatibility.NewTextItem(compatibility.ItemAuthorUser, "hi")}), ports.NewExecutionContract(compatibility.DeliveryModeBuffered),
		ports.NewRoutableTarget("backend-a", "custom", upstream.URL+"/v1", "cred-1", "chat_completions", "", ""),
	))
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var backendErr compatibility.BackendError
	if !errors.As(err, &backendErr) {
		t.Fatalf("expected compatibility.BackendError, got %T", err)
	}
	if backendErr.Origin != compatibility.ErrorOriginBackend {
		t.Fatalf("origin = %q, want %q", backendErr.Origin, compatibility.ErrorOriginBackend)
	}
	if backendErr.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", backendErr.StatusCode, http.StatusTooManyRequests)
	}
	if backendErr.RetryAfterHeaderValue != "30" {
		t.Fatalf("retry-after = %q, want %q", backendErr.RetryAfterHeaderValue, "30")
	}
}

func TestCustomAdapter_RejectsUnsupportedCanonicalSemanticRequest(t *testing.T) {
	t.Parallel()

	executor := customadapter.NewExecutor(http.DefaultClient, nil)
	_, err := executor.Execute(context.Background(), ports.NewExecuteRequest(
		unsupportedRequest{}, ports.NewExecutionContract(compatibility.DeliveryModeBuffered),
		ports.NewRoutableTarget("backend-a", "custom", "https://example.test/v1", "", "chat_completions", "", ""),
	))
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var swobuErr compatibility.Error
	if !errors.As(err, &swobuErr) {
		t.Fatalf("expected compatibility.Error, got %T", err)
	}
	if swobuErr.Code != compatibility.ErrorCodeUnsupportedOperation {
		t.Fatalf("code = %q, want %q", swobuErr.Code, compatibility.ErrorCodeUnsupportedOperation)
	}
}

func TestCustomAdapter_CarriesSuccessfulStreamingResponses(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("" +
			"data: {\"id\":\"chatcmpl_1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n\n" +
			"data: {\"id\":\"chatcmpl_1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n" +
			"data: {\"id\":\"chatcmpl_1\",\"model\":\"m\",\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
			"data: [DONE]\n\n"))
	}))
	defer upstream.Close()

	executor := customadapter.NewExecutor(upstream.Client(), nil)
	resp, err := executor.Execute(context.Background(), ports.NewExecuteRequest(
		compatibility.NewDialogRequest("m", []compatibility.CanonicalItem{compatibility.NewTextItem(compatibility.ItemAuthorUser, "hi")}), ports.NewExecutionContract(compatibility.DeliveryModeStreaming),
		ports.NewRoutableTarget("backend-a", "custom", upstream.URL+"/v1", "", "chat_completions", "", ""),
	))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	defer func() { _ = resp.Close() }()

	if got := resp.DeliveryMode(); got != compatibility.DeliveryModeStreaming {
		t.Fatalf("delivery mode = %q, want %q", got, compatibility.DeliveryModeStreaming)
	}
	event, err := resp.Stream().Next()
	if err != nil {
		t.Fatalf("stream next returned error: %v", err)
	}
	if got := event.Kind; got != compatibility.OutputEventStarted {
		t.Fatalf("event kind = %q, want %q", got, compatibility.OutputEventStarted)
	}
	event, err = resp.Stream().Next()
	if err != nil {
		t.Fatalf("stream next returned error: %v", err)
	}
	if got := event.Kind; got != compatibility.OutputEventItemStarted {
		t.Fatalf("event kind = %q, want %q", got, compatibility.OutputEventItemStarted)
	}
	event, err = resp.Stream().Next()
	if err != nil {
		t.Fatalf("stream next returned error: %v", err)
	}
	if got := event.TextDelta; got != "hello" {
		t.Fatalf("event text = %q, want %q", got, "hello")
	}
}

func TestCustomAdapter_DecodesChatToolCallStreaming(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("" +
			"data: {\"id\":\"chatcmpl_1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n\n" +
			"data: {\"id\":\"chatcmpl_1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"grep\",\"arguments\":\"{\\\"pattern\\\":\"}}]}}]}\n\n" +
			"data: {\"id\":\"chatcmpl_1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"\\\"TODO\\\"}\"}}]}}]}\n\n" +
			"data: {\"id\":\"chatcmpl_1\",\"model\":\"m\",\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n" +
			"data: [DONE]\n\n"))
	}))
	defer upstream.Close()

	executor := customadapter.NewExecutor(upstream.Client(), nil)
	resp, err := executor.Execute(context.Background(), ports.NewExecuteRequest(
		compatibility.NewDialogRequest("m", []compatibility.CanonicalItem{compatibility.NewTextItem(compatibility.ItemAuthorUser, "hi")}), ports.NewExecutionContract(compatibility.DeliveryModeStreaming),
		ports.NewRoutableTarget("backend-a", "custom", upstream.URL+"/v1", "", "chat_completions", "", ""),
	))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	defer func() { _ = resp.Close() }()

	gotKinds := make([]compatibility.OutputEventKind, 0, 5)
	gotArgs := ""
	for {
		event, err := resp.Stream().Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("stream next returned error: %v", err)
		}
		gotKinds = append(gotKinds, event.Kind)
		if event.Kind == compatibility.OutputEventToolUseArgumentsDelta {
			gotArgs += event.ArgumentsDelta
		}
	}
	if len(gotKinds) < 5 {
		t.Fatalf("event count = %d, want >= 5", len(gotKinds))
	}
	if gotKinds[1] != compatibility.OutputEventItemStarted {
		t.Fatalf("second event kind = %q, want %q", gotKinds[1], compatibility.OutputEventItemStarted)
	}
	if gotArgs != "{\"pattern\":\"TODO\"}" {
		t.Fatalf("arguments delta = %q, want %q", gotArgs, "{\"pattern\":\"TODO\"}")
	}
}

func TestCustomAdapter_DecodesResponsesToolCallStreaming(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("" +
			"data: {\"type\":\"response.created\",\"id\":\"resp_1\",\"model\":\"m\"}\n\n" +
			"data: {\"type\":\"response.function_call_arguments.delta\",\"item_id\":\"tool_0\",\"call_id\":\"call_1\",\"name\":\"grep\",\"delta\":\"{\\\"pattern\\\":\"}\n\n" +
			"data: {\"type\":\"response.function_call_arguments.delta\",\"item_id\":\"tool_0\",\"call_id\":\"call_1\",\"name\":\"grep\",\"delta\":\"\\\"TODO\\\"}\"}\n\n" +
			"data: {\"type\":\"response.function_call_arguments.done\",\"item_id\":\"tool_0\",\"call_id\":\"call_1\",\"name\":\"grep\"}\n\n" +
			"data: {\"type\":\"response.completed\",\"status\":\"completed\"}\n\n"))
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

	gotArgs := ""
	sawStart := false
	sawDone := false
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
			sawStart = true
		case compatibility.OutputEventToolUseArgumentsDelta:
			gotArgs += event.ArgumentsDelta
		case compatibility.OutputEventItemCompleted:
			sawDone = true
		}
	}
	if !sawStart || !sawDone {
		t.Fatalf("sawStart=%v sawDone=%v, want both true", sawStart, sawDone)
	}
	if gotArgs != "{\"pattern\":\"TODO\"}" {
		t.Fatalf("arguments delta = %q, want %q", gotArgs, "{\"pattern\":\"TODO\"}")
	}
}

type unsupportedRequest struct{}

func (unsupportedRequest) SemanticKind() compatibility.SemanticKind {
	return "unsupported"
}

func (unsupportedRequest) Clone() compatibility.CanonicalRequest {
	return unsupportedRequest{}
}

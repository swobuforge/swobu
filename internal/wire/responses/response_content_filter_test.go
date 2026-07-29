package responses

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestDecodeResponseBuffered_ContentFilterPreservesTerminalReason(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"id":"resp_1",
		"model":"m",
		"status":"incomplete",
		"incomplete_details":{"reason":"content_filter"},
		"content_filters":[{"source_type":"completion","blocked":true}],
		"output":[]
	}`)

	reader, err := decodeResponseBuffered(context.Background(), canonical.CanonicalRequest{}, raw, "ex_content_filter", &recordingDecisionSink{})
	if err != nil {
		t.Fatalf("decodeResponseBuffered returned error: %v", err)
	}
	closed, err := canonical.ReadClosedEnvelope(context.Background(), canonical.NewBoundResponseIdentityStream(reader, canonical.ResponseBinding{SwobuID: "resp_test"}), canonical.EnvResponse)
	if err != nil {
		t.Fatalf("ReadClosedEnvelope returned error: %v", err)
	}
	out, err := closed.ProjectResponse()
	if err != nil {
		t.Fatalf("ProjectResponse returned error: %v", err)
	}
	if got := out.Completion().Reason(); got != "content_filter" {
		t.Fatalf("finish reason = %q, want content_filter", got)
	}
	if len(out.Items()) != 0 {
		t.Fatalf("items = %#v, want empty for blocked completion", out.Items())
	}
}

func TestDecodeResponseBuffered_PromptContentFilterReturnsBackendError(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"id":"resp_1",
		"model":"m",
		"status":"incomplete",
		"incomplete_details":{"reason":"content_filter"},
		"content_filters":[{"source_type":"prompt","blocked":true}],
		"output":[]
	}`)

	reader, err := decodeResponseBuffered(context.Background(), canonical.CanonicalRequest{}, raw, "ex_prompt_content_filter", &recordingDecisionSink{})
	if err == nil {
		t.Fatal("decodeResponseBuffered returned nil error, want backend error")
	}
	var backendErr canonical.BackendError
	if !errors.As(err, &backendErr) {
		t.Fatalf("decodeResponseBuffered error type = %T, want canonical.BackendError", err)
	}
	if backendErr.StatusCode != http.StatusForbidden {
		t.Fatalf("backend status = %d, want %d", backendErr.StatusCode, http.StatusForbidden)
	}
	if backendErr.TargetID != "responses" {
		t.Fatalf("backend ref = %q, want responses", backendErr.TargetID)
	}
	if got := backendErr.Message; got != "provider input was blocked by content filter" {
		t.Fatalf("backend error message = %q, want provider input block reason", got)
	}
	if reader != nil {
		t.Fatalf("decodeResponseBuffered returned reader %#v, want nil on backend error", reader)
	}
}

func TestDecodeResponseStream_ContentFilterPreservesTerminalReason(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		eventType string
		status    string
	}{
		{name: "completed", eventType: "response.completed", status: "completed"},
		{name: "incomplete", eventType: "response.incomplete", status: "incomplete"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			raw := "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"model\":\"m\",\"status\":\"in_progress\",\"output\":[]}}\n\n" +
				"event: " + tc.eventType + "\ndata: {\"type\":\"" + tc.eventType + "\",\"response\":{\"id\":\"resp_1\",\"model\":\"m\",\"status\":\"" + tc.status + "\",\"incomplete_details\":{\"reason\":\"content_filter\"},\"content_filters\":[{\"source_type\":\"completion\",\"blocked\":true}],\"output\":[]}}\n\n"

			sink := &recordingDecisionSink{}
			reader := decodeResponseStream(canonical.CanonicalRequest{}, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex_stream_content_filter", sink)
			defer func() { _ = reader.Close(context.Background()) }()

			closed, err := canonical.ReadClosedEnvelope(context.Background(), canonical.NewBoundResponseIdentityStream(reader, canonical.ResponseBinding{SwobuID: "resp_test"}), canonical.EnvResponse)
			if err != nil {
				t.Fatalf("ReadClosedEnvelope returned error: %v", err)
			}
			out, err := closed.ProjectResponse()
			if err != nil {
				t.Fatalf("ProjectResponse returned error: %v", err)
			}
			if got := out.Completion().Reason(); got != "content_filter" {
				t.Fatalf("finish reason = %q, want content_filter", got)
			}
			if len(out.Items()) != 0 {
				t.Fatalf("items = %#v, want empty for blocked completion", out.Items())
			}
		})
	}
}

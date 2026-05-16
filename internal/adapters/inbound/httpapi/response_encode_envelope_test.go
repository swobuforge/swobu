package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/ports"
)

func TestWriteSuccessResponse_StreamingFromEnvelope(t *testing.T) {
	out := canonical.NewConversationOutput(
		"resp_env_http_1",
		"m",
		[]canonical.OutputItem{
			canonical.NewTextOutputItem("text_0", "hello"),
		},
		"completed",
	)
	envelope, err := canonical.EventReaderFromCanonicalOutput("ex_http_env", out)
	if err != nil {
		t.Fatalf("EventReaderFromCanonicalOutput error: %v", err)
	}
	resp := ports.NewEnvelopeStreamingProviderResponse(envelope)

	rr := httptest.NewRecorder()
	if err := writeSuccessResponse(rr, "req_test_1", canonical.IngressFamilyResponses, resp, true); err != nil {
		t.Fatalf("writeSuccessResponse error: %v", err)
	}
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "response.completed") {
		t.Fatalf("body missing response.completed frame: %s", body)
	}
	if !strings.Contains(body, "hello") {
		t.Fatalf("body missing streamed text: %s", body)
	}
}

func TestWriteSuccessResponse_StreamingEnvelopePreferredOverLegacyStream(t *testing.T) {
	out := canonical.NewConversationOutput(
		"resp_env_http_2",
		"m",
		[]canonical.OutputItem{
			canonical.NewTextOutputItem("text_0", "truth"),
		},
		"completed",
	)
	envelope, err := canonical.EventReaderFromCanonicalOutput("ex_http_env_2", out)
	if err != nil {
		t.Fatalf("EventReaderFromCanonicalOutput error: %v", err)
	}
	resp := ports.NewEnvelopeStreamingProviderResponse(envelope)

	rr := httptest.NewRecorder()
	if err := writeSuccessResponse(rr, "req_test_2", canonical.IngressFamilyChatCompletions, resp, true); err != nil {
		t.Fatalf("writeSuccessResponse error: %v", err)
	}
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "truth") {
		t.Fatalf("body missing streamed text: %s", body)
	}
}

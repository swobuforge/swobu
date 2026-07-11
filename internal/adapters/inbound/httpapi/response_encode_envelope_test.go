package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/exchange"
	transportpkg "github.com/swobuforge/swobu/internal/transport"
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
	stream, err := testResponseStreamEncoderForFamily(canonical.ClientFamilyResponses).EncodeResponseStream(envelope, delivery.StreamingDelivery(delivery.FramingSSE))
	if err != nil {
		t.Fatalf("EncodeResponseStream error: %v", err)
	}
	resp := exchange.RequestOutput{Response: exchange.NewTransportResponseFromStream(stream)}

	rr := httptest.NewRecorder()
	if err := writeSuccessResponse(rr, "req_test_1", canonical.ClientFamilyResponses, resp); err != nil {
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
	stream, err := testResponseStreamEncoderForFamily(canonical.ClientFamilyChatCompletions).EncodeResponseStream(envelope, delivery.StreamingDelivery(delivery.FramingSSE))
	if err != nil {
		t.Fatalf("EncodeResponseStream error: %v", err)
	}
	resp := exchange.RequestOutput{Response: exchange.NewTransportResponseFromStream(stream)}

	rr := httptest.NewRecorder()
	if err := writeSuccessResponse(rr, "req_test_2", canonical.ClientFamilyChatCompletions, resp); err != nil {
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

func TestWriteSuccessResponse_StreamingReadFailureDoesNotCommitHeaders(t *testing.T) {
	resp := exchange.RequestOutput{
		Response: exchange.TransportResponse{
			Transport: transportpkg.TransportResponse{
				Status: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"text/event-stream"},
				},
				Body: immediateReadErrorBody{},
			},
		},
	}

	writer := &writeHeaderCountingResponseWriter{}
	err := writeSuccessResponse(writer, "req_test_3", canonical.ClientFamilyResponses, resp)
	if err == nil {
		t.Fatal("writeSuccessResponse returned nil, want stream decoding failure")
	}
	if !strings.Contains(err.Error(), "stream decoding failed") {
		t.Fatalf("error = %v, want stream decoding failed", err)
	}
	if writer.writeHeaderCount != 0 {
		t.Fatalf("writeHeader count = %d, want 0", writer.writeHeaderCount)
	}
}

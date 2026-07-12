package httpapi

import (
	"context"
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
	stream, err := testResponseStreamEncoderForFamily(canonical.ClientFamilyResponses).EncodeResponseStream(canonical.NewSliceEventReader(canonical.SynthesizeResponseEnvelopeEvents("ex_http_env", out.ResultID(), out.Model(), out.Items(), out.FinishReason(), out.Usage())), delivery.StreamingDelivery(delivery.FramingSSE))
	if err != nil {
		t.Fatalf("EncodeResponseStream error: %v", err)
	}
	resp := exchange.RequestOutput{Response: exchange.NewTransportResponseFromStream(stream, false)}

	rr := httptest.NewRecorder()
	if err := writeSuccessResponse(context.Background(), rr, "req_test_1", canonical.ClientFamilyResponses, resp); err != nil {
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
	stream, err := testResponseStreamEncoderForFamily(canonical.ClientFamilyChatCompletions).EncodeResponseStream(canonical.NewSliceEventReader(canonical.SynthesizeResponseEnvelopeEvents("ex_http_env_2", out.ResultID(), out.Model(), out.Items(), out.FinishReason(), out.Usage())), delivery.StreamingDelivery(delivery.FramingSSE))
	if err != nil {
		t.Fatalf("EncodeResponseStream error: %v", err)
	}
	resp := exchange.RequestOutput{Response: exchange.NewTransportResponseFromStream(stream, false)}

	rr := httptest.NewRecorder()
	if err := writeSuccessResponse(context.Background(), rr, "req_test_2", canonical.ClientFamilyChatCompletions, resp); err != nil {
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
	err := writeSuccessResponse(context.Background(), writer, "req_test_3", canonical.ClientFamilyResponses, resp)
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

func TestWriteSuccessResponse_StreamingDisconnectAfterCommitIsGraceful(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	resp := exchange.RequestOutput{
		Response: exchange.TransportResponse{
			Transport: transportpkg.TransportResponse{
				Status: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"text/event-stream"},
				},
				Body: &firstChunkThenErrorBody{},
			},
		},
	}

	writer := &writeHeaderCountingResponseWriter{
		cancelAfterWriteCount: 1,
		cancel:                cancel,
	}
	err := writeSuccessResponse(ctx, writer, "req_test_4", canonical.ClientFamilyResponses, resp)
	if err != nil {
		t.Fatalf("writeSuccessResponse returned error: %v", err)
	}
	if writer.writeHeaderCount != 1 {
		t.Fatalf("writeHeader count = %d, want 1", writer.writeHeaderCount)
	}
	if writer.writeCount == 0 {
		t.Fatal("body was not written")
	}
}

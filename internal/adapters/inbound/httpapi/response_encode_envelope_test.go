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
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
	transportpkg "github.com/swobuforge/swobu/internal/transport"
)

func TestWriteSuccessResponse_StreamingFromEnvelope(t *testing.T) {
	out := canonicaltest.Response(t,
		"resp_env_http_1",
		"m",
		[]canonical.CanonicalItem{
			canonicaltest.MustMessage(canonical.MessageRoleAssistant, "hello"),
		},
		canonical.Completed("completed"),
	)
	stream, err := testResponseStreamEncoderForFamily(canonical.ClientFamilyResponses).EncodeResponseStream(context.Background(), canonical.NewSliceEventReader(canonical.SynthesizeResponseEnvelopeEvents("ex_http_env", out.Response(), out.Model(), out.Items(), out.Completion(), out.Usage())), delivery.StreamingDelivery(delivery.FramingSSE))
	if err != nil {
		t.Fatalf("EncodeResponseStream error: %v", err)
	}
	resp := exchange.RequestOutput{Response: exchange.NewStreamingResponse(stream)}

	rr := httptest.NewRecorder()
	if result := writeSuccessResponse(context.Background(), rr, "req_test_1", canonical.ClientFamilyResponses, resp); result.Kind != transportpkg.DeliverySucceeded {
		t.Fatalf("delivery result: %#v", result)
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
	out := canonicaltest.Response(t,
		"resp_env_http_2",
		"m",
		[]canonical.CanonicalItem{
			canonicaltest.MustMessage(canonical.MessageRoleAssistant, "truth"),
		},
		canonical.Completed("completed"),
	)
	stream, err := testResponseStreamEncoderForFamily(canonical.ClientFamilyChatCompletions).EncodeResponseStream(context.Background(), canonical.NewSliceEventReader(canonical.SynthesizeResponseEnvelopeEvents("ex_http_env_2", out.Response(), out.Model(), out.Items(), out.Completion(), out.Usage())), delivery.StreamingDelivery(delivery.FramingSSE))
	if err != nil {
		t.Fatalf("EncodeResponseStream error: %v", err)
	}
	resp := exchange.RequestOutput{Response: exchange.NewStreamingResponse(stream)}

	rr := httptest.NewRecorder()
	if result := writeSuccessResponse(context.Background(), rr, "req_test_2", canonical.ClientFamilyChatCompletions, resp); result.Kind != transportpkg.DeliverySucceeded {
		t.Fatalf("delivery result: %#v", result)
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
		Response: exchange.StreamingResponse{Response: transportpkg.Response{
			Status: http.StatusOK,
			Header: http.Header{
				"Content-Type": []string{"text/event-stream"},
			},
			Body: immediateReadErrorBody{},
		}},
	}

	writer := &writeHeaderCountingResponseWriter{}
	result := writeSuccessResponse(context.Background(), writer, "req_test_3", canonical.ClientFamilyResponses, resp)
	if result.Kind != transportpkg.DeliveryProviderStreamFailed || result.Err == nil {
		t.Fatalf("delivery result = %#v, want provider stream failure", result)
	}
	if !strings.Contains(result.Err.Error(), "stream body failed") {
		t.Fatalf("error = %v, want stream body failure", result.Err)
	}
	if writer.writeHeaderCount != 0 {
		t.Fatalf("writeHeader count = %d, want 0", writer.writeHeaderCount)
	}
}

func TestClassifyDeliveryFailurePreservesCheckpointCommitKind(t *testing.T) {
	result := classifyDeliveryFailure(context.Background(), nil, exchange.CheckpointCommitError{}, nil)
	if result.Kind != transportpkg.DeliveryCheckpointCommitFailed {
		t.Fatalf("delivery kind = %q, want %q", result.Kind, transportpkg.DeliveryCheckpointCommitFailed)
	}
}

func TestWriteSuccessResponse_StreamingDisconnectAfterCommitIsGraceful(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	resp := exchange.RequestOutput{
		Response: exchange.StreamingResponse{Response: transportpkg.Response{
			Status: http.StatusOK,
			Header: http.Header{
				"Content-Type": []string{"text/event-stream"},
			},
			Body: &firstChunkThenErrorBody{},
		}},
	}

	writer := &writeHeaderCountingResponseWriter{
		cancelAfterWriteCount: 1,
		cancel:                cancel,
	}
	result := writeSuccessResponse(ctx, writer, "req_test_4", canonical.ClientFamilyResponses, resp)
	if result.Kind != transportpkg.DeliveryClientCancelled {
		t.Fatalf("delivery result = %#v, want client cancellation", result)
	}
	if writer.writeHeaderCount != 1 {
		t.Fatalf("writeHeader count = %d, want 1", writer.writeHeaderCount)
	}
	if writer.writeCount == 0 {
		t.Fatal("body was not written")
	}
}

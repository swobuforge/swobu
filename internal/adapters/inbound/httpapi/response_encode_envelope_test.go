package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/replay"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
	transportpkg "github.com/swobuforge/swobu/internal/transport"
)

type deliveryReplayStore struct {
	putCalls int
	putErr   error
}

func (*deliveryReplayStore) Get(context.Context, string, canonical.SwobuResponseID) (replay.Record, bool, error) {
	return replay.Record{}, false, nil
}

func (s *deliveryReplayStore) Put(context.Context, string, replay.Record) error {
	s.putCalls++
	return s.putErr
}

func TestWriteSuccessResponse_StreamingFromEnvelope(t *testing.T) {
	out := canonicaltest.Response(t,
		"resp_env_http_1",
		"m",
		[]canonical.CanonicalItem{
			canonicaltest.MustMessage(canonical.MessageRoleAssistant, "hello"),
		},
		"completed",
	)
	stream, err := testResponseStreamEncoderForFamily(canonical.ClientFamilyResponses).EncodeResponseStream(context.Background(), canonical.NewSliceEventReader(canonical.SynthesizeResponseEnvelopeEvents("ex_http_env", out.Response(), out.Model(), out.Items(), out.CompletionReason(), out.Usage())), delivery.StreamingDelivery(delivery.FramingSSE))
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
		"completed",
	)
	stream, err := testResponseStreamEncoderForFamily(canonical.ClientFamilyChatCompletions).EncodeResponseStream(context.Background(), canonical.NewSliceEventReader(canonical.SynthesizeResponseEnvelopeEvents("ex_http_env_2", out.Response(), out.Model(), out.Items(), out.CompletionReason(), out.Usage())), delivery.StreamingDelivery(delivery.FramingSSE))
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

func TestWriteSuccessResponse_ReplayCommitFailureIsNotDeliverySuccess(t *testing.T) {
	store := &deliveryReplayStore{putErr: errors.New("store unavailable")}
	response := canonicaltest.Response(t,
		"provider_response_1",
		"m",
		[]canonical.CanonicalItem{canonicaltest.MustMessage(canonical.MessageRoleAssistant, "hello")},
		"completed",
	)
	var events canonical.ResponseStream = canonical.NewSliceEventReader(canonical.SynthesizeResponseEnvelopeEvents(
		"ex_replay_failure",
		response.Response(),
		response.Model(),
		response.Items(),
		response.CompletionReason(),
		response.Usage(),
	))
	binding := canonical.ResponseBinding{SwobuID: canonical.SwobuResponseID("swobu_replay_failure")}
	events = canonical.NewBoundResponseIdentityStream(events, binding)
	committed := replay.NewCommitReader(events, replay.TerminalCommitConfig{
		WorkspaceSlug:    "alpha",
		ExchangeID:       "ex_replay_failure",
		Binding:          binding,
		Store:            store,
		MaxSemanticBytes: exchange.DefaultRuntimeLimits().MaxReplayBytes,
		SemanticRequest: canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: canonical.Specify("m"),
			Items: []canonical.CanonicalItem{canonicaltest.MustMessage(canonical.MessageRoleAssistant, "hello")},
		}),
	})
	stream, err := testResponseStreamEncoderForFamily(canonical.ClientFamilyResponses).EncodeResponseStream(
		context.Background(), committed, delivery.StreamingDelivery(delivery.FramingSSE),
	)
	if err != nil {
		t.Fatalf("EncodeResponseStream error: %v", err)
	}

	result := writeSuccessResponse(context.Background(), httptest.NewRecorder(), "req_replay_failure", canonical.ClientFamilyResponses, exchange.RequestOutput{
		Response: exchange.NewStreamingResponse(stream),
	})

	if result.Kind != transportpkg.DeliveryReplayCommitFailed || result.Err == nil {
		t.Fatalf("delivery result = %#v, want replay commit failure", result)
	}
	if store.putCalls != 1 {
		t.Fatalf("replay put calls = %d, want 1", store.putCalls)
	}
}

func TestWriteSuccessResponse_ClientCancellationDoesNotCommitReplay(t *testing.T) {
	store := &deliveryReplayStore{}
	response := canonicaltest.Response(t,
		"provider_response_2",
		"m",
		[]canonical.CanonicalItem{canonicaltest.MustMessage(canonical.MessageRoleAssistant, "hello")},
		"completed",
	)
	var events canonical.ResponseStream = canonical.NewSliceEventReader(canonical.SynthesizeResponseEnvelopeEvents(
		"ex_cancel",
		response.Response(),
		response.Model(),
		response.Items(),
		response.CompletionReason(),
		response.Usage(),
	))
	binding := canonical.ResponseBinding{SwobuID: canonical.SwobuResponseID("swobu_cancel")}
	events = canonical.NewBoundResponseIdentityStream(events, binding)
	committed := replay.NewCommitReader(
		events,
		replay.TerminalCommitConfig{
			WorkspaceSlug:    "alpha",
			ExchangeID:       "ex_cancel",
			Binding:          binding,
			Store:            store,
			MaxSemanticBytes: exchange.DefaultRuntimeLimits().MaxReplayBytes,
			SemanticRequest: canonical.NewCanonicalRequest(canonical.RequestParams{
				Model: canonical.Specify("m"),
				Items: []canonical.CanonicalItem{canonicaltest.MustMessage(canonical.MessageRoleAssistant, "hello")},
			}),
		},
	)
	ctx, cancel := context.WithCancel(context.Background())
	stream, err := testResponseStreamEncoderForFamily(canonical.ClientFamilyResponses).EncodeResponseStream(
		ctx, committed, delivery.StreamingDelivery(delivery.FramingSSE),
	)
	if err != nil {
		t.Fatalf("EncodeResponseStream error: %v", err)
	}
	writer := &writeHeaderCountingResponseWriter{cancelAfterWriteCount: 1, cancel: cancel}

	result := writeSuccessResponse(ctx, writer, "req_cancel", canonical.ClientFamilyResponses, exchange.RequestOutput{
		Response: exchange.NewStreamingResponse(stream),
	})

	if result.Kind != transportpkg.DeliveryClientCancelled {
		t.Fatalf("delivery result = %#v, want client cancellation", result)
	}
	if store.putCalls != 0 {
		t.Fatalf("replay put calls = %d, want 0", store.putCalls)
	}
}

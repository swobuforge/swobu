package exchange

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/replay"
	transportpkg "github.com/swobuforge/swobu/internal/transport"
)

// ClientResponse is the sealed client-delivery sum. Its concrete variants
// make neither/both pointer states unrepresentable.
type ClientResponse interface{ isClientResponse() }

type BufferedResponse struct {
	Response transportpkg.Response
}

type StreamingResponse struct {
	Response transportpkg.Response
}

type MessageStreamingResponse struct {
	Response transportpkg.MessageResponse
}

func (BufferedResponse) isClientResponse()         {}
func (StreamingResponse) isClientResponse()        {}
func (MessageStreamingResponse) isClientResponse() {}

func NewBufferedResponse(doc carrier.Document) ClientResponse {
	header := cloneHeader(doc.Header)
	if header.Get("Content-Type") == "" {
		header.Set("Content-Type", "application/json")
	}
	return BufferedResponse{Response: transportpkg.Response{
		Status: http.StatusOK,
		Header: header,
		Body:   io.NopCloser(bytes.NewReader(doc.RawBytes())),
	}}
}

func newBufferedClientResponse(body io.ReadCloser) ClientResponse {
	return BufferedResponse{Response: transportpkg.Response{
		Status: http.StatusOK,
		Header: http.Header{"Content-Type": []string{"application/json"}},
		Body:   body,
	}}
}

func NewStreamingResponse(stream carrier.ByteStream) ClientResponse {
	header := cloneHeader(stream.Header)
	if header.Get("Content-Type") == "" {
		if strings.TrimSpace(stream.MediaType) == "" { // swobu:io-string source=boundary
			header.Set("Content-Type", "application/octet-stream")
		} else {
			header.Set("Content-Type", stream.MediaType)
		}
	}
	body := stream.Body
	if body == nil {
		body = io.NopCloser(bytes.NewReader(nil))
	}
	return StreamingResponse{Response: transportpkg.Response{
		Status: http.StatusOK,
		Header: header,
		Body:   body,
	}}
}

func NewMessageStreamingResponse(stream carrier.MessageResponse) ClientResponse {
	header := cloneHeader(stream.Header)
	if header.Get("Content-Type") == "" {
		header.Set("Content-Type", stream.MediaType)
	}
	return MessageStreamingResponse{Response: transportpkg.MessageResponse{
		Status:   http.StatusOK,
		Header:   header,
		Messages: stream.Messages,
	}}
}

func cloneHeader(in http.Header) http.Header {
	if in == nil {
		return make(http.Header)
	}
	out := make(http.Header, len(in))
	for k, values := range in {
		copied := make([]string, len(values))
		copy(copied, values)
		out[k] = copied
	}
	return out
}

func deliveryIsIncremental(clientDelivery delivery.Delivery, providerDelivery delivery.Delivery) bool {
	return clientDelivery.IsStreaming() && providerDelivery.IsStreaming()
}

// IsReplayCommitFailure classifies the replay gate's terminal error without
// exposing replay implementation details to inbound delivery adapters.
func IsReplayCommitFailure(err error) bool { return replay.IsTerminalCommitFailure(err) }

func commitDecisionsBestEffort(ctx context.Context, sink compat.Sink, exchangeID string, decisions []compat.Decision) {
	if sink == nil || len(decisions) == 0 {
		return
	}
	_ = sink.Commit(ctx, exchangeID, decisions)
}

func deliveryCompatibilityDecisions(call providerCall, incremental bool) []compat.Decision {
	if !call.clientDelivery.IsStreaming() {
		return nil
	}
	decisions := make([]compat.Decision, 0, 4)
	if decision, ok := deliveryStreamingDecision(call, incremental); ok {
		decisions = append(decisions, decision)
	}
	if decision, ok := deliveryIncrementalDecision(call, incremental); ok {
		decisions = append(decisions, decision)
	}
	decisions = append(decisions, deliveryFramingDecisions(call)...)
	return decisions
}

func deliveryIncrementalDecision(call providerCall, incremental bool) (compat.Decision, bool) {
	if !call.clientDelivery.IsStreaming() {
		return compat.Decision{}, false
	}
	outcome := compat.Approx
	if incremental {
		outcome = compat.Exact
	}
	return compat.Decision{
		Feature: compat.DeliveryIncremental,
		Outcome: outcome,
		Subject: routeDecisionSubject(call.backend.Target.ProviderID(), string(call.backend.Target.ProtocolKind)),
	}, true
}

func deliveryFramingDecisions(call providerCall) []compat.Decision {
	if !call.clientDelivery.IsStreaming() {
		return nil
	}
	subject := routeDecisionSubject(call.backend.Target.ProviderID(), string(call.backend.Target.ProtocolKind))
	switch call.clientDelivery.Framing {
	case delivery.FramingSSE:
		outcome := compat.Exact
		if call.request.Delivery.IsStreaming() && call.request.Delivery.Framing == delivery.FramingWebSocket {
			outcome = compat.Approx
		}
		return []compat.Decision{
			{
				Feature: compat.DeliveryServerSentEvents,
				Outcome: outcome,
				Subject: subject,
			},
		}
	case delivery.FramingWebSocket:
		if call.request.Delivery.IsStreaming() && call.request.Delivery.Framing == delivery.FramingSSE {
			return []compat.Decision{
				{
					Feature: compat.DeliveryServerSentEvents,
					Outcome: compat.Approx,
					Subject: subject,
				},
				{
					Feature: compat.DeliveryWebSocket,
					Outcome: compat.Approx,
					Subject: subject,
				},
			}
		}
		return []compat.Decision{
			{
				Feature: compat.DeliveryWebSocket,
				Outcome: compat.Exact,
				Subject: subject,
			},
		}
	default:
		return nil
	}
}

func deliveryStreamingDecision(call providerCall, incremental bool) (compat.Decision, bool) {
	if !call.clientDelivery.IsStreaming() {
		return compat.Decision{}, false
	}
	outcome := compat.Approx
	if incremental {
		outcome = compat.Exact
	}
	return compat.Decision{
		Feature: compat.DeliveryStreaming,
		Outcome: outcome,
		Subject: routeDecisionSubject(call.backend.Target.ProviderID(), string(call.backend.Target.ProtocolKind)),
	}, true
}

// Backend errors become message-only canonical errors before the client
// envelope is written, so record the shape drop on the candidate route here.
func backendErrorShapeDecisions(call providerCall, err error) []compat.Decision {
	var backendErr canonical.BackendError
	if !errors.As(err, &backendErr) {
		return nil
	}
	if strings.TrimSpace(backendErr.Message) == "" { // swobu:io-string source=boundary
		return nil
	}
	subject := routeDecisionSubject(call.backend.Target.ProviderID(), string(call.backend.Target.ProtocolKind))
	if subject == "" {
		return nil
	}
	return []compat.Decision{
		{
			Feature: compat.ErrorShape,
			Outcome: compat.Drop,
			Subject: subject,
		},
	}
}

func routeDecisionSubject(providerID string, protocol string) compat.Subject {
	protocol = strings.TrimSpace(protocol) // swobu:io-string source=boundary
	if providerID == "" || protocol == "" {
		return ""
	}
	return compat.Subject("route:provider/" + providerID + "/protocol/" + protocol)
}

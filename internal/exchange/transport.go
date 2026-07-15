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
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/effect"
	stage "github.com/swobuforge/swobu/internal/exchange/stage"
	transportpkg "github.com/swobuforge/swobu/internal/transport"
)

func NewTransportResponseFromDocument(doc carrier.CarrierDocument) TransportResponse {
	header := cloneHeader(doc.Header)
	if header.Get("Content-Type") == "" {
		header.Set("Content-Type", "application/json")
	}
	return TransportResponse{Transport: transportpkg.TransportResponse{
		Status: http.StatusOK,
		Header: header,
		Body:   io.NopCloser(bytes.NewReader(doc.RawBytes())),
	}}
}

func NewTransportResponseFromStream(stream carrier.CarrierStream, progressive bool) TransportResponse {
	header := cloneHeader(stream.Header)
	if header.Get("Content-Type") == "" {
		switch stream.Framing {
		case carrier.FramingWebSocket:
			header.Set("Content-Type", "application/json")
		default:
			header.Set("Content-Type", "text/event-stream")
		}
	}
	body := carrier.ReadCloserFromFrameReader(stream.Frames)
	if body == nil {
		body = io.NopCloser(bytes.NewReader(nil))
	}
	return TransportResponse{Transport: transportpkg.TransportResponse{
		Status: http.StatusOK,
		Header: header,
		Body:   body,
	}, Progressive: progressive}
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

func toCarrierFraming(in delivery.Framing) carrier.Framing {
	switch in {
	case delivery.FramingSSE:
		return carrier.FramingSSE
	case delivery.FramingWebSocket:
		return carrier.FramingWebSocket
	case delivery.FramingNDJSON:
		return carrier.FramingNDJSON
	default:
		return carrier.FramingNone
	}
}

// streamProgressive keeps source-incremental truth separate from stream-shaped
// batch output when a stage wrapper declares response buffering or streaming loss.
func streamProgressive(clientDelivery delivery.Delivery, providerDelivery delivery.Delivery, applied []stage.AppliedEventStreamWrapper) bool {
	if !clientDelivery.IsStreaming() || !providerDelivery.IsStreaming() {
		return false
	}
	for _, wrapper := range applied {
		if wrapper.Capabilities.BlocksProgressiveStreaming() {
			return false
		}
	}
	return true
}

func applyDocumentPatches(ctx context.Context, mechanics stage.StageMechanics, exchangeID string, stageKey stage.Stage, doc carrier.CarrierDocument, d delivery.Delivery) (Result[carrier.CarrierDocument], error) {
	next, applied, err := mechanics.ApplyDocument(stageKey, stage.Context{
		ExchangeID: exchangeID,
		Carrier:    carrier.KindCarrierDocument,
		Family:     doc.Family,
		Delivery:   d,
	}, doc)
	effects := make([]effect.Effect, 0, len(applied))
	var reject UnsupportedProjectionError
	hasReject := false
	for _, entry := range applied {
		effects = append(effects, entry.Effects...)
		for _, appliedEffect := range entry.Effects {
			compatibility, ok := appliedEffect.(effect.CompatibilityEffect)
			if !ok {
				continue
			}
			if compatibility.Outcome != compat.Reject {
				continue
			}
			if !hasReject {
				reject = unsupportedProjectionErrorForCompatibility(compatibility, entry.ID)
				hasReject = true
			}
		}
	}
	if err != nil {
		return Result[carrier.CarrierDocument]{Effects: effects}, canonical.InternalError("request patch failed")
	}
	if hasReject {
		return Result[carrier.CarrierDocument]{Effects: effects}, reject
	}
	return effect.NewResult(next, effects...), nil
}

func commitEffectsBestEffort(ctx context.Context, sink effect.Sink, exchangeID string, effects []effect.Effect) {
	if sink == nil || len(effects) == 0 {
		return
	}
	_ = sink.Commit(ctx, exchangeID, effects)
}

func collectReaderEffects(reader canonical.EventReader) []effect.Effect {
	type effectReader interface {
		Effects() []effect.Effect
	}
	carrier, ok := reader.(effectReader)
	if !ok {
		return nil
	}
	effects := carrier.Effects()
	if len(effects) == 0 {
		return nil
	}
	return append([]effect.Effect(nil), effects...)
}

func deliveryCompatibilityEffects(in ExchangeInput, progressive bool) []effect.Effect {
	if !in.ClientDelivery.IsStreaming() {
		return nil
	}
	effects := make([]effect.Effect, 0, 4)
	if decision, ok := deliveryStreamingDecision(in, progressive); ok {
		effects = append(effects, decision)
	}
	if decision, ok := deliveryIncrementalDecision(in, progressive); ok {
		effects = append(effects, decision)
	}
	effects = append(effects, deliveryFramingDecisionEffects(in)...)
	return effects
}

func deliveryIncrementalDecision(in ExchangeInput, progressive bool) (effect.Effect, bool) {
	if !in.ClientDelivery.IsStreaming() {
		return nil, false
	}
	outcome := compat.Approx
	if progressive {
		outcome = compat.Exact
	}
	return effect.CompatibilityEffect{
		Feature: compat.DeliveryIncremental,
		Outcome: outcome,
		Subject: routeDecisionSubject(in.Target.ProviderID(), string(in.ProviderProtocol)),
	}, true
}

func deliveryFramingDecisionEffects(in ExchangeInput) []effect.Effect {
	if !in.ClientDelivery.IsStreaming() {
		return nil
	}
	subject := routeDecisionSubject(in.Target.ProviderID(), string(in.ProviderProtocol))
	switch in.ClientDelivery.Framing {
	case delivery.FramingSSE:
		outcome := compat.Exact
		if in.ProviderDelivery.IsStreaming() && in.ProviderDelivery.Framing == delivery.FramingWebSocket {
			outcome = compat.Approx
		}
		return []effect.Effect{
			effect.CompatibilityEffect{
				Feature: compat.DeliveryServerSentEvents,
				Outcome: outcome,
				Subject: subject,
			},
		}
	case delivery.FramingWebSocket:
		if in.ProviderDelivery.IsStreaming() && in.ProviderDelivery.Framing == delivery.FramingSSE {
			return []effect.Effect{
				effect.CompatibilityEffect{
					Feature: compat.DeliveryServerSentEvents,
					Outcome: compat.Approx,
					Subject: subject,
				},
				effect.CompatibilityEffect{
					Feature: compat.DeliveryWebSocket,
					Outcome: compat.Approx,
					Subject: subject,
				},
			}
		}
		return []effect.Effect{
			effect.CompatibilityEffect{
				Feature: compat.DeliveryWebSocket,
				Outcome: compat.Exact,
				Subject: subject,
			},
		}
	default:
		return nil
	}
}

func deliveryStreamingDecision(in ExchangeInput, progressive bool) (effect.Effect, bool) {
	if !in.ClientDelivery.IsStreaming() {
		return nil, false
	}
	outcome := compat.Approx
	if progressive {
		outcome = compat.Exact
	}
	return effect.CompatibilityEffect{
		Feature: compat.DeliveryStreaming,
		Outcome: outcome,
		Subject: routeDecisionSubject(in.Target.ProviderID(), string(in.ProviderProtocol)),
	}, true
}

func nativePayloadEffects(in ExchangeInput, providerDoc carrier.CarrierDocument) []effect.Effect {
	if in.ProviderProtocol != protocolkind.Responses {
		return nil
	}
	if in.Request.Turn().IsZero() {
		return nil
	}
	if providerDoc.IsEmpty() {
		return nil
	}
	return []effect.Effect{
		effect.TurnStateEffect{
			Op:    effect.TurnStateReplay,
			Key:   "turn.request.raw",
			Value: providerDoc.RawBytes(),
		},
		effect.CompatibilityEffect{
			Feature: compat.WireNativePayload,
			Outcome: compat.Exact,
			Subject: compat.Subject("state:turn.request.raw"),
		},
	}
}

// Backend errors become message-only canonical errors before the client
// envelope is written, so record the shape drop on the candidate route here.
func backendErrorShapeEffects(in ExchangeInput, err error) []effect.Effect {
	var backendErr canonical.BackendError
	if !errors.As(err, &backendErr) {
		return nil
	}
	if strings.TrimSpace(backendErr.Message) == "" { // swobu:io-string source=boundary
		return nil
	}
	subject := routeDecisionSubject(in.Target.ProviderID(), string(in.ProviderProtocol))
	if subject == "" {
		return nil
	}
	return []effect.Effect{
		effect.CompatibilityEffect{
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

func unsupportedProjectionErrorForCompatibility(decision effect.CompatibilityEffect, patchID string) UnsupportedProjectionError {
	field := strings.TrimSpace(string(decision.Subject)) // swobu:io-string source=boundary
	if field == "" {
		field = strings.TrimSpace(string(decision.Feature)) // swobu:io-string source=boundary
	}
	reason := strings.TrimSpace(string(decision.Outcome)) // swobu:io-string source=boundary
	if patchID != "" {
		if reason != "" {
			reason += " "
		}
		reason += "(" + patchID + ")"
	}
	if reason == "" {
		reason = "unsupported semantic projection"
	}
	return UnsupportedProjectionError{
		Field:  field,
		Reason: reason,
	}
}

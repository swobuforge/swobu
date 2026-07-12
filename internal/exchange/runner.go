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

// Runner executes one exchange through a single event-first lifecycle.
// It owns runtime codec lookup and wrapper application for the exchange.
type Runner struct {
	Runtime        ExecutionRuntime
	StageMechanics stage.StageMechanics
	EffectSink     effect.Sink
}

// ClientCodec translates client-family wire documents and client-facing responses.
type ClientCodec interface {
	DecodeClientRequest(doc carrier.WireDocument) (Result[ClientRequestDecode], error)
	EncodeResponseDocument(output canonical.CanonicalOutput) (Result[carrier.WireDocument], error)
	EncodeResponseStream(events canonical.EventReader, d delivery.Delivery) (Result[carrier.WireStream], error)
}

// ProviderRequestDocumentEncoder translates canonical requests into provider wire documents.
type ProviderRequestDocumentEncoder interface {
	EncodeProviderRequestDocument(request canonical.CanonicalRequest, d delivery.Delivery, exchangeID string) (Result[carrier.WireDocument], error)
}

// ProviderEnvelopeDecoder translates provider streams into canonical events.
type ProviderEnvelopeDecoder interface {
	DecodeProviderEnvelope(stream carrier.WireStream, exchangeID string) (Result[canonical.EventReader], error)
}

// ProviderDocumentDecoder translates provider documents into canonical events.
type ProviderDocumentDecoder interface {
	DecodeProviderDocument(ctx context.Context, doc carrier.WireDocument, exchangeID string) (Result[canonical.EventReader], error)
}

// ExchangeInput contains the factual inputs for one request/response exchange.
// Runtime lookup and stage mechanics live on Runner, not here.
type ExchangeInput struct {
	ExchangeID       string
	ClientFamily     canonical.ClientFamily
	ClientDelivery   delivery.Delivery
	Request          canonical.CanonicalRequest
	Target           RoutableTarget
	Contract         ExecutionContract
	ProviderProtocol protocolkind.ProtocolKind
	ProviderDelivery delivery.Delivery
}

// TransportResponse contains one client-facing wire result for one exchange run.
type TransportResponse struct {
	Transport transportpkg.TransportResponse
	// Progressive reports whether a streaming response stayed source-incremental
	// after exchange routing and wrapper application.
	Progressive bool
}

// Run executes one exchange using the runner-owned runtime and stage mechanics.
func (r Runner) Run(ctx context.Context, in ExchangeInput) (TransportResponse, error) {
	if r.Runtime == nil {
		return TransportResponse{}, errors.New("exchange runner runtime resolver is required")
	}
	clientCodec := r.Runtime.ClientCodec(in.ClientFamily)
	if clientCodec == nil {
		return TransportResponse{}, canonical.UnsupportedOperation("client family is not implemented")
	}
	envelope, progressive, err := r.runProviderEnvelope(ctx, in)
	if err != nil {
		return TransportResponse{}, err
	}
	return encodeClientOutput(ctx, in, clientCodec, envelope, progressive, r.EffectSink)
}

func (r Runner) runProviderEnvelope(ctx context.Context, in ExchangeInput) (canonical.EventReader, bool, error) {
	effectSink := r.EffectSink
	if effectSink == nil {
		effectSink = effect.NoopSink{}
	}
	pendingEffects := make([]effect.Effect, 0, 8)
	runtime := r.Runtime
	providerDelivery := in.ProviderDelivery
	if runtime == nil {
		return nil, false, errors.New("exchange runner runtime resolver is required")
	}
	requestEncoder := runtime.ProviderRequestDocumentEncoder(in.ProviderProtocol)
	if requestEncoder == nil {
		return nil, false, errors.New("exchange runner provider request encoder is required")
	}
	streamDecoder := runtime.ProviderEnvelopeDecoder(in.ProviderProtocol, providerDelivery)
	documentDecoder := runtime.ProviderDocumentDecoder(in.ProviderProtocol, providerDelivery)
	if providerDelivery.Mode == delivery.Streaming && streamDecoder == nil {
		return nil, false, errors.New("exchange runner provider stream decoder is required for streaming delivery")
	}
	if providerDelivery.Mode == delivery.Buffered && documentDecoder == nil {
		return nil, false, errors.New("exchange runner provider document decoder is required for buffered delivery")
	}
	if strings.TrimSpace(in.Request.Model()) == "" { // swobu:io-string source=domain
		return nil, false, canonical.BadRequest("canonical request is required")
	}
	if err := in.Contract.Validate(); err != nil {
		return nil, false, canonical.BadRequest("execution contract is invalid")
	}

	providerDocResult, err := requestEncoder.EncodeProviderRequestDocument(canonical.CloneCanonicalRequest(in.Request), providerDelivery, in.ExchangeID)
	pendingEffects = append(pendingEffects, providerDocResult.Effects...)
	if err != nil {
		commitEffectsBestEffort(ctx, effectSink, in.ExchangeID, pendingEffects)
		return nil, false, err
	}
	providerDoc := providerDocResult.Value
	providerDocPatchResult, err := applyDocumentPatches(
		ctx,
		r.StageMechanics,
		in.ExchangeID,
		stage.StageRequestDocumentOut,
		providerDoc,
		providerDelivery,
	)
	if err != nil {
		pendingEffects = append(pendingEffects, providerDocPatchResult.Effects...)
		commitEffectsBestEffort(ctx, effectSink, in.ExchangeID, pendingEffects)
		return nil, false, err
	}
	providerDoc = providerDocPatchResult.Value
	pendingEffects = append(pendingEffects, providerDocPatchResult.Effects...)
	pendingEffects = append(pendingEffects, nativePayloadEffects(in, providerDoc)...)
	if strings.TrimSpace(in.Target.ProviderSpec) == "" { // swobu:io-string source=boundary
		return nil, false, canonical.BadEndpoint("provider target is required")
	}
	providerIngress, err := runtime.ResolveProviderIngress(ctx, NewProviderRequest(
		in.ExchangeID,
		in.ClientFamily,
		in.Request,
		providerDoc,
		in.Contract,
		in.Target,
		effect.AccumulatorSink{Effects: &pendingEffects},
	))
	if err != nil {
		pendingEffects = append(pendingEffects, backendErrorShapeEffects(in, err)...)
		commitEffectsBestEffort(ctx, effectSink, in.ExchangeID, pendingEffects)
		return nil, false, err
	}
	if err := ValidateProviderIngress(providerIngress); err != nil {
		commitEffectsBestEffort(ctx, effectSink, in.ExchangeID, pendingEffects)
		return nil, false, canonical.InternalError("provider ingress shape is invalid")
	}

	envelope, decodeEffects, err := r.decodeProviderEnvelope(ctx, in, providerIngress, streamDecoder, documentDecoder)
	pendingEffects = append(pendingEffects, decodeEffects...)
	if err != nil {
		commitEffectsBestEffort(ctx, effectSink, in.ExchangeID, pendingEffects)
		return nil, false, err
	}
	envelope, streamApplied, err := r.StageMechanics.WrapEventStream(stage.StageSemanticEvents, stage.Context{
		ExchangeID: in.ExchangeID,
		Carrier:    carrier.KindCanonicalEventStream,
		Family:     in.ProviderProtocol,
		Delivery:   providerDelivery,
	}, envelope)
	if err != nil {
		for _, applied := range streamApplied {
			pendingEffects = append(pendingEffects, applied.Effects...)
		}
		commitEffectsBestEffort(ctx, effectSink, in.ExchangeID, pendingEffects)
		return nil, false, canonical.InternalError("semantic event wrapper failed")
	}
	progressive := streamProgressive(in.ClientDelivery, providerDelivery, streamApplied)
	for _, applied := range streamApplied {
		pendingEffects = append(pendingEffects, applied.Effects...)
	}
	pendingEffects = append(pendingEffects, deliveryCompatibilityEffects(in, progressive)...)
	if err := effectSink.Commit(ctx, in.ExchangeID, pendingEffects); err != nil {
		return nil, false, canonical.InternalError("effect sink commit failed")
	}
	return envelope, progressive, nil
}

func (r Runner) decodeProviderEnvelope(ctx context.Context, in ExchangeInput, ingress ProviderIngress, streamDecoder ProviderEnvelopeDecoder, documentDecoder ProviderDocumentDecoder) (canonical.EventReader, []effect.Effect, error) {
	switch resolved := ingress.(type) {
	case carrier.CanonicalEventStream:
		if resolved.Events == nil {
			return nil, nil, canonical.InternalError("provider ingress canonical event stream is required")
		}
		return resolved.Events, nil, nil
	case carrier.WireStream:
		if resolved.Frames != nil {
			if in.ProviderDelivery.Mode != delivery.Streaming {
				return nil, nil, canonical.InternalError("provider wire stream requires streaming delivery")
			}
			if streamDecoder == nil {
				return nil, nil, canonical.InternalError("provider wire stream decoder is required")
			}
			result, decodeErr := streamDecoder.DecodeProviderEnvelope(resolved, in.ExchangeID)
			if decodeErr != nil {
				return nil, result.Effects, decodeErr
			}
			return result.Value, result.Effects, nil
		}
		return nil, nil, canonical.InternalError("provider wire stream is required")
	case carrier.WireDocument:
		if resolved.IsEmpty() {
			return nil, nil, canonical.InternalError("provider wire document is required")
		}
		if in.ProviderDelivery.Mode != delivery.Buffered {
			return nil, nil, canonical.InternalError("provider wire document requires buffered delivery")
		}
		patchedDocResult, err := applyDocumentPatches(
			ctx,
			r.StageMechanics,
			in.ExchangeID,
			stage.StageRequestDocumentIn,
			resolved,
			in.ProviderDelivery,
		)
		if err != nil {
			return nil, patchedDocResult.Effects, err
		}
		patchedDoc := patchedDocResult.Value
		effects := patchedDocResult.Effects
		if documentDecoder == nil {
			return nil, effects, canonical.InternalError("provider wire document decoder is required")
		}
		result, decodeErr := documentDecoder.DecodeProviderDocument(ctx, patchedDoc, in.ExchangeID)
		effects = append(effects, result.Effects...)
		if decodeErr != nil {
			return nil, effects, decodeErr
		}
		return result.Value, effects, nil
	default:
		return nil, nil, canonical.InternalError("provider ingress carrier is unsupported")
	}
}

func encodeClientOutput(ctx context.Context, in ExchangeInput, clientCodec ClientCodec, envelope canonical.EventReader, progressive bool, sink effect.Sink) (TransportResponse, error) {
	if in.ClientDelivery.Mode == delivery.Streaming {
		streamResult, err := clientCodec.EncodeResponseStream(envelope, in.ClientDelivery)
		streamResult.Effects = append(streamResult.Effects, collectReaderEffects(envelope)...)
		commitEffectsBestEffort(ctx, sink, in.ExchangeID, streamResult.Effects)
		if err != nil {
			return TransportResponse{}, err
		}
		stream := streamResult.Value
		stream.Framing = toCarrierFraming(in.ClientDelivery.Framing)
		return NewTransportResponseFromStream(stream, progressive), nil
	}
	response, err := projectClientDocument(ctx, envelope)
	commitEffectsBestEffort(ctx, sink, in.ExchangeID, response.Effects)
	if err != nil {
		return TransportResponse{}, err
	}
	bodyDocResult, err := clientCodec.EncodeResponseDocument(response.Value)
	commitEffectsBestEffort(ctx, sink, in.ExchangeID, bodyDocResult.Effects)
	if err != nil {
		return TransportResponse{}, err
	}
	return NewTransportResponseFromDocument(bodyDocResult.Value), nil
}

func NewTransportResponseFromDocument(doc carrier.WireDocument) TransportResponse {
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

func NewTransportResponseFromStream(stream carrier.WireStream, progressive bool) TransportResponse {
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

func applyDocumentPatches(ctx context.Context, mechanics stage.StageMechanics, exchangeID string, stageKey stage.Stage, doc carrier.WireDocument, d delivery.Delivery) (Result[carrier.WireDocument], error) {
	next, applied, err := mechanics.ApplyDocument(stageKey, stage.Context{
		ExchangeID: exchangeID,
		Carrier:    carrier.KindWireDocument,
		Family:     doc.Family,
		Delivery:   d,
	}, doc)
	effects := make([]effect.Effect, 0, len(applied))
	var reject UnsupportedProjectionError
	hasReject := false
	for _, entry := range applied {
		effects = append(effects, entry.Effects...)
		for _, appliedEffect := range entry.Effects {
			compatibility, ok := appliedEffect.(effect.Compatibility)
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
		return Result[carrier.WireDocument]{Effects: effects}, canonical.InternalError("request patch failed")
	}
	if hasReject {
		return Result[carrier.WireDocument]{Effects: effects}, reject
	}
	return NewResult(next, effects...), nil
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
	return effect.Compatibility{
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
			effect.Compatibility{
				Feature: compat.DeliveryServerSentEvents,
				Outcome: outcome,
				Subject: subject,
			},
		}
	case delivery.FramingWebSocket:
		if in.ProviderDelivery.IsStreaming() && in.ProviderDelivery.Framing == delivery.FramingSSE {
			return []effect.Effect{
				effect.Compatibility{
					Feature: compat.DeliveryServerSentEvents,
					Outcome: compat.Approx,
					Subject: subject,
				},
				effect.Compatibility{
					Feature: compat.DeliveryWebSocket,
					Outcome: compat.Approx,
					Subject: subject,
				},
			}
		}
		return []effect.Effect{
			effect.Compatibility{
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
	return effect.Compatibility{
		Feature: compat.DeliveryStreaming,
		Outcome: outcome,
		Subject: routeDecisionSubject(in.Target.ProviderID(), string(in.ProviderProtocol)),
	}, true
}

func nativePayloadEffects(in ExchangeInput, providerDoc carrier.WireDocument) []effect.Effect {
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
		effect.TurnState{
			Op:    effect.TurnStateReplay,
			Key:   "turn.request.raw",
			Value: providerDoc.RawBytes(),
		},
		effect.Compatibility{
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
		effect.Compatibility{
			Feature: compat.ErrorShape,
			Outcome: compat.Drop,
			Subject: subject,
		},
	}
}

func routeDecisionSubject(providerID string, protocol string) compat.Subject {
	providerID = strings.TrimSpace(providerID) // swobu:io-string source=boundary
	protocol = strings.TrimSpace(protocol)     // swobu:io-string source=boundary
	if providerID == "" || protocol == "" {
		return ""
	}
	return compat.Subject("route:provider/" + providerID + "/protocol/" + protocol)
}

func unsupportedProjectionErrorForCompatibility(decision effect.Compatibility, patchID string) UnsupportedProjectionError {
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

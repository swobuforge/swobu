package exchange

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/effect"
	"github.com/swobuforge/swobu/internal/observation"
	"github.com/swobuforge/swobu/internal/transform"
	transportpkg "github.com/swobuforge/swobu/internal/transport"
)

// Runner executes one exchange through a single event-first lifecycle.
// It owns runtime codec lookup and transform application for the exchange.
type Runner struct {
	Runtime    ExecutionRuntime
	Transforms transform.Registry
	EffectSink effect.Sink
}

// ClientCodec translates client-family wire documents and client-facing responses.
type ClientCodec interface {
	DecodeClientRequest(doc carrier.WireDocument) (canonical.CanonicalRequest, delivery.Delivery, error)
	EncodeResponseDocument(output canonical.CanonicalOutput) (carrier.WireDocument, error)
	EncodeResponseStream(events canonical.EventReader, d delivery.Delivery) (carrier.WireStream, error)
}

// ProviderRequestDocumentEncoder translates canonical requests into provider wire documents.
type ProviderRequestDocumentEncoder interface {
	EncodeProviderRequestDocument(request canonical.CanonicalRequest, d delivery.Delivery) (carrier.WireDocument, error)
}

// ProviderEnvelopeDecoder translates provider streams into canonical events.
type ProviderEnvelopeDecoder interface {
	DecodeProviderEnvelope(stream carrier.WireStream, exchangeID string) canonical.EventReader
}

// ProviderDocumentDecoder translates provider documents into canonical events.
type ProviderDocumentDecoder interface {
	DecodeProviderDocument(doc carrier.WireDocument, exchangeID string) (canonical.EventReader, error)
}

// ExchangeInput contains the factual inputs for one request/response exchange.
// Runtime lookup and transform registries live on Runner, not here.
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
	// after exchange routing and middleware application.
	Progressive bool
}

// Run executes one exchange using the runner-owned runtime and transforms.
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
	return encodeClientOutput(ctx, in, clientCodec, envelope, progressive)
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

	providerDoc, err := requestEncoder.EncodeProviderRequestDocument(canonical.CloneCanonicalRequest(in.Request), providerDelivery)
	if err != nil {
		return nil, false, err
	}
	providerDocResult, err := applyDocumentMiddleware(
		ctx,
		r.Transforms,
		in.ExchangeID,
		in.Target.BackendRef,
		in.Target.ProviderID(),
		in.Request.Model(),
		transform.StageRequestDocumentOut,
		providerRequestWireOutPort(),
		providerDoc,
		providerDelivery,
	)
	if err != nil {
		return nil, false, err
	}
	providerDoc = providerDocResult.Value
	pendingEffects = append(pendingEffects, providerDocResult.Effects...)

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
	))
	if err != nil {
		return nil, false, err
	}
	if err := ValidateProviderIngress(providerIngress); err != nil {
		return nil, false, canonical.InternalError("provider ingress shape is invalid")
	}

	envelope, err := r.decodeProviderEnvelope(ctx, in, providerIngress, streamDecoder, documentDecoder)
	if err != nil {
		return nil, false, err
	}
	envelope, streamApplied, err := r.Transforms.WrapEventStream(transform.StageSemanticEvents, transform.Context{
		ExchangeID: in.ExchangeID,
		Carrier:    carrier.KindCanonicalEventStream,
		Family:     in.ProviderProtocol,
		Delivery:   providerDelivery,
	}, envelope)
	if err != nil {
		return nil, false, canonical.InternalError("semantic event transform failed")
	}
	progressive := streamProgressive(in.ClientDelivery, providerDelivery, streamApplied)
	for _, applied := range streamApplied {
		for _, obs := range applied.Observations {
			pendingEffects = append(pendingEffects, effect.ObservationEffect{Observation: observationRecordForExchange(
				in.Target.BackendRef,
				in.Target.ProviderID(),
				in.Request.Model(),
				obs.Code,
				obs.Reason,
			)})
		}
		for _, loss := range applied.Losses {
			pendingEffects = append(pendingEffects, effect.LossEffect{
				Loss: loss,
				Observation: observationRecordForExchange(
					in.Target.BackendRef,
					in.Target.ProviderID(),
					in.Request.Model(),
					string(loss.ReasonCode),
					loss.Reason,
				),
			})
		}
	}
	if err := effectSink.Commit(ctx, in.ExchangeID, pendingEffects); err != nil {
		return nil, false, canonical.InternalError("effect sink commit failed")
	}
	return envelope, progressive, nil
}

func (r Runner) decodeProviderEnvelope(ctx context.Context, in ExchangeInput, ingress ProviderIngress, streamDecoder ProviderEnvelopeDecoder, documentDecoder ProviderDocumentDecoder) (canonical.EventReader, error) {
	switch resolved := ingress.(type) {
	case carrier.CanonicalEventStream:
		if resolved.Events == nil {
			return nil, canonical.InternalError("provider ingress canonical event stream is required")
		}
		return resolved.Events, nil
	case carrier.WireStream:
		if resolved.Frames != nil {
			if in.ProviderDelivery.Mode != delivery.Streaming {
				return nil, canonical.InternalError("provider wire stream requires streaming delivery")
			}
			if streamDecoder == nil {
				return nil, canonical.InternalError("provider wire stream decoder is required")
			}
			return streamDecoder.DecodeProviderEnvelope(resolved, in.ExchangeID), nil
		}
		return nil, canonical.InternalError("provider wire stream is required")
	case carrier.WireDocument:
		if resolved.IsEmpty() {
			return nil, canonical.InternalError("provider wire document is required")
		}
		if in.ProviderDelivery.Mode != delivery.Buffered {
			return nil, canonical.InternalError("provider wire document requires buffered delivery")
		}
		transformedDocResult, err := applyDocumentMiddleware(
			ctx,
			r.Transforms,
			in.ExchangeID,
			in.Target.BackendRef,
			in.Target.ProviderID(),
			in.Request.Model(),
			transform.StageRequestDocumentIn,
			providerResponseWireInPort(),
			resolved,
			in.ProviderDelivery,
		)
		if err != nil {
			return nil, err
		}
		transformedDoc := transformedDocResult.Value
		if documentDecoder == nil {
			return nil, canonical.InternalError("provider wire document decoder is required")
		}
		return documentDecoder.DecodeProviderDocument(transformedDoc, in.ExchangeID)
	default:
		return nil, canonical.InternalError("provider ingress carrier is unsupported")
	}
}

func encodeClientOutput(ctx context.Context, in ExchangeInput, clientCodec ClientCodec, envelope canonical.EventReader, progressive bool) (TransportResponse, error) {
	if in.ClientDelivery.Mode == delivery.Streaming {
		stream, err := clientCodec.EncodeResponseStream(envelope, in.ClientDelivery)
		if err != nil {
			return TransportResponse{}, err
		}
		stream.Framing = toCarrierFraming(in.ClientDelivery.Framing)
		return NewTransportResponseFromStream(stream, progressive), nil
	}
	response, err := projectClientDocument(ctx, envelope)
	if err != nil {
		return TransportResponse{}, err
	}
	bodyDoc, err := clientCodec.EncodeResponseDocument(response.Value)
	if err != nil {
		return TransportResponse{}, err
	}
	return NewTransportResponseFromDocument(bodyDoc), nil
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
	if in == delivery.FramingSSE {
		return carrier.FramingSSE
	}
	return carrier.FramingNone
}

// streamProgressive keeps source-incremental truth separate from stream-shaped
// batch output when middleware declares response buffering or streaming loss.
func streamProgressive(clientDelivery delivery.Delivery, providerDelivery delivery.Delivery, applied []transform.AppliedEventStreamTransform) bool {
	if !clientDelivery.IsStreaming() || !providerDelivery.IsStreaming() {
		return false
	}
	for _, transform := range applied {
		if transform.Capabilities.BlocksProgressiveStreaming() {
			return false
		}
	}
	return true
}

func applyDocumentMiddleware(ctx context.Context, registry transform.Registry, exchangeID string, routeID string, providerID string, modelID string, transformStage transform.Stage, port Port[carrier.WireDocument], doc carrier.WireDocument, d delivery.Delivery) (Result[carrier.WireDocument], error) {
	link := NewLink(
		LinkID(port.ID()),
		port,
		port,
		func(_ context.Context, in carrier.WireDocument) (Result[carrier.WireDocument], error) {
			next, applied, err := registry.ApplyDocument(transformStage, transform.Context{
				ExchangeID: exchangeID,
				Carrier:    carrier.KindWireDocument,
				Family:     in.Family,
				Delivery:   d,
			}, in)
			if err != nil {
				return Result[carrier.WireDocument]{}, canonical.InternalError("request transform failed")
			}
			effects := make([]effect.Effect, 0, len(applied))
			for _, entry := range applied {
				for _, obs := range entry.Observations {
					effects = append(effects, effect.ObservationEffect{Observation: observationRecordForExchange(routeID, providerID, modelID, obs.Code, obs.Reason)})
				}
				if len(entry.Losses) == 0 {
					continue
				}
				loss := entry.Losses[0]
				effects = append(effects, effect.LossEffect{
					Loss:        loss,
					Observation: observationRecordForExchange(routeID, providerID, modelID, string(loss.ReasonCode), loss.Reason),
				})
				field := strings.TrimSpace(loss.Field) // swobu:io-string source=boundary
				if field == "" {
					field = "unknown_field"
				}
				reason := strings.TrimSpace(loss.Reason) // swobu:io-string source=boundary
				if reason == "" {
					reason = "unsupported semantic projection"
				}
				return Result[carrier.WireDocument]{Effects: effects}, UnsupportedProjectionError{Field: field, Reason: fmt.Sprintf("%s (%s)", reason, entry.ID)}
			}
			return NewResult(next, effects...), nil
		},
	)
	return link.Run(ctx, doc)
}

func observationRecordForExchange(routeID string, providerID string, modelID string, code string, reason string) observation.ObservationRecord {
	normalizedModelID := strings.TrimSpace(modelID) // swobu:io-string source=boundary
	return observation.ObservationRecord{
		RouteID:    strings.TrimSpace(routeID), // swobu:io-string source=boundary
		ProviderID: providerID,
		ModelID:    normalizedModelID,
		Code:       strings.TrimSpace(code),   // swobu:io-string source=boundary
		Reason:     strings.TrimSpace(reason), // swobu:io-string source=boundary
	}
}

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
type Runner struct {
	ResolveProviderIngress func(context.Context, ProviderRequest) (ProviderIngress, error)
	EffectSink             effect.Sink
}

type ClientCodec interface {
	DecodeClientRequest(doc carrier.WireDocument) (canonical.CanonicalRequest, delivery.Delivery, error)
	EncodeResponseDocument(output canonical.CanonicalOutput) (carrier.WireDocument, error)
	EncodeResponseStream(events canonical.EventReader, d delivery.Delivery) (carrier.WireStream, error)
}

type ProviderRequestDocumentEncoder interface {
	EncodeProviderRequestDocument(request canonical.CanonicalRequest, d delivery.Delivery) (carrier.WireDocument, error)
}

type ProviderEnvelopeDecoder interface {
	DecodeProviderEnvelope(stream carrier.WireStream, exchangeID string) canonical.EventReader
}

type ProviderDocumentDecoder interface {
	DecodeProviderDocument(doc carrier.WireDocument, exchangeID string) (canonical.EventReader, error)
}

// ExchangeInput contains the full runner input for one request/response exchange.
// TODO god object - architecture smell
type ExchangeInput struct {
	ExchangeID     string
	ClientFamily   canonical.ClientFamily
	ClientDelivery delivery.Delivery
	Request        canonical.CanonicalRequest
	Target         RoutableTarget
	Contract       ExecutionContract

	ProviderProtocol               protocolkind.ProtocolKind
	ProviderDelivery               delivery.Delivery
	ClientCodec                    ClientCodec
	ProviderRequestDocumentEncoder ProviderRequestDocumentEncoder
	ProviderEnvelopeDecoder        ProviderEnvelopeDecoder
	ProviderDocumentDecoder        ProviderDocumentDecoder
	Transforms                     transform.Registry
}

// TransportResponse contains one client-facing wire result for one exchange run.
type TransportResponse struct {
	Transport transportpkg.TransportResponse
}

func (r Runner) Run(ctx context.Context, in ExchangeInput) (TransportResponse, error) {
	if in.ClientCodec == nil {
		return TransportResponse{}, errors.New("exchange runner client codec is required")
	}
	envelope, err := r.runProviderEnvelope(ctx, in)
	if err != nil {
		return TransportResponse{}, err
	}
	return encodeClientOutput(ctx, in, in.ClientCodec, envelope)
}

func (r Runner) runProviderEnvelope(ctx context.Context, in ExchangeInput) (canonical.EventReader, error) {
	effectSink := r.EffectSink
	if effectSink == nil {
		effectSink = effect.NoopSink{}
	}
	pendingEffects := make([]effect.Effect, 0, 8)
	resolveProviderIngress := r.ResolveProviderIngress
	requestEncoder := in.ProviderRequestDocumentEncoder
	streamDecoder := in.ProviderEnvelopeDecoder
	documentDecoder := in.ProviderDocumentDecoder
	providerDelivery := in.ProviderDelivery
	if resolveProviderIngress == nil {
		return nil, errors.New("exchange runner provider ingress resolver is required")
	}
	if requestEncoder == nil {
		return nil, errors.New("exchange runner provider request encoder is required")
	}
	if providerDelivery.Mode == delivery.Streaming && streamDecoder == nil {
		return nil, errors.New("exchange runner provider stream decoder is required for streaming delivery")
	}
	if providerDelivery.Mode == delivery.Buffered && documentDecoder == nil {
		return nil, errors.New("exchange runner provider document decoder is required for buffered delivery")
	}
	if strings.TrimSpace(in.Request.Model()) == "" { // swobu:io-string source=domain
		return nil, canonical.BadRequest("canonical request is required")
	}
	if err := in.Contract.Validate(); err != nil {
		return nil, canonical.BadRequest("execution contract is invalid")
	}

	providerDoc, err := requestEncoder.EncodeProviderRequestDocument(canonical.CloneCanonicalRequest(in.Request), providerDelivery)
	if err != nil {
		return nil, err
	}
	providerCarrier := providerDoc.Clone()
	providerCarrier.Stage = carrier.StageProviderRequestOut
	providerDoc, stageEffects, err := applyDocumentTransformStage(
		in.Transforms,
		in.ExchangeID,
		transform.StageRequestDocumentOut,
		providerCarrier,
		providerDelivery,
	)
	if err != nil {
		return nil, err
	}
	pendingEffects = append(pendingEffects, stageEffects...)

	targetSpec := in.Target.ProviderSpec
	if strings.TrimSpace(targetSpec) == "" { // swobu:io-string source=domain
		return nil, canonical.BadEndpoint("provider target is required")
	}
	providerIngress, err := resolveProviderIngress(ctx, NewProviderRequest(
		in.ExchangeID,
		in.ClientFamily,
		in.Request,
		providerDoc,
		in.Contract,
		in.Target,
	))
	if err != nil {
		return nil, err
	}
	if err := ValidateProviderIngress(providerIngress); err != nil {
		return nil, canonical.InternalError("provider ingress shape is invalid")
	}

	envelope, err := decodeProviderEnvelope(in, providerIngress)
	if err != nil {
		return nil, err
	}
	envelope, streamApplied, err := in.Transforms.WrapEventStream(transform.Context{
		ExchangeID: in.ExchangeID,
		Stage:      transform.StageSemanticEvents,
		Carrier:    carrier.KindCanonicalEventStream,
		Family:     in.ProviderProtocol,
		Delivery:   providerDelivery,
	}, envelope)
	if err != nil {
		return nil, canonical.InternalError("semantic event transform failed")
	}
	for _, applied := range streamApplied {
		for _, obs := range applied.Observations {
			pendingEffects = append(pendingEffects, effect.ObservationEffect{Observation: observation.ObservationRecord{
				RouteID:    "",
				ProviderID: "",
				ModelID:    "",
				Code:       strings.TrimSpace(obs.Code),   // swobu:io-string source=boundary
				Reason:     strings.TrimSpace(obs.Reason), // swobu:io-string source=boundary
			}})
		}
		for _, loss := range applied.Losses {
			pendingEffects = append(pendingEffects, effect.LossEffect{Loss: loss})
		}
	}
	if err := effectSink.Commit(ctx, in.ExchangeID, pendingEffects); err != nil {
		return nil, canonical.InternalError("effect sink commit failed")
	}
	return envelope, nil
}

func decodeProviderEnvelope(in ExchangeInput, ingress ProviderIngress) (canonical.EventReader, error) {
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
			return in.ProviderEnvelopeDecoder.DecodeProviderEnvelope(resolved, in.ExchangeID), nil
		}
		return nil, canonical.InternalError("provider wire stream is required")
	case carrier.WireDocument:
		if resolved.IsEmpty() {
			return nil, canonical.InternalError("provider wire document is required")
		}
		if in.ProviderDelivery.Mode != delivery.Buffered {
			return nil, canonical.InternalError("provider wire document requires buffered delivery")
		}
		transformedDoc, _, err := applyDocumentTransformStage(
			in.Transforms,
			in.ExchangeID,
			transform.StageRequestDocumentIn,
			resolved,
			in.ProviderDelivery,
		)
		if err != nil {
			return nil, err
		}
		return in.ProviderDocumentDecoder.DecodeProviderDocument(transformedDoc, in.ExchangeID)
	default:
		return nil, canonical.InternalError("provider ingress carrier is unsupported")
	}
}

func encodeClientOutput(ctx context.Context, in ExchangeInput, clientCodec ClientCodec, envelope canonical.EventReader) (TransportResponse, error) {
	if in.ClientDelivery.Mode == delivery.Streaming {
		stream, err := clientCodec.EncodeResponseStream(envelope, in.ClientDelivery)
		if err != nil {
			return TransportResponse{}, err
		}
		stream.Stage = carrier.StageClientResponseOut
		stream.Framing = toCarrierFraming(in.ClientDelivery.Framing)
		return NewTransportResponseFromStream(stream), nil
	}
	response, err := projectClientDocument(ctx, envelope)
	if err != nil {
		return TransportResponse{}, err
	}
	bodyDoc, err := clientCodec.EncodeResponseDocument(response)
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

func NewTransportResponseFromStream(stream carrier.WireStream) TransportResponse {
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

func toCarrierFraming(in delivery.Framing) carrier.Framing {
	if in == delivery.FramingSSE {
		return carrier.FramingSSE
	}
	return carrier.FramingNone
}

func applyDocumentTransformStage(registry transform.Registry, exchangeID string, stage transform.Stage, doc carrier.WireDocument, d delivery.Delivery) (carrier.WireDocument, []effect.Effect, error) {
	next, applied, err := registry.ApplyDocument(transform.Context{
		ExchangeID:   exchangeID,
		Stage:        stage,
		CarrierStage: doc.Stage,
		Carrier:      carrier.KindWireDocument,
		Family:       doc.Family,
		Delivery:     d,
	}, doc)
	if err != nil {
		return carrier.WireDocument{}, nil, canonical.InternalError("staged request transform failed")
	}
	effects := make([]effect.Effect, 0, len(applied))
	for _, entry := range applied {
		for _, obs := range entry.Observations {
			effects = append(effects, effect.ObservationEffect{Observation: observation.ObservationRecord{
				RouteID:    "",
				ProviderID: "",
				ModelID:    "",
				Code:       strings.TrimSpace(obs.Code),   // swobu:io-string source=boundary
				Reason:     strings.TrimSpace(obs.Reason), // swobu:io-string source=boundary
			}})
		}
		if len(entry.Losses) == 0 {
			continue
		}
		effects = append(effects, effect.LossEffect{Loss: entry.Losses[0]})
		loss := entry.Losses[0]
		field := strings.TrimSpace(loss.Field) // swobu:io-string source=boundary
		if field == "" {
			field = "unknown_field"
		}
		reason := strings.TrimSpace(loss.Reason) // swobu:io-string source=boundary
		if reason == "" {
			reason = "unsupported semantic projection"
		}
		return carrier.WireDocument{}, effects, UnsupportedProjectionError{Field: field, Reason: fmt.Sprintf("%s (%s)", reason, entry.ID)}
	}
	return next, effects, nil
}

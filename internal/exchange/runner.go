package exchange

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/transform"
)

// Runner executes one exchange through a single event-first lifecycle.
type Runner struct {
	ProviderExecute func(context.Context, ProviderRequest) (ProviderTransportResponse, error)
}

type ClientCodec interface {
	DecodeClientRequest(doc carrier.WireDocument) (canonical.CanonicalRequest, delivery.Delivery, error)
	EncodeClientDocument(output canonical.CanonicalOutput) (carrier.WireDocument, error)
	EncodeClientStream(events canonical.EventReader) (carrier.WireStream, error)
}

type ProviderRequestEncoder interface {
	EncodeProviderRequest(request canonical.CanonicalRequest, d delivery.Delivery) (carrier.WireDocument, error)
}

type ProviderStreamDecoder interface {
	DecodeProviderStream(stream carrier.WireStream, exchangeID string) canonical.EventReader
}

type ProviderDocumentDecoder interface {
	DecodeProviderDocument(doc carrier.WireDocument, exchangeID string) (canonical.EventReader, error)
}

// ClientInput contains the full runner input for one request/response exchange.
type ClientInput struct {
	ExchangeID     string
	ClientFamily   canonical.IngressFamily
	ClientDelivery delivery.Delivery
	Request        canonical.CanonicalRequest
	Target         RoutableTarget
	Contract       ExecutionContract

	ProviderFamily   protocolkind.ProtocolKind
	ProviderDelivery delivery.Delivery
	ClientCodec      ClientCodec
	ProviderEncoder  ProviderRequestEncoder
	StreamDecoder    ProviderStreamDecoder
	DocumentDecoder  ProviderDocumentDecoder
	Transforms       transform.Registry
}

// ClientOutput contains runtime artifacts produced by one exchange run.
type ClientOutput struct {
	Document *carrier.WireDocument
	Stream   *carrier.WireStream
	Envelope canonical.EventReader
}

func (r Runner) Run(ctx context.Context, in ClientInput) (ClientOutput, error) {
	return r.run(ctx, in, true)
}

func (r Runner) RunEnvelope(ctx context.Context, in ClientInput) (ClientOutput, error) {
	return r.run(ctx, in, false)
}

func (r Runner) run(ctx context.Context, in ClientInput, projectClient bool) (ClientOutput, error) {
	if r.ProviderExecute == nil {
		return ClientOutput{}, errors.New("exchange runner provider executor is required")
	}
	if in.ClientCodec == nil {
		return ClientOutput{}, errors.New("exchange runner client codec is required")
	}
	if in.ProviderEncoder == nil {
		return ClientOutput{}, errors.New("exchange runner provider request encoder is required")
	}
	if in.ProviderDelivery.Mode == delivery.Streaming && in.StreamDecoder == nil {
		return ClientOutput{}, errors.New("exchange runner provider stream decoder is required for streaming delivery")
	}
	if in.ProviderDelivery.Mode == delivery.Buffered && in.DocumentDecoder == nil {
		return ClientOutput{}, errors.New("exchange runner provider document decoder is required for buffered delivery")
	}
	if strings.TrimSpace(in.Request.Model()) == "" { // swobu:io-string source=domain
		return ClientOutput{}, canonical.BadRequest("canonical request is required")
	}

	providerDoc, err := in.ProviderEncoder.EncodeProviderRequest(canonical.CloneCanonicalRequest(in.Request), in.ProviderDelivery)
	if err != nil {
		return ClientOutput{}, err
	}
	providerDoc, err = applyDocumentTransformStage(in.Transforms, in.ExchangeID, transform.StageProviderWireOut, carrier.WireDocument{
		Leg:    carrier.LegProviderRequestOut,
		Family: providerDoc.Family,
		Media:  providerDoc.Media,
		Header: providerDoc.Header,
		Raw:    append([]byte(nil), providerDoc.Raw...),
		Meta:   providerDoc.Meta,
	}, in.ProviderDelivery)
	if err != nil {
		return ClientOutput{}, err
	}

	if strings.TrimSpace(in.Target.ProviderID()) == "" { // swobu:io-string source=domain
		return ClientOutput{}, canonical.BadEndpoint("provider target is required")
	}
	providerResp, err := r.ProviderExecute(ctx, NewProviderRequest(
		in.ExchangeID,
		in.ClientFamily,
		in.Request,
		providerDoc,
		in.Contract,
		in.Target,
	))
	if err != nil {
		return ClientOutput{}, err
	}
	if err := providerResp.Validate(); err != nil {
		return ClientOutput{}, canonical.InternalError("provider transport response shape is invalid")
	}

	envelope, err := decodeProviderEnvelope(in, providerResp)
	if err != nil {
		return ClientOutput{}, err
	}
	envelope, _, err = in.Transforms.WrapEventStream(transform.Context{
		ExchangeID: in.ExchangeID,
		Stage:      transform.StageSemanticEvents,
		Carrier:    carrier.KindCanonicalEventStream,
		Family:     in.ProviderFamily,
		Delivery:   in.ProviderDelivery,
	}, envelope)
	if err != nil {
		return ClientOutput{}, canonical.InternalError("semantic event transform failed")
	}

	return encodeClientOutput(ctx, in, in.ClientCodec, envelope, projectClient)
}

func decodeProviderEnvelope(in ClientInput, providerResp ProviderTransportResponse) (canonical.EventReader, error) {
	if providerResp.Envelope != nil {
		return providerResp.Envelope, nil
	}
	if providerResp.Stream != nil {
		if in.ProviderDelivery.Mode != delivery.Streaming {
			return nil, canonical.InternalError("provider transport stream requires streaming delivery")
		}
		envelope := in.StreamDecoder.DecodeProviderStream(carrier.WireStream{
			Leg:     carrier.LegProviderResponseIn,
			Family:  in.ProviderFamily,
			Framing: toCarrierFraming(in.ProviderDelivery.Framing),
			Header:  providerResp.Header,
			Frames:  carrier.FrameReaderFromReadCloser(providerResp.Stream),
		}, in.ExchangeID)
		return envelope, nil
	}
	if in.ProviderDelivery.Mode != delivery.Buffered {
		return nil, canonical.InternalError("provider transport document requires buffered delivery")
	}
	providerDoc := carrier.WireDocument{
		Leg:    carrier.LegProviderResponseIn,
		Family: in.ProviderFamily,
		Media:  "application/json",
		Header: providerResp.Header,
		Raw:    append([]byte(nil), providerResp.Document...),
	}
	transformedDoc, err := applyDocumentTransformStage(
		in.Transforms,
		in.ExchangeID,
		transform.StageProviderWireIn,
		providerDoc,
		in.ProviderDelivery,
	)
	if err != nil {
		return nil, err
	}
	return in.DocumentDecoder.DecodeProviderDocument(transformedDoc, in.ExchangeID)
}

func encodeClientOutput(ctx context.Context, in ClientInput, clientCodec ClientCodec, envelope canonical.EventReader, projectClient bool) (ClientOutput, error) {
	out := ClientOutput{}
	if !projectClient {
		out.Envelope = envelope
		return out, nil
	}
	if in.ClientDelivery.Mode == delivery.Streaming {
		stream, err := clientCodec.EncodeClientStream(envelope)
		if err != nil {
			return ClientOutput{}, err
		}
		stream.Leg = carrier.LegClientResponseOut
		stream.Framing = toCarrierFraming(in.ClientDelivery.Framing)
		out.Stream = &stream
		return out, nil
	}
	response, err := projectClientDocument(ctx, envelope)
	if err != nil {
		return ClientOutput{}, err
	}
	bodyDoc, err := clientCodec.EncodeClientDocument(response)
	if err != nil {
		return ClientOutput{}, err
	}
	out.Document = &bodyDoc
	return out, nil
}

func toCarrierFraming(in delivery.Framing) carrier.Framing {
	if in == delivery.FramingSSE {
		return carrier.FramingSSE
	}
	return carrier.FramingNone
}

func applyDocumentTransformStage(registry transform.Registry, exchangeID string, stage transform.Stage, doc carrier.WireDocument, d delivery.Delivery) (carrier.WireDocument, error) {
	next, applied, err := registry.ApplyDocument(transform.Context{
		ExchangeID: exchangeID,
		Stage:      stage,
		Leg:        doc.Leg,
		Carrier:    carrier.KindWireDocument,
		Family:     doc.Family,
		Delivery:   d,
	}, doc)
	if err != nil {
		return carrier.WireDocument{}, canonical.InternalError("staged request transform failed")
	}
	for _, entry := range applied {
		if len(entry.Losses) == 0 {
			continue
		}
		loss := entry.Losses[0]
		field := strings.TrimSpace(loss.Field) // swobu:io-string source=boundary
		if field == "" {
			field = "unknown_field"
		}
		reason := strings.TrimSpace(loss.Reason) // swobu:io-string source=boundary
		if reason == "" {
			reason = "unsupported semantic projection"
		}
		return carrier.WireDocument{}, UnsupportedProjectionError{Field: field, Reason: fmt.Sprintf("%s (%s)", reason, entry.ID)}
	}
	return next, nil
}

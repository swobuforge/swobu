package exchange

import (
	"context"
	"errors"
	"strings"

	protocolregistry "github.com/swobuforge/swobu/internal/adapters/wire/protocolregistry"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/ports"
	"github.com/swobuforge/swobu/internal/transform"
)

// Runner executes one exchange through canonical stages.
type Runner struct{}

// ClientInput contains the full runner input for one request/response exchange.
type ClientInput struct {
	ExchangeID           string
	ClientFamily         canonical.IngressFamily
	ClientDelivery       delivery.Delivery
	ClientRequestRaw     []byte
	Request              canonical.CanonicalRequest
	Target               ports.RoutableTarget
	Contract             ports.ExecutionContract
	SkipClientProjection bool

	ProviderFamily   protocolkind.ProtocolKind
	ProviderDelivery delivery.Delivery
	ProviderExecute  func(context.Context, ports.ProviderRequest) (ports.ProviderTransportResponse, error)
	Transforms       transform.Registry
}

// ClientOutput contains runtime artifacts produced by one exchange run.
type ClientOutput struct {
	Document *carrier.WireDocument
	Stream   *carrier.WireStream
	Envelope canonical.EventReader
	Report   ExchangeReport
}

// Run executes the runner-controlled request path. Stages are reported from
// executed steps only; no synthetic fallback reports are created.
func (Runner) Run(ctx context.Context, in ClientInput) (ClientOutput, error) {
	if in.ProviderExecute == nil {
		return ClientOutput{}, errors.New("exchange runner provider executor is required")
	}
	clientCodec, err := protocolregistry.ForClientFamily(in.ClientFamily)
	if err != nil {
		return ClientOutput{}, err
	}
	stages := make([]StageReport, 0, 10)
	losses := make([]ProjectionLoss, 0)
	notices := make([]Notice, 0)
	stages = append(stages, StageReport{Stage: string(StageClientHTTPIn)})
	stages = append(stages, StageReport{Stage: string(StageClientWireIn)})

	request, err := resolveCanonicalRequest(in, clientCodec)
	if err != nil {
		return ClientOutput{}, err
	}
	stages = append(stages, StageReport{Stage: string(StageSemanticRequest)})

	providerEncoder, err := protocolregistry.ForProviderRequestProtocolCarrier(in.ProviderFamily)
	if err != nil {
		return ClientOutput{}, err
	}
	providerDoc, err := providerEncoder.EncodeProviderRequest(request, toPortsDelivery(in.ProviderDelivery))
	if err != nil {
		return ClientOutput{}, err
	}
	providerDoc, _, transformStageReports, transformNotices, err := transform.ApplyDocumentStage(
		transform.StageProviderWireOut,
		carrier.WireDocument{
			Leg:    carrier.LegProviderRequestOut,
			Family: providerDoc.Family,
			Media:  providerDoc.Media,
			Header: providerDoc.Header,
			Raw:    append([]byte(nil), providerDoc.Raw...),
			Meta:   providerDoc.Meta,
		},
		in.Transforms,
	)
	if err != nil {
		return ClientOutput{}, err
	}
	_ = providerDoc
	for _, stageReport := range transformStageReports {
		stages = append(stages, stageReport)
	}
	for _, notice := range transformNotices {
		notices = append(notices, Notice(notice))
	}
	for _, stageReport := range transformStageReports {
		if stageReport.Mutated {
			continue
		}
	}
	stages = append(stages, StageReport{Stage: string(StageProviderHTTPOut)})

	if strings.TrimSpace(in.Target.ProviderID()) == "" { // swobu:io-string source=domain
		return ClientOutput{}, canonical.BadEndpoint("provider target is required")
	}
	providerResp, err := in.ProviderExecute(ctx, ports.NewProviderRequest(request, in.Contract, in.Target))
	if err != nil {
		return ClientOutput{}, err
	}
	if err := providerResp.Validate(); err != nil {
		return ClientOutput{}, canonical.InternalError("provider transport response shape is invalid")
	}
	stages = append(stages, StageReport{Stage: string(StageProviderHTTPIn)})
	envelope, providerWireInStage, err := decodeProviderEnvelope(in, providerResp)
	if err != nil {
		return ClientOutput{}, err
	}
	stages = append(stages, providerWireInStage)
	envelope, eventApplied, err := in.Transforms.WrapEventStream(transform.Context{
		ExchangeID: in.ExchangeID,
		Stage:      transform.StageSemanticEvents,
		Carrier:    carrier.KindCanonicalEventStream,
		Family:     in.ProviderFamily,
		Delivery:   in.ProviderDelivery,
	}, envelope)
	if err != nil {
		return ClientOutput{}, canonical.InternalError("semantic event transform failed")
	}
	semanticEventsReport := StageReport{
		Stage:   string(StageSemanticEvents),
		Carrier: string(carrier.KindCanonicalEventStream),
	}
	if len(eventApplied) > 0 {
		semanticEventsReport.Applied = make([]string, 0, len(eventApplied))
		for _, applied := range eventApplied {
			semanticEventsReport.Applied = append(semanticEventsReport.Applied, applied.ID)
			semanticEventsReport.Mutated = semanticEventsReport.Mutated || applied.Mutated
		}
	}
	stages = append(stages, semanticEventsReport)

	out, err := encodeClientOutput(ctx, in, clientCodec, envelope)
	if err != nil {
		return ClientOutput{}, err
	}
	if !in.SkipClientProjection {
		stages = append(stages, StageReport{Stage: string(StageClientWireOut)})
		stages = append(stages, StageReport{Stage: string(StageClientHTTPOut)})
	}

	out.Report = ExchangeReport{
		ExchangeID: in.ExchangeID,
		Stages:     stages,
		Losses:     losses,
		Notices:    notices,
		Evidence:   []Evidence{},
	}
	return out, nil
}

func resolveCanonicalRequest(in ClientInput, clientCodec protocolregistry.ClientFamily) (canonical.CanonicalRequest, error) {
	if strings.TrimSpace(in.Request.Model()) != "" { // swobu:io-string source=domain
		return canonical.CloneCanonicalRequest(in.Request), nil
	}
	request, _, err := clientCodec.DecodeClientRequest(carrier.WireDocument{
		Leg:    carrier.LegClientRequestIn,
		Family: protocolkind.ProtocolKind(in.ClientFamily),
		Media:  "application/json",
		Raw:    append([]byte(nil), in.ClientRequestRaw...),
	})
	if err != nil {
		return canonical.CanonicalRequest{}, err
	}
	return request, nil
}

func decodeProviderEnvelope(in ClientInput, providerResp ports.ProviderTransportResponse) (canonical.EventReader, StageReport, error) {
	if providerResp.Envelope != nil {
		return providerResp.Envelope, StageReport{Stage: string(StageProviderWireIn), Carrier: string(carrier.KindCanonicalEventStream)}, nil
	}
	if providerResp.Stream != nil {
		if in.ProviderDelivery.Mode != delivery.Streaming {
			return nil, StageReport{}, canonical.InternalError("provider transport stream requires streaming delivery")
		}
		streamDecoder, err := protocolregistry.ForProviderResponseStreamProtocolCarrier(in.ProviderFamily)
		if err != nil {
			return nil, StageReport{}, err
		}
		envelope := streamDecoder.DecodeProviderStream(carrier.WireStream{
			Leg:     carrier.LegProviderResponseIn,
			Family:  in.ProviderFamily,
			Framing: toCarrierFraming(in.ProviderDelivery.Framing),
			Header:  providerResp.Header,
			Body:    providerResp.Stream,
		}, in.ExchangeID)
		return envelope, StageReport{Stage: string(StageProviderWireIn), Carrier: string(carrier.KindWireStream)}, nil
	}
	if in.ProviderDelivery.Mode != delivery.Buffered {
		return nil, StageReport{}, canonical.InternalError("provider transport document requires buffered delivery")
	}
	docDecoder, err := protocolregistry.ForProviderResponseDocumentProtocolCarrierEnvelope(in.ProviderFamily)
	if err != nil {
		return nil, StageReport{}, err
	}
	envelope, err := docDecoder.DecodeProviderDocument(carrier.WireDocument{
		Leg:    carrier.LegProviderResponseIn,
		Family: in.ProviderFamily,
		Media:  "application/json",
		Header: providerResp.Header,
		Raw:    append([]byte(nil), providerResp.Document...),
	}, in.ExchangeID)
	if err != nil {
		return nil, StageReport{}, err
	}
	return envelope, StageReport{Stage: string(StageProviderWireIn), Carrier: string(carrier.KindWireDocument)}, nil
}

func encodeClientOutput(ctx context.Context, in ClientInput, clientCodec protocolregistry.ClientFamily, envelope canonical.EventReader) (ClientOutput, error) {
	out := ClientOutput{}
	if in.SkipClientProjection {
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

func toPortsDelivery(in delivery.Delivery) delivery.Delivery {
	if in.Mode == delivery.Streaming {
		return delivery.StreamingDelivery(delivery.FramingSSE)
	}
	return delivery.BufferedDelivery()
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

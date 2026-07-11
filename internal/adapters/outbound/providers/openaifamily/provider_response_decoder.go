package openaifamily

import (
	protocolregistry "github.com/swobuforge/swobu/internal/adapters/wire/protocolregistry"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/ports"
	"github.com/swobuforge/swobu/internal/report"
	"github.com/swobuforge/swobu/internal/transform"
)

func decodeBufferedCarrierDocument(profile ProviderRoutePolicy, doc carrier.WireDocument) (ports.ProviderResponseStream, []report.Notice, []report.Mutation, error) {
	envelope, err := decodeInteractionEnvelopeFromCarrier(doc, "provider_buffered:"+string(doc.Family))
	if err != nil {
		return ports.ProviderResponseStream{}, nil, nil, err
	}
	return ports.NewEnvelopeStreamingProviderResponseStream(envelope), nil, nil, nil
}

func decodeInteractionEnvelopeFromCarrier(packet carrier.WireDocument, exchangeID string) (canonical.EventReader, error) {
	codec, err := protocolregistry.ForProviderResponseDocumentProtocolCarrierEnvelope(packet.Family)
	if err != nil {
		if packet.Family == protocolkind.Messages {
			return nil, canonical.UnsupportedOperation("OpenAI-family provider does not implement the messages protocol")
		}
		return nil, err
	}
	return codec.DecodeProviderDocument(packet, exchangeID)
}

func decodeStreamCarrierFrame(profile ProviderRoutePolicy, stream carrier.WireStream, exchangeID string) (ports.ProviderResponseStream, []report.Notice, []report.StageReport, error) {
	codec, err := protocolregistry.ForProviderResponseStreamProtocolCarrier(stream.Family)
	if err != nil {
		if stream.Family == protocolkind.Messages {
			return ports.ProviderResponseStream{}, nil, nil, canonical.UnsupportedOperation("OpenAI-family provider does not implement the messages protocol")
		}
		return ports.ProviderResponseStream{}, nil, nil, err
	}
	reader := codec.DecodeProviderStream(stream, exchangeID)
	normalizedReader := newUsageEventReader(reader, RawUsageEnvelope{
		ProviderID: profile.ProviderID(),
		Protocol:   stream.Family,
		Headers:    stream.Header,
	}, profile.UsageDecoder())
	registry := newTransformRegistry(profile.Facts(canonical.CanonicalRequest{}))
	wrappedReader, applied, err := registry.WrapEventStream(transform.Context{
		Stage:   transform.StageSemanticEvents,
		Leg:     carrier.LegProviderResponseIn,
		Carrier: carrier.KindCanonicalEventStream,
		Family:  stream.Family,
	}, normalizedReader)
	if err != nil {
		return ports.ProviderResponseStream{}, nil, nil, canonical.InternalError("semantic event transform failed")
	}
	var stageReports []report.StageReport
	if len(applied) > 0 {
		ids := make([]string, 0, len(applied))
		mutated := false
		for _, entry := range applied {
			ids = append(ids, entry.ID)
			mutated = mutated || entry.Mutated
		}
		stageReports = []report.StageReport{{
			Stage:   string(transform.StageSemanticEvents),
			Carrier: string(carrier.KindCanonicalEventStream),
			Applied: ids,
			Mutated: mutated,
		}}
	}
	return ports.NewEnvelopeStreamingProviderResponseStream(wrappedReader), nil, stageReports, nil
}

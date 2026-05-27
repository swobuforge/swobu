package openaifamily

import (
	"io"
	"net/http"

	core "github.com/swobuforge/swobu/internal/adapters/wire/primitives"
	protocolregistry "github.com/swobuforge/swobu/internal/adapters/wire/protocolregistry"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/ports"
)

func decodeBufferedByKind(profile ProviderRoutePolicy, kind protocolkind.ProtocolKind, raw []byte, headers http.Header) (canonical.CanonicalOutputValue, []ports.DegradationWarning, error) {
	packet := core.WirePacket{
		Protocol: kind,
		Headers:  headers,
		RawBody:  raw,
	}
	for _, patch := range profile.DecodePatches() {
		if patch == nil {
			continue
		}
		if err := patch.ApplyDecode(&packet); err != nil {
			return canonical.CanonicalOutputValue{}, nil, canonical.InternalError("provider decode patch failed")
		}
	}
	output, err := decodeInteractionFromWire(packet)
	if err != nil {
		return canonical.CanonicalOutputValue{}, nil, err
	}
	usage, warnings := profile.UsageDecoder().DecodeToCanonical(RawUsageEnvelope{
		ProviderID: profile.ProviderID(),
		Protocol:   packet.Protocol,
		Body:       packet.RawBody,
		Headers:    packet.Headers,
	}, output.Usage())
	return canonical.NewOutputWithUsage(
		output.SemanticKind(),
		output.ResultID(),
		output.Model(),
		output.Items(),
		output.FinishReason(),
		usage,
	), warnings, nil
}

func decodeInteractionFromWire(packet core.WirePacket) (canonical.CanonicalOutputValue, error) {
	codec, err := protocolregistry.ForProtocolKind(packet.Protocol)
	if err != nil {
		if packet.Protocol == protocolkind.Messages {
			return canonical.CanonicalOutputValue{}, canonical.UnsupportedOperation("OpenAI-family provider does not implement the messages protocol")
		}
		return canonical.CanonicalOutputValue{}, err
	}
	return codec.DecodeResponse(packet.RawBody)
}

func decodeStreamByKind(profile ProviderRoutePolicy, kind protocolkind.ProtocolKind, body io.ReadCloser, exchangeID string, headers http.Header) (ports.ProviderResponse, []ports.DegradationWarning, error) {
	codec, err := protocolregistry.ForProtocolKind(kind)
	if err != nil {
		if kind == protocolkind.Messages {
			return ports.ProviderResponse{}, nil, canonical.UnsupportedOperation("OpenAI-family provider does not implement the messages protocol")
		}
		return ports.ProviderResponse{}, nil, err
	}
	reader := codec.DecodeResponseStream(body, exchangeID)
	normalizedReader := newUsageEventReader(reader, RawUsageEnvelope{
		ProviderID: profile.ProviderID(),
		Protocol:   kind,
		Headers:    headers,
	}, profile.UsageDecoder())
	return ports.NewEnvelopeStreamingProviderResponse(normalizedReader), nil, nil
}

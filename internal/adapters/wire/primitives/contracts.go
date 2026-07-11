package core

import (
	"fmt"
	"io"
	"net/http"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

// FramingKind names the transport framing shape for stream carriers.
type FramingKind string

const (
	FramingSSE       FramingKind = "sse"
	FramingWebSocket FramingKind = "websocket"
	FramingNDJSON    FramingKind = "ndjson"
	FramingRaw       FramingKind = "raw"
)

// WireDocument is the protocol-neutral JSON document carrier between transport
// and wire adapters.
type WireDocument struct {
	Kind     WireKind
	Protocol protocolkind.ProtocolKind
	Method   string
	Path     string
	Query    map[string][]string
	Headers  http.Header
	Payload  map[string]any
	RawBody  []byte
}

// WireStream carries framed protocol-native stream data for one leg.
type WireStream struct {
	Kind     WireKind
	Protocol protocolkind.ProtocolKind
	Method   string
	Path     string
	Headers  http.Header
	Body     io.ReadCloser
	Framing  FramingKind
}

func ValidateResponseSSEWireStream(wire WireStream, expectedProtocol protocolkind.ProtocolKind) error {
	if wire.Kind != WireKindResponseStream {
		return fmt.Errorf("wire stream kind must be %q", WireKindResponseStream)
	}
	if wire.Protocol != expectedProtocol {
		return fmt.Errorf("wire stream protocol must be %q", expectedProtocol)
	}
	if wire.Framing != FramingSSE {
		return fmt.Errorf("wire stream framing must be %q", FramingSSE)
	}
	if wire.Body == nil {
		return fmt.Errorf("wire stream body must be configured")
	}
	return nil
}

func ValidateResponseSSECarrierStream(stream carrier.WireStream, expectedProtocol protocolkind.ProtocolKind) error {
	if stream.Family != expectedProtocol {
		return fmt.Errorf("wire stream protocol must be %q", expectedProtocol)
	}
	if stream.Framing != carrier.FramingSSE {
		return fmt.Errorf("wire stream framing must be %q", carrier.FramingSSE)
	}
	if stream.Body == nil {
		return fmt.Errorf("wire stream body must be configured")
	}
	return nil
}

// WireAdapter maps between wire packet and one canonical type.
type WireAdapter[T any] interface {
	DecodeToCanonical(packet WireDocument) (T, error)
	EncodeFromCanonical(value T, packet *WireDocument) error
}

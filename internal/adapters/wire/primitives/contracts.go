package core

import (
	"net/http"

	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

// WirePacket is the low-level protocol-neutral carrier between transport and
// wire adapters.
type WirePacket struct {
	Kind     WireKind
	Protocol protocolkind.ProtocolKind
	Method   string
	Path     string
	Query    map[string][]string
	Headers  http.Header
	Payload  map[string]any
	RawBody  []byte
	Stream   bool
}

// WireAdapter maps between wire packet and one canonical type.
type WireAdapter[T any] interface {
	DecodeToCanonical(packet WirePacket) (T, error)
	EncodeFromCanonical(value T, packet *WirePacket) error
}

// WirePatch is an escape hatch for provider-specific wire mutations that must
// happen before/after generic protocol parsing.
type WirePatch interface {
	ApplyEncode(packet *WirePacket) error
	ApplyDecode(packet *WirePacket) error
}

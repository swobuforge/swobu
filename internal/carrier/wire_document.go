package carrier

import (
	"net/http"

	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

// WireDocument is one protocol-family document carrier on a wire leg.
type WireDocument struct {
	Leg    Leg
	Family protocolkind.ProtocolKind
	Media  string
	Header http.Header
	Raw    []byte
	Meta   Meta
}

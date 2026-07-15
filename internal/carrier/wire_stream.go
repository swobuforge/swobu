package carrier

import (
	"net/http"

	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

// Framing identifies the boundary framing shape for streamed carriers.
type Framing string

const (
	FramingNone      Framing = ""
	FramingSSE       Framing = "sse"
	FramingWebSocket Framing = "websocket"
	FramingNDJSON    Framing = "ndjson"
)

// CarrierStream is one framed stream carrier on a wire leg.
type CarrierStream struct {
	Stage   Stage
	Family  protocolkind.ProtocolKind
	Framing Framing
	Header  http.Header
	Frames  FrameReader
	Meta    Meta
}

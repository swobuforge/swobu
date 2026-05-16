package providercatalog

import (
	"strings"

	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

const (
	FrameHTTPJSONBody = "http_json_body"
	FrameSSEEvent     = "sse_event"
)

func SupportedFramesForSpecProtocol(spec string, protocolKind protocolkind.ProtocolKind) []string {
	if !SupportsExecutionProtocolForSpec(spec, protocolKind) {
		return nil
	}
	// Swobu v0 supports one batch frame and one response-stream frame.
	return []string{FrameHTTPJSONBody, FrameSSEEvent}
}

func SupportsFrameForSpecProtocol(spec string, protocolKind protocolkind.ProtocolKind, frame string) bool {
	frame = strings.TrimSpace(frame) // trimlowerlint:allow domain canonicalization
	if frame == "" {
		return false
	}
	for _, supported := range SupportedFramesForSpecProtocol(spec, protocolKind) {
		if supported == frame {
			return true
		}
	}
	return false
}

func DefaultFrameForSpecProtocol(spec string, protocolKind protocolkind.ProtocolKind) (string, bool) {
	supported := SupportedFramesForSpecProtocol(spec, protocolKind)
	if len(supported) == 0 {
		return "", false
	}
	return supported[0], true
}

func StreamingForFrame(frame string) (bool, bool) {
	switch strings.TrimSpace(frame) { // trimlowerlint:allow domain canonicalization
	case FrameHTTPJSONBody:
		return false, true
	case FrameSSEEvent:
		return true, true
	default:
		return false, false
	}
}

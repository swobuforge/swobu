package profile

import (
	"strings"
)

const (
	FrameHTTPJSONBody = "http_json_body"
	FrameSSEEvent     = "sse_event"
)

func StreamingForFrame(frame string) (bool, bool) {
	trimmed := strings.TrimSpace(frame) // swobu:io-string source=domain
	if trimmed == FrameHTTPJSONBody {
		return false, true
	}
	if trimmed == FrameSSEEvent {
		return true, true
	}
	return false, false
}

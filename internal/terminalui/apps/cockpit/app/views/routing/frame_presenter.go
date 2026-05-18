package routing

import (
	"strings"

	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/domain/providercatalog"
)

const providerDeliveryRowLabel = "delivery"

func presentDeliveryFrame(frame string) string {
	trimmed := strings.TrimSpace(frame) // swobu:io-string source=boundary
	if trimmed == providercatalog.FrameSSEEvent {
		return "streaming"
	}
	if trimmed == providercatalog.FrameHTTPJSONBody {
		return "non-streaming"
	}
	return trimmed
}

func presentDeliveryFrameForProvider(spec string, protocolKind protocolkind.ProtocolKind, frame string) string {
	label := presentDeliveryFrame(frame)
	frame = strings.TrimSpace(frame) // swobu:io-string source=boundary
	if frame == "" {
		return "auto"
	}
	return label
}

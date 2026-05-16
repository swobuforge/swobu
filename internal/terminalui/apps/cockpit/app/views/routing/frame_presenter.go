package routing

import (
	"fmt"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/domain/providercatalog"
)

const providerDeliveryRowLabel = "delivery"

func presentDeliveryFrame(frame string) string {
	switch strings.TrimSpace(frame) { // trimlowerlint:allow boundary canonicalization
	case providercatalog.FrameSSEEvent:
		return "streaming (SSE)"
	case providercatalog.FrameHTTPJSONBody:
		return "request-response (HTTP JSON)"
	default:
		return strings.TrimSpace(frame) // trimlowerlint:allow boundary canonicalization
	}
}

func presentDeliveryFrameForProvider(spec string, protocolKind protocolkind.ProtocolKind, frame string) string {
	label := presentDeliveryFrame(frame)
	frame = strings.TrimSpace(frame) // trimlowerlint:allow boundary canonicalization
	if frame == "" {
		return label
	}
	def, ok := providercatalog.DefaultFrameForSpecProtocol(spec, protocolKind)
	if ok && strings.TrimSpace(def) == frame { // trimlowerlint:allow boundary canonicalization
		return fmt.Sprintf("%s (default)", label)
	}
	return label
}

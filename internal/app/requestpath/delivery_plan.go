package requestpath

import (
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/providercatalog"
	"github.com/swobuforge/swobu/internal/ports"
)

// planProviderCallMode centralizes delivery planning for one route decision.
// Requestpath must orchestrate conversion plans, not reject convertible modes.
func planProviderCallMode(clientMode ports.ResponseMode, target ports.RoutableTarget) (ports.ResponseMode, error) {
	providerMode := clientMode
	if target.SelectedFrame != "" {
		streaming, ok := providercatalog.StreamingForFrame(target.SelectedFrame)
		if !ok {
			return ports.ResponseModeBuffered, canonical.BadEndpoint("selected provider frame is unsupported")
		}
		providerMode = ports.ResponseModeFromStreaming(streaming)
	}
	// ChatGPT codex execute path is stream-native. Buffered clients still work
	// through stream->batch collection at the client encoder boundary.
	if target.ProviderID() == string(providercatalog.ProviderSpecChatGPT) {
		providerMode = ports.ResponseModeStreaming
	}
	return providerMode, nil
}


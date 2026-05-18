package requestpath

import (
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/providercatalog"
	"github.com/swobuforge/swobu/internal/ports"
)

// providerCallModePolicy centralizes delivery planning for one route decision.
// Requestpath must orchestrate conversion plans, not reject convertible modes.
func providerCallModePolicy(clientMode ports.ResponseMode, target ports.RoutableTarget) (ports.ResponseMode, error) {
	providerMode := clientMode
	if target.SelectedFrame != "" {
		if _, ok := providercatalog.StreamingForFrame(target.SelectedFrame); !ok {
			return ports.ResponseModeBuffered, canonical.BadEndpoint("selected provider frame is unsupported")
		}
	}
	return providerMode, nil
}

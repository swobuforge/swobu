package exchange

import (
	"strings"

	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/replay"
)

// replayTargetKey builds the exact backend key used for replay lookup.
// Empty credential refs are treated as unsafe for native replay and return nil
// so the attempt falls back to semantic materialization only.
func replayTargetKey(target RoutableTarget, protocol protocolkind.ProtocolKind, modelID string) *replay.TargetKey {
	authScope := strings.TrimSpace(target.CredentialRef)
	if authScope == "" {
		return nil
	}
	key := replay.TargetKey{
		ProviderSpec:     target.ProviderSpec,
		Protocol:         protocol,
		ProviderProtocol: target.ProviderProtocol,
		BaseURL:          target.BaseURL,
		AuthScope:        authScope,
		ModelID:          modelID,
	}
	return &key
}

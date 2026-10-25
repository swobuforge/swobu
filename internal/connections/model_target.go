package connections

import (
	"errors"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/routing"
)

var (
	ErrUnknownProtocol     = errors.New("provider protocol is unknown")
	ErrUnsupportedProtocol = errors.New("protocol is unsupported for provider")
)

// ValidateModelTarget joins a target-local selection with its exact reusable
// upstream. Run under the publication lock for authoring, and against a frozen
// registry value for execution. Protocol alone never establishes a provider.
func ValidateModelTarget(target routing.Target, value Connection) error {
	if target.ConnectionID() != value.ID() || value.ID().String() == "" {
		return ErrNotFound
	}
	spec, ok := value.Spec().(ModelSpec)
	if !ok {
		return ErrReferenceKind
	}
	if !profile.RoutingConstructionFacts().ProtocolSupported(spec.Upstream().Provider(), target.Protocol().String()) {
		if !profile.IsKnownProviderProtocol(target.Protocol().String()) {
			return ErrUnknownProtocol
		}
		return ErrUnsupportedProtocol
	}
	return nil
}

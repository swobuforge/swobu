package chatcompletions

import (
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/wire"
)

// decodeChatCallableKey resolves a generic function spelling through the one
// attempt-scoped source map; the recovered key determines its lifecycle.
func decodeChatCallableKey(names wire.ToolNames, environment canonical.ToolEnvironment, name string) (canonical.ToolKey, error) {
	return wire.DecodeCallableKey(names, environment, name)
}

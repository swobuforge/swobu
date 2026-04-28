// Package update owns the generic state-update and effect description types
// used by the Elm-style app loop.
package update

import (
	"context"
)

// Action is one semantic application update emitted by event handlers,
// lifecycle hooks, or effect results.  It is reduced against the app model.
type Action any

// Effect describes one outbound side effect (HTTP call, clipboard write,
// process launch, etc.).  The runtime executes Effects asynchronously and
// feeds the result back as Actions.
type Effect interface {
	Execute(ctx context.Context) []Action
}

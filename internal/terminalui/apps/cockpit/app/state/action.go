package state

import "context"

// Action is the cockpit event union.  All actions, whether user intent,
// effect results, or lifecycle signals, satisfy this interface.  The zero
// value is unused; concrete action structs carry data.
type Action any

// EffectOnce declares a present-tense side effect that the runtime must
// execute.  The concrete return type is any (the same underlying type as
// Action) so that effect structs defined in sub-packages do not need to
// import the state package and create an import cycle.
type EffectOnce = interface {
	Run(ctx context.Context) any
}

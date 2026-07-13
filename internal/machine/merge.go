package machine

// Mergeable allows a state type to define how multiple reducer outputs of
// the same type are merged after a single event dispatch.
//
// If a state type implements Mergeable but the merge fails (e.g. conflicting
// mutations), the machine loop returns the error and stops.
//
// If a state type does NOT implement Mergeable and multiple reducers produce
// it, the machine loop returns an error and stops.
type Mergeable[T any] interface {
	Merge(other T) (T, error)
}

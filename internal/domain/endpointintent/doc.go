// Package endpointintent owns durable operator-declared execution intent.
//
// It defines endpoint identity, ordered provider-config declarations, and the
// explicit selected provider-config invariant. Provider-spec catalog truth
// lives in domain/providercatalog and is consumed here for route validation.
// Request-time execution state and runtime evidence do not belong here.
package endpointintent

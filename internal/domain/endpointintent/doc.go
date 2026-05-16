// Package endpointintent owns durable operator-declared execution intent.
//
// It defines endpoint identity, ordered provider-config declarations, and the
// explicit selected provider-config invariant. Provider-spec catalog truth
// lives in domain/providercatalog and is consumed here for provider and auth
// validation. Canonical protocol-family vocabulary is owned by
// domain/protocolkind and referenced from provider configuration here.
//
// Request-time protocol realization and runtime evidence do not belong here.
package endpointintent

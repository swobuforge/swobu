// Package compat owns non-exact semantic changes returned by concrete
// lowerings. Exactness is an empty effective trace. Unsupported lowering is a
// typed failure consumed by the reducer, never a successful change.
//
// Changes travel with transformed values and reducer attempts. Only ingress
// and winning-path changes enter the terminal summary. They never become
// provider declarations, routing policy, support prediction, or observer-owned
// truth.
// A change records what one concrete lowering did. It never answers whether an
// exact target supports the semantic, how a provider spells it, or which
// recovery Exchange should attempt.
//
// A polyfill is a concrete reducer-integrated execution mechanism, such as the
// request-scoped MCP runtime. Its execution fact classifies the terminal result
// without inventing a codec change kind or capability registry.
package compat

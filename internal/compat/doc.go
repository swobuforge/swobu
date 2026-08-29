// Package compat records semantic loss produced by projection. It is
// append-only evidence and has no authority over projection, execution,
// routing, or recovery. Exactness is an empty effective trace.
//
// Changes travel with transformed values and reducer attempts. Only ingress
// and winning-path changes enter the terminal summary. They never become
// provider declarations, routing policy, support prediction, or observer-owned
// truth.
// A change records what one concrete lowering did. It never answers whether an
// exact target supports the semantic, how a provider spells it, or which
// recovery Exchange should attempt.
//
// Execution mechanisms such as request-scoped MCP or image materialization are
// exact when they preserve meaning. Their execution evidence stays outside
// compatibility; only a named semantic approximation or omission belongs here.
package compat

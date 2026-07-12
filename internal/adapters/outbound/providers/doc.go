// Package providers owns outbound provider adapter composition and dispatch.
// It selects one provider-owned adapter bundle per configured provider id at
// the composition edge and exposes one explicit composition root to the rest
// of the runtime.
package providers

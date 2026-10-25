// Package connectioncheck composes bounded Models Connection observations from
// provider-owned catalog discovery. It owns check-only transport policy, exact
// Connection credential scope and sanitized evidence; it does not infer model
// compatibility, retain catalogs or mutate routing or credentials.
package connectioncheck

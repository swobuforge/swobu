// Package credentials owns credential resolution adapters.
//
// Resolver implementations translate operator-selected credential references
// into provider tokens at the provider execution edge through one explicit
// source-resolver seam.
//
// Materialized secret persistence supports write policy modes:
// `keyring`, `file`, and `auto` (keyring then file fallback). Auto returns the
// actual resulting reference kind. Once created, `secret:` resolves only
// through the keyring and `secretfile:` resolves only through the file store;
// reference authority never falls through to another backend.
package credentials

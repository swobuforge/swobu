// Package credentials owns credential resolution adapters.
//
// Resolver implementations translate operator-selected credential references
// into provider tokens at the provider execution edge through one explicit
// source-resolver seam.
//
// Materialized secret persistence supports write policy modes:
// `keyring`, `file`, and `auto` (keyring then file fallback).
package credentials

// Package compat owns descriptive decisions emitted by concrete representation
// seams. Swobu has no compatibility behavior mode: each seam has one safe
// best-effort lowering. Decisions are post-operation evidence only; they are
// never provider declarations, routing inputs, session state, or predictions
// of future support.
package compat

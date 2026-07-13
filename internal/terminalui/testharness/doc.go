// Package testharness owns terminalui test-only adapters for rendering a
// retained ViewSpec into a screen buffer.
//
// Visual assert contract:
//   - Prefer testscreen/assert (screenassert) predicates for semantic invariants.
//   - Use AssertVisual for stable, user-visible composition contracts that cannot be
//     stated cleanly as semantic predicates.
//   - Fixture path convention: testdata/<testfile>__<testname>/fixture/<assertname>.txt
//   - Promote fixtures with SWOBU_UPDATE_WIREFRAMES=1 go test ./...
package testharness

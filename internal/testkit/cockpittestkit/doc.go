// Package testkit provides deterministic rendering and fixture-backed assertion
// for go-tui Cockpit components.
//
// RenderMountedString and RenderMountedScreen mount components before rendering;
// RenderString accepts an already-built element tree.
// AssertVisual delegates fixture comparison to testscreen/fixture.
// MockAppHarness uses framework internals and does not exercise a real terminal.
// ScreenFromBuffer canonicalizes declarative styles to effective terminal
// rendition before they enter the shared Screen model, canonicalizes
// continuation cells, and rejects combining content that the shared producer
// contract cannot preserve.
package testkit

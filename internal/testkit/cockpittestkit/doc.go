// Package testkit provides deterministic rendering and fixture-backed assertion
// for go-tui Cockpit components.
//
// RenderMountedString and RenderMountedBuffer mount components before rendering;
// RenderString and RenderBuffer accept already-built element trees.
// AssertVisual delegates fixture comparison to testscreen/fixture.
// MockAppHarness uses framework internals and does not exercise a real terminal.
package testkit

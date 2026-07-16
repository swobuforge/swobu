// Part of testscreen family: surface=cockpit
//
// Package testkit provides deterministic rendering and fixture-backed assertion
// for go-tui Cockpit components.
//
// Canonical lane:
//   - fixture/unit proof in `swobucli/opencore`
//   - RenderMountedString(component, width, height) → string: deterministic
//     mounted output for component fixture comparison.
//   - RenderMountedBuffer(component, width, height) → buf.View: mounted output
//     for testscreen/assert spatial predicates (Text, TextRE, LeftOf, Below,
//     etc.).
//   - RenderString/RenderBuffer remain available only for already-built inert
//     element trees; Cockpit tests must not call component.Render(nil).
//   - AssertVisual(name).Fixture(...).Normalize(...).Viewport(...).Now(t, snapshot)
//     uses the same visual fixture config shape as PTY/e2e assertions and
//     delegates comparison to testscreen/fixture.CompareSnapshot.
//   - AssertFocusedFrame(frame, want) / AssertUnfocusedFrame(frame, want)
//     capture the visible focus marker contract when the test already has the
//     frame string.
//   - AssertFocusVisible(t, harness, step, want) is the shared contract for
//     proving that a focusable interaction changes the frame and renders the
//     expected visible marker.
//
// Non-canonical helper surface:
//   - MockAppHarness is a quarantined debug harness that seeds go-tui App
//     internals with reflect/unsafe.
//   - It exists for local package probes and harness drift tests, not as the
//     canonical Cockpit integration lane.
//   - Cockpit root/page/section temporal proof now belongs in
//     `swobucli/test/integration/ui`, where mounted production roots are driven
//     under the integration lane contract.
//   - PTY/e2e remains the shipped-binary/runtime lane.
package testkit

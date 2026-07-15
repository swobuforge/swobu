// Part of testscreen family: surface=cockpit
//
// Package testkit provides deterministic rendering and fixture-backed assertion
// for go-tui Cockpit components. It is the canonical proof lane for layout
// fidelity before feature logic is added.
//
// What works (component render):
//   - RenderString(element, width, height) → string: deterministic output for
//     fixture comparison.
//   - RenderBuffer(element, width, height) → buf.View: for testscreen/assert
//     spatial predicates (Text, TextRE, LeftOf, Below, etc.).
//   - AssertVisual(name).Fixture(...).Normalize(...).Viewport(...).Now(t, snapshot)
//     uses the same visual fixture config shape as PTY/e2e assertions and
//     delegates comparison to testscreen/fixture.CompareSnapshot.
//
// What is deferred (app-loop limitation):
//   - Full App lifecycle (event loop, keyboard dispatch, focus navigation) is
//     NOT testable without a real TTY because NewApp/NewAppWithReader create a
//     real ANSITerminal and attempt EnterRawMode, which fails without a
//     controlling terminal. The upstream module provides MockTerminal but no
//     public AppOption to inject it (go-tui v0.17.0). App-loop behavior
//     (focus, keymap, state updates) is therefore a PTY/e2e lane, not a
//     component-render lane.
//   - This is an intentional boundary: component-render catches layout drift;
//     PTY/e2e catches interaction drift. Do not fake the app loop.
package testkit

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
//     approximated by seeded MockAppHarness, a testkit-only harness that mutates
//     go-tui App internals with reflect and unsafe so cockpit app-loop behavior
//     can be proven without a real PTY.
//   - That harness is intentionally coupled to upstream App internals and must
//     stay quarantined to this package. Loud drift tests in testkit must fail if
//     go-tui changes the private App fields or mount shape the harness seeds.
//     Use PTY/e2e only for binary-runtime claims the seeded mock app cannot
//     prove.
//   - This is an intentional boundary: component-render catches layout drift;
//     MockAppHarness catches cockpit interaction drift; PTY/e2e catches shipped
//     binary/runtime drift. Do not describe the harness as the full upstream
//     app loop.
package testkit

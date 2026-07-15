# Testscreen Family Architecture

## Goal

Every testing surface (unit, integration, e2e/PTY, HTTP, etc.) uses the **same predicate grammar** from `testscreen/assert`. Each surface has its own `testkit` package that provides two things:

1. **Predicate instantiation** — thin re-exports of `testscreen/assert` constructors
2. **Execution hooks** — surface-specific adapter that pulls `*testing.T` or `*PtySession` or `Require` in, so assertion failures surface through that mechanism

## Architecture

```
testscreen/assert          — kernel: Expr, Predicate, Text, TextRE, EvalNow, EvalEventually
testscreen/tempo           — kernel: Eventually
testscreen/buf             — kernel: View
testscreen/fixture         — kernel: Config, Path, ConfigFor, CompareSnapshot
testscreen/testpath        — kernel: TestID, Token

cockpit/testkit            — go-tui surface: renders components and asserts predicates/fixtures via t.Fatalf
framework/pty/testkit      — PTY/e2e surface: drives terminal sessions and asserts predicates/fixtures through plans
http/testkit               — (future) HTTP surface: Parses response bodies, asserts via t.Fatalf
```

## UI Test Capability Contract

| Dimension | Shared owner | Cockpit component surface | PTY/e2e surface | Invalid state to exclude |
|---|---|---|---|---|
| Semantic predicate | `testscreen/assert` | `testkit.AssertNow(t, rendered, pred)` | `ConditionOf(pred)` inside `Ensure` / `Do(...).Until(...)` | ad-hoc `strings.Contains` as the primary assertion |
| Structural/spatial predicate | `testscreen/assert` + `testscreen/buf` | `RenderBuffer` + `AssertNowView` | `SnapshotView` through predicate evaluation | duplicate relation grammar outside `testscreen/assert` |
| Visual fixture | `testscreen/fixture` + `testscreen/testpath` | `AssertVisual(name).Fixture(...).Normalize(...).Viewport(...).Now(t, snapshot)` | `AssertVisual(name).Fixture(...).Normalize(...).Viewport(...).Now()` / `.Eventually(timeout)` | surface-local fixture type, update env, path grammar, or compare implementation |
| Temporal wait | `testscreen/tempo` for raw polling; PTY testkit owns quiescent-frame waits | component render is one-shot only | `Ensure(...).Eventually(timeout)` and visual `.Eventually(timeout)` wait for settled snapshots | using sleep as proof or choosing non-quiescent waits for live UI |
| Input/action | PTY runtime adapter owns terminal input vocabulary | not available without a real app loop under pinned go-tui | `Key`, `Type`, `Do`, `DoNow`, `Any` | invented in-process go-tui app harness or duplicated key maps |
| Terminal lifecycle/snapshot | PTY runtime adapter owns process and terminal emulator state | not owned by component tests | `termsession.StartWithSize`, `Snapshot`, `SnapshotView`, `Shutdown` | component tests spawning PTYs or PTY tests bypassing terminal emulator state |
| Trace/artifact | `screentrace` owns deterministic artifact names and writes | package-local failures use `testing.TB`; no step trace | PTY runner writes before/after/meta/failure/final and assertion artifacts | ad-hoc artifact names or unsafe artifact paths |

Component render tests stop at deterministic element rendering, semantic/spatial
predicates over rendered output, and visual fixture comparison. Key dispatch,
focus movement, input editing, process lifecycle, and settled-frame behavior are
PTY/e2e fidelity claims until go-tui exposes a real app-loop test harness.

## Surface Adaptation Rules

### Rule 1: Re-export, Don't Re-implement

Every surface testkit re-exports:
- `type Expr = screenassert.Expr`
- `type Predicate = screenassert.Predicate`
- `Text`, `TextRE`, `All`, `Not`, `Box`, `Within`
- Chain methods (`.Exists()`, `.Below()`, `.LeftOf()`, etc.) come from the kernel, never redeclared

Cargo test-level proof: grep for `type Expr interface` or `func Text` in any testkit must return only kernel symbols.

### Rule 2: Execution Hooks Are Surface-Specific

| Surface | Instantiated by | Failure mechanism |
|---|---|---|
| cockpit | `testkit.AssertNow(t, rendered, pred)` / `testkit.AssertVisual(name)` | `testing.TB.Fatalf` |
| PTY/e2e | `testkit.ConditionOf(pred)` / `testkit.AssertVisual(name)` | plan failure through `Result.Must(t)` plus trace artifacts |

### Rule 3: Surface-Specific Helpers Live In That Testkit

Helpers that are unique to the surface are fine, but they must be named to the surface:
- `RenderString` — cockpit-only (go-tui component → string)
- `RenderBuffer` — cockpit-only (go-tui component → buf.View)
- `AssertVisual` — surface adapter over `testscreen/fixture.CompareSnapshot`
- `Runner.Run` — PTY testkit-specific because it drives input and settled snapshots

Visual helpers that are conceptually shared across surfaces must share method
names and config shape: `AssertVisual(name).Fixture(path).Normalize(fn).Viewport(cols, rows)`.
Cockpit terminates that chain with `Now(t, snapshot)` or `Compare(snapshot)`;
PTY/e2e terminates it with `Now()` or `Eventually(timeout)` because it must
be embedded as a plan step.

### Rule 4: No Kernel Duplication

- No custom tokenizers in any testkit (use testscreen/testpath)
- No custom `Eventually` implementations (use testscreen/tempo)
- No custom predicate implementations (compose from screenassert kernel)
- No custom visual fixture promotion env or compare implementation outside
  `testscreen/fixture`
- No custom trace artifact naming outside the PTY trace owner
- No duplicated terminal key maps outside the PTY runtime adapter

## Current State

- `cockpit/testkit` — ✅ aligned to this architecture
- `framework/pty/testkit` — ✅ aligned package name and visual assertion API;
  old retained-UI scenarios may still use it, but the legacy
  `framework/terminaldriver` path must not return.
- `http/testkit` — ❌ does not exist yet

## Migration Path

When a new testing surface needs visual assertions:
1. Create `SURFACE/testkit/testkit.go` with re-exports and execution hooks
2. Name the surface-specific helpers explicitly (don't name them generically like `Assert`)
3. Add a family-membership comment: `// Part of testscreen family: surface=SURFACE`
4. Use `testscreen/fixture.Config`, `testscreen/fixture.Path`, and
   `testscreen/fixture.CompareSnapshot`; do not create a surface-local fixture
   type or update env.
5. If the surface needs an existing legacy helper, rename the package at the
   migration boundary instead of adding an alias package.

## Enforcement

Run `check-ui-test-plane` after changing UI test framework packages. It rejects
known drift paths such as duplicate visual fixture env vars, legacy fixture
trees, local visual compare implementations, fake go-tui harness APIs, and
active Cockpit e2e imports of the legacy `terminaldriver` package.

# terminalui residue architecture

`internal/terminalui` is not the Swobu TUI architecture anymore.

The active interactive cockpit lives in `internal/cockpit` and is built
directly on `github.com/grindlemire/go-tui`. New cockpit work must start from:

- `docs/04-design/tui-design-system.md`
- `docs/04-design/go-tui-canons.md`
- `swobucli/opencore/internal/cockpit/doc.go`
- `.agents/skills/swobu-go-tui-authoring/SKILL.md`

## Remaining Purpose

This subtree is retained only for noninteractive startup/session residue:

| Package | Status |
|---|---|
| `apps/cli` | CLI startup presenter |
| `session` | session/mode plumbing still imported by CLI/logging |
| `transcript` | line-oriented transcript primitives for startup output |
| `engine/output` | transcript output renderer |
| `engine/reconcile` | transcript reconciliation |
| `view/layout` and `view/textmetrics` | transcript layout support |

No package here owns interactive cockpit state, focus, keys, forms, route
editing, or operator workflow semantics.

## Deleted Interactive Surface

These retained-framework packages are gone and must not be reintroduced:

- `apps/cockpit`
- `core`
- `component`
- `components/*`
- `toolkit`
- `engine/retained/*`
- `view/retained`
- `testharness`

`swobucli/tools/cmd/check-tui-system` enforces this boundary in the opencore
lint bundle.

## Rules

- Do not add new imports of `internal/terminalui` from `internal/cockpit`.
- Do not import `internal/cockpit` from this subtree.
- Do not add new interactive UI features here.
- Do not build wrapper APIs or compatibility bridges from this subtree into
  go-tui.
- If startup output needs more behavior, either keep the change transcript-only
  and local to this subtree or frame a deletion/migration slice that moves it
  out of `terminalui`.

## Deletion Direction

The desired end state is no `internal/terminalui` package family. Until then,
changes here must either:

- preserve the existing startup transcript/session behavior, or
- delete/migrate a retained residue package with proof that CLI startup,
  logging/session mode, and opencore tests still pass.

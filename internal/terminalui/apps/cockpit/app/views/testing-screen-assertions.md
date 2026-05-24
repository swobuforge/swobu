# Screen Test Lanes (Intent-First)

Write tests from scenario intent, not from existing helper mechanics.

## 1) Declarative screen asserts (default)

Use `testscreen/assert` predicates for semantic contracts:
- section presence/order grammar
- disclosure open/closed states
- focused row/choice invariants
- critical copy/state labels

Prefer this lane when the assertion should survive harmless spacing/layout refactors.

## 2) Visual diffing (narrow lane)

Use full-screen diff contracts only when the frame shape itself is the contract:
- compact viewport overflow behavior
- multiline payload wrapping/continuation cues
- punctuation/alignment grammar where exact form matters

Do not use visual diffs for simple presence checks; that creates brittle tests.

## 3) Boundary placement

- Root tests own cross-section orchestration intents only.
- Subview-specific grammar belongs to leaf packages (`routing`, `clients`, `traffic`, etc.).
- If a root test only validates a single section's local copy/layout, move it to the leaf package.

## 4) Scenario shape

Every non-trivial scenario should read as:
- `Given`: model seed + viewport
- `When`: operator actions (keys/focus/disclosure)
- `Then`: declarative predicates (and negative predicates)

If a helper hides this structure, delete it.

// Package retained is a migration bridge between the retained ViewSpec API and
// the new core.Node semantic algebra.
//
// Deprecated: All new code should target core.Node constructors (core.Text,
// core.Box, core.Stack, core.Scroll, etc.) and compose via the corelower
// lowering adapter. The types and helpers in this package are retained only
// until all existing call sites in apps/cockpit are migrated away.
//
// Current status of each surface:
//   - ViewSpec, Context, Build, View, Named, Materialize: bridge only; migrate
//     callers to core.Node composition and CoreNodeAsRetained
//   - UseState: legacy shim with TODO to remove after all callers migrate to model state
//   - BuildWithLifecycle: DELETED in 2026-06-14 slice 8
//   - Modifiers (WithPadding, WithGrow, WithScrollY, etc.): DELETED in 2026-06-14 slice 8
//     → use Constrain, Padded, ScrollY, Grow direct helpers as bridge
//   - VStack, HStack, Flex, Padded, Grow, Constrain, ScrollY: bridge helpers; migrate
//     callers to core.Stack, core.Box, core.Scroll
//
// The engine rendergraph types (RenderNode = layout.RenderNode) remain
// engine-internal and are not part of the app-facing contract.
package retained

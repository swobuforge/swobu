# terminalui architecture

terminalui has two presentation modes:

1. transcript: line-oriented, append/live/fullscreen rendering for non-interactive output.
2. retained: interactive cockpit UI with retained identity, local state, focus, effects, layout, and paint.

Rules:

- Author-facing components live in `component`; they build semantic
  `core.Node` values and own build-scoped local state.
- Reusable primitive and compound constructors live in `components/*`; they
  stay on the core side of the seam and avoid rendergraph coupling.
- Semantic UI algebra lives in `core`; it describes nodes, layout, style,
  interaction, and contract vocabulary without owning runtime or terminal I/O.
- `corelower` is the bridge from semantic `core.Node` into retained
  rendergraph primitives.
- `view` is the deprecated transcript compatibility shim; new code should use
  `transcript` directly.
- `view/retained` owns the bridge functions that lower core views into the
  retained view contract.
- Components describe UI.
- Runtime owns time, focus, reconciliation, and effects.
- Domain/app owns truth.
- Rendergraph owns measure, arrange, and paint.
- Host owns terminal I/O.
- Effects touch the world.
- Views must not mutate during build.
- Dynamic and stateful children need stable keys.

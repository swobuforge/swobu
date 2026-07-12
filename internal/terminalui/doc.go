// Package terminalui is the centralized terminal presentation system.
//
// See ARCHITECTURE.md for the stable mode split and boundary contract.
//
// Boundary law:
//   - component: author-facing semantic component API that builds core.Node
//   - components/*: reusable semantic primitive and compound component
//     constructors that build core.Node values
//   - core: semantic UI algebra and validation vocabulary for component
//     authoring
//   - corelower: bridge from semantic core nodes into retained rendergraph
//   - transcript: line-oriented, append/live/fullscreen rendering for
//     non-interactive output
//   - retained: interactive cockpit UI with retained identity, local state,
//     focus, effects, layout, and paint
//   - engine: framework runtime/model/output mechanics
//   - toolkit: reusable framework-grammar views and interaction elements
//   - apps/*: product-specific presentation assemblies (CLI, cockpit)
//
// This package family is product runtime, not developer tooling.
package terminalui

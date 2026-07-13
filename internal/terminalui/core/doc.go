// Package core owns the semantic UI algebra for terminalui.
//
// It defines the portable authoring vocabulary for nodes, layout, style,
// interaction, contracts, capabilities, and validation. It deliberately does
// not depend on rendergraph, host, or app packages.
//
// Core types:
//   - Node: immutable semantic UI intent tree.
//   - App[S, E]: pure typed application contract (Init/Update/View).
//   - Effect[E]: declarative external work with keyed lifecycle policies.
//   - Layout, Style, InteractionSpec: semantic descriptors resolved by the
//     framework compiler.
package core

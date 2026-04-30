// Package view defines the public typed view API and retained local
// state scope for the extractable retained TUI engine layer.
//
// # ViewSpec contract
//
// Authoring views are declarative values ([ViewSpec][M]). Engine/view owns
// runtime node materialization behind a package-sealed seam.
//
// Authoring follows a function-first model: builders return [ViewSpec][M].
// Runtime node creation uses a separate internal verb.
//
// # Ergonomic composition
//
// Use [VStack], [VStackGap], [HStack], [HStackGap], and [Flex] to compose
// structure. Use view-transform helpers (for example [WithGrow],
// [WithPadLeft], [WithConstrain]) directly as function composition.
//
//	view.VStack(ctx,
//	    HeaderView{},
//	    view.WithGrow[Model]()(BodyView{}),
//	    FooterView{},
//	)
//
// Keys are relative to parent — the reconciler provides namespace isolation.
// Just use the view type name; the engine handles scoping.
//
// # Hook local state
//
// Local state uses [UseState] with React-style call-order identity inside one
// Build scope. [Build] creates component-local scope boundaries; [Named]
// applies stable identity prefixing for continuity across reorder.
//
// # Typed keyboard input
//
// Interaction events carry [interaction.Key] enums.
//
// Package docs are the source of truth. Do not infer architecture from code alone.
package view

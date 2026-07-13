// Package corelower bridges semantic core nodes into retained rendergraph nodes.
//
// It is a hard compiler: invalid core trees always produce errors, never
// render nodes. This boundary validates semantic integrity before the retained
// engine sees any layout or paint work.
//
// Intent routing and style resolution live here.
//
// # Migration architecture
//
// The retained engine (update.Action, view/retained, rendergraph, host) is
// internal implementation machinery that app code must never depend on.
// The migration bridge is:
//
//	app code → core.Node[E] → corelower.Lower → retained rendergraph → screen
//
// As of the wall slice, the ONLY retained type visible from app code is
// corelower.Lower (called by bridge helpers). All other retained imports
// must be quarantined behind internal/ packages.
//
// # Effect model
//
// core.Effect[E] returns exactly one event of type E. The retained engine
// supports multiple-return effects (Execute → []Action). During migration,
// bridgeEffects maps core.Effect[E] into retained update.Effect. Once all
// effects migrate to core.Effect[E], the retained effect model will be
// retired.
//
// # Runtime entrypoint
//
// New code should use engine.RunApp[S,E] with core.App[S,E]. Legacy code
// uses host.New + retained.ViewSpec. RunApp bridges App into the retained
// runtime internally.
package corelower

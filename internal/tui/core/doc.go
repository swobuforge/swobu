// Package core defines the semantic types for the denotational TUI framework.
//
// Core owns:
//
//	Node[E]        – immutable semantic UI intent tree
//	App[S,E]       – application contract (state, events, update, view)
//	Effect[E]      – typed async work declarations
//	Signal[E]      – node interaction to typed app event
//	Key            – semantic node identity
//	FocusID        – focusable node identity
//	FocusScopeID   – focus boundary identity
//	Intent         – semantic user actions
//	Diagnostic     – deterministic validation and compilation issue
//	Frame          – terminal cell buffer output
//
// Core also owns semantic invalid tree classes: missing keys, duplicate keys,
// invalid focusability, action/field signal requirements, invalid ranges,
// disabled interaction rules, and unsupported roles.
//
// Core has no dependencies on runtime, terminal, compiler, or application
// packages. Product screens may import core but must not import compiler,
// runtime, or terminal directly.
//
// Import law: standard library only (context is allowed for Effect). No
// terminal libraries, no product routing nouns (Route, Target, Rank, Weight,
// AttemptPlan, Trace), no compatibility aliases.
package core

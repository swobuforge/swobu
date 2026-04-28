// Package host owns the terminal backend adapter and runtime execution seam
// for the retained TUI engine.
//
// This package maps tcell events into typed engine events and owns the
// terminal event loop, including foreground client handoff that temporarily
// suspends tcell, runs a child process attached to the same terminal, and
// resumes deterministic cockpit rendering. It is the product-facing edge of
// the extractable engine framework -- deleting this package leaves a coherent
// generic engine plus toolkit behind.
package host

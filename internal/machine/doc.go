// Package machine is a generic event-driven state machine kernel with no
// domain knowledge.
//
// One law:
//
//	State and event are the contract.
//
// A reducer declares what state it reads and what event it reacts to.
// That is its entire contract.
//
//	func SomeReducer(s SomeState, e SomeEvent) (SomeState, []Event, []Command, error)
//
// Composite input is just a read view — a struct with exported fields whose
// types are present in the store. No wrapper ceremony.
//
// Commands are the only effect boundary. If a transform does not touch the
// outside world, it is not a command — it is a reducer.
//
// Reflection lives inside the engine. Reducer authors do not see it.
//
// The canonical design lives in
// docs/03-architecture/system-shape-and-request-flow/exchange-machine-design-and-invariants.md.
package machine

// Package clientprofile defines operator-facing client profiles for cockpit
// handoff rendering.
//
// Each client profile is colocated in one file and provides a single Actions
// method that yields operator rows plus side-effect payloads derived from
// runtime context (for example, endpoint base URL). A thin registry exposes
// all available profiles.
//
// A single capability matrix owns both operator-visible action rows and
// run-once wiring for supported clients.
package clientprofile

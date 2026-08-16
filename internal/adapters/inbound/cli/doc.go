// Package cli implements the operator-facing entrypoint and daemon namespace.
//
// It owns argument parsing, startup notices, interactive-vs-noninteractive
// dispatch, machine-readable status output, and graceful lifecycle command
// behavior. It renders startup/version/telemetry copy directly and hands off
// to Cockpit without retained session plumbing. Human notice boxes clamp to
// the output terminal and wrap complete commands inside their borders; machine
// output remains undecorated. The connect leaf owns workspace selection and
// semantic presentation while clientconnect owns every foreign-file effect.
// It does not own runtime truth, provider wiring, or domain mutation semantics.
package cli

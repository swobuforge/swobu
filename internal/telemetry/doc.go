// Package telemetry owns local operator-facing product telemetry state
// and command-level telemetry preview behavior.
//
// It defines v0 telemetry enablement semantics for the CLI
// (`swobu telemetry status|on|off`) plus `DO_NOT_TRACK` override.
// It does not
// own runtime evidence truth, request-path behavior, or provider adaptation.
package telemetry

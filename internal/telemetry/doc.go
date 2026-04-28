// Package telemetry owns local operator-facing product telemetry state
// and command-level telemetry preview behavior.
//
// It defines v0 telemetry enablement and inspect semantics for the CLI
// (`swobu telemetry status|on|off|inspect|show-payload|reset`). It does not
// own runtime evidence truth, request-path behavior, or provider adaptation.
package telemetry

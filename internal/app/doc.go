// Package app is the namespace root for Swobu application-layer use cases.
//
// The real ownership lives in child packages:
//
// - `app/operator/authplane`: auth/session orchestration and credential state
// - `app/operator/chatgptlogin`: ChatGPT auth callback and device-flow helpers
// - `app/operator/client`: client-facing endpoint selection and probe helpers
// - `app/operator/controlplane`: daemon version and control-plane helpers
// - `app/operator/daemonlifecycle`: daemon start/attach/shutdown use cases
// - `app/operator/endpoints`: daemon-owned endpoint-intent control use cases
//
// Inbound adapters call these use-case packages. The daemon process hosts them.
// They are neither UI packages nor runtime-container packages.
package app

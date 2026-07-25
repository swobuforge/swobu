package controlplane

import "strings"

// swobuVersion is overridden at build time via:
// -ldflags "-X github.com/swobuforge/swobu/internal/app/operator/controlplane.swobuVersion=vX.Y.Z"
var swobuVersion = "dev"

// SwobuVersion returns the canonical daemon/operator version string surfaced
// through internal control-plane status payloads. It is carried in product
// telemetry as a bounded opaque evidence token, not parsed or classified by the
// client (dev / v0.4.0 / 0.4.0-dev are all accepted); version semantics are
// derived downstream.
func SwobuVersion() string {
	value := strings.TrimSpace(swobuVersion) // swobu:io-string source=boundary
	if value == "" {
		return "dev"
	}
	return value
}

package controlplane

import "strings"

// swobuVersion is overridden at build time via:
// -ldflags "-X github.com/metrofun/swobu/internal/app/operator/controlplane.swobuVersion=vX.Y.Z"
var swobuVersion = "dev"

// SwobuVersion returns the canonical daemon/operator version string surfaced
// through internal control-plane status payloads.
func SwobuVersion() string {
	value := strings.TrimSpace(swobuVersion)
	if value == "" {
		return "dev"
	}
	return value
}

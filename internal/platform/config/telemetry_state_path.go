package config

import (
	"path/filepath"
)

// DefaultTelemetryStateDir is the directory holding the telemetry state files
// (identity, preference, notice, first-success). The store owns this directory;
// no single file is the centre of gravity.
func DefaultTelemetryStateDir() string {
	return filepath.Join(defaultStateRoot(), "telemetry")
}

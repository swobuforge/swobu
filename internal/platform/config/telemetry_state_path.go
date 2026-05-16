package config

import (
	"path/filepath"
)

func DefaultTelemetryStatePath() string {
	return filepath.Join(defaultStateRoot(), "telemetry", "state.json")
}

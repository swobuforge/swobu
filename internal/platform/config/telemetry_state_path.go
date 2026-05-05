package config

import (
	"os"
	"path/filepath"
	"strings"
)

func defaultTelemetryStatePath() string {
	if explicit := strings.TrimSpace(os.Getenv(EnvTelemetryStatePath)); explicit != "" {
		return explicit
	}
	if xdg := strings.TrimSpace(os.Getenv(EnvXDGStateHome)); xdg != "" {
		return filepath.Join(xdg, "swobu", "telemetry", "state.json")
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return filepath.Join(".", ".swobu", "telemetry", "state.json")
	}
	return filepath.Join(home, ".local", "state", "swobu", "telemetry", "state.json")
}

package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// ResolveTelemetryEndpoint applies env override over the built-in endpoint.
func ResolveTelemetryEndpoint(defaultValue string) string {
	if explicit := strings.TrimSpace(os.Getenv(EnvTelemetryEndpoint)); explicit != "" { // swobu:io-string source=boundary
		return explicit
	}
	return strings.TrimSpace(defaultValue) // swobu:io-string source=boundary
}

// ResolveTelemetryExportInterval applies env override over a built-in export interval.
func ResolveTelemetryExportInterval(defaultValue time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(EnvTelemetryExportIntervalSeconds)) // swobu:io-string source=boundary
	if raw == "" {
		return defaultValue
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds <= 0 {
		return defaultValue
	}
	return time.Duration(seconds) * time.Second
}

// ResolveConfigPath resolves the config path for `swobu daemon`.
// If the operator supplied --config explicitly, that path is used as-is.
// Otherwise the default path is returned without filesystem mutation.
func ResolveConfigPath(flagValue string) string {
	if trimmed := strings.TrimSpace(flagValue); trimmed != "" { // swobu:io-string source=boundary
		return trimmed
	}
	return DefaultConfigPath()
}

// ResolveAuthCredentialWritePolicy resolves daemon credential write policy.
func ResolveAuthCredentialWritePolicy() string {
	return strings.TrimSpace(strings.ToLower(os.Getenv(EnvAuthCredentialWritePolicy))) // swobu:io-string source=boundary
}

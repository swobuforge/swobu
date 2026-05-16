package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// ResolveDaemonURL applies standard CLI precedence: flag > env > default.
func ResolveDaemonURL(flagValue string) string {
	if trimmed := strings.TrimSpace(flagValue); trimmed != "" { // trimlowerlint:allow boundary canonicalization
		return trimmed
	}
	return DefaultDaemonURL()
}

// ResolveConfigPath applies standard CLI precedence: flag > env > default.
func ResolveConfigPath(flagValue string) string {
	if trimmed := strings.TrimSpace(flagValue); trimmed != "" { // trimlowerlint:allow boundary canonicalization
		return trimmed
	}
	return DefaultConfigPath()
}

// ResolveTelemetryEndpoint applies env override over the built-in endpoint.
func ResolveTelemetryEndpoint(defaultValue string) string {
	if explicit := strings.TrimSpace(os.Getenv(EnvTelemetryEndpoint)); explicit != "" { // trimlowerlint:allow boundary canonicalization
		return explicit
	}
	return strings.TrimSpace(defaultValue) // trimlowerlint:allow boundary canonicalization
}

// ResolveTelemetryExportInterval applies env override over a built-in export interval.
func ResolveTelemetryExportInterval(defaultValue time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(EnvTelemetryExportIntervalSeconds)) // trimlowerlint:allow boundary canonicalization
	if raw == "" {
		return defaultValue
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds <= 0 {
		return defaultValue
	}
	return time.Duration(seconds) * time.Second
}

// ResolveDaemonRuntimeConfigPath resolves the config path for `swobu daemon`.
// If the operator supplied --config explicitly, that path is used as-is.
// Otherwise the default path is resolved and ensured on disk.
func ResolveDaemonRuntimeConfigPath(flagValue string) (string, error) {
	if trimmed := strings.TrimSpace(flagValue); trimmed != "" { // trimlowerlint:allow boundary canonicalization
		return trimmed, nil
	}
	return EnsureDefaultConfigFile()
}

// ResolveAuthCredentialWritePolicy resolves daemon credential write policy.
func ResolveAuthCredentialWritePolicy() string {
	return strings.TrimSpace(strings.ToLower(os.Getenv(EnvAuthCredentialWritePolicy))) // trimlowerlint:allow boundary canonicalization
}

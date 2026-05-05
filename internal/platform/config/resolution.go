package config

import "strings"

// ResolveDaemonURL applies standard CLI precedence: flag > env > default.
func ResolveDaemonURL(flagValue string) string {
	if trimmed := strings.TrimSpace(flagValue); trimmed != "" {
		return trimmed
	}
	return DefaultDaemonURL()
}

// ResolveConfigPath applies standard CLI precedence: flag > env > default.
func ResolveConfigPath(flagValue string) string {
	if trimmed := strings.TrimSpace(flagValue); trimmed != "" {
		return trimmed
	}
	return DefaultConfigPath()
}

// ResolveTelemetryStatePath applies standard CLI precedence:
// flag > env > built-in state-path default.
func ResolveTelemetryStatePath(flagValue string) string {
	if trimmed := strings.TrimSpace(flagValue); trimmed != "" {
		return trimmed
	}
	return defaultTelemetryStatePath()
}

// ResolveDaemonRuntimeConfigPath resolves the config path for `swobu daemon`.
// If the operator supplied --config explicitly, that path is used as-is.
// Otherwise the default path is resolved and ensured on disk.
func ResolveDaemonRuntimeConfigPath(flagValue string) (string, error) {
	if trimmed := strings.TrimSpace(flagValue); trimmed != "" {
		return trimmed, nil
	}
	return EnsureDefaultConfigFile()
}

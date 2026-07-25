package config

import (
	"os"
	"strings"
)

const (
	EnvAddr                      = "SWOBU_ADDR"
	EnvConfigPath                = "SWOBU_CONFIG_PATH"
	EnvSwobuHome                 = "SWOBU_HOME"
	EnvXDGStateHome              = "XDG_STATE_HOME"
	EnvAuthCredentialWritePolicy = "SWOBU_AUTH_CREDENTIAL_WRITE_POLICY"

	EnvTelemetryEndpoint = "SWOBU_TELEMETRY_ENDPOINT_URL"
	EnvDoNotTrack        = "DO_NOT_TRACK"
	EnvSkipVersionNotice = "SWOBU_SKIP_VERSION_NOTICE"
)

func EnvTruthy(value string) bool {
	normalized := strings.TrimSpace(strings.ToLower(value)) // swobu:io-string source=boundary
	return normalized == "1" || normalized == "true" || normalized == "yes" || normalized == "on"
}

func ReadEnvTrim(key string) string {
	return strings.TrimSpace(os.Getenv(key)) // swobu:io-string source=boundary
}

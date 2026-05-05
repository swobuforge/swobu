package config

import "strings"

const (
	EnvDaemonURL    = "SWOBU_DAEMON_URL"
	EnvConfigPath   = "SWOBU_CONFIG_PATH"
	EnvXDGStateHome = "XDG_STATE_HOME"

	EnvTelemetryEndpoint  = "SWOBU_TELEMETRY_ENDPOINT"
	EnvTelemetryInterval  = "SWOBU_TELEMETRY_INTERVAL"
	EnvTelemetryDebug     = "SWOBU_TELEMETRY_DEBUG"
	EnvTelemetryStatePath = "SWOBU_TELEMETRY_STATE_PATH"
	EnvDoNotTrack         = "DO_NOT_TRACK"
	EnvSkipVersionNotice  = "SWOBU_SKIP_VERSION_NOTICE"
)

func EnvTruthy(value string) bool {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

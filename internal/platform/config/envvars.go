package config

import "strings"

const (
	EnvDaemonURL    = "SWOBU_DAEMON_URL"
	EnvConfigPath   = "SWOBU_CONFIG_PATH"
	EnvXDGStateHome = "XDG_STATE_HOME"

	EnvTelemetryEndpoint             = "SWOBU_TELEMETRY_ENDPOINT_URL"
	EnvTelemetryIntervalSeconds      = "SWOBU_TELEMETRY_INTERVAL_SECONDS"
	EnvTelemetryDebugStdoutSink      = "SWOBU_TELEMETRY_STDOUT_SINK_DEBUG"
	EnvTelemetryDebugTraceStack      = "SWOBU_TELEMETRY_ERROR_TRACE_STACK_DEBUG"
	EnvTelemetryErrorTraceMaxPerTick = "SWOBU_TELEMETRY_ERROR_TRACE_MAX_PER_TICK"
	EnvTelemetryStatePath            = "SWOBU_TELEMETRY_STATE_PATH"
	EnvDoNotTrack                    = "DO_NOT_TRACK"
	EnvSkipVersionNotice             = "SWOBU_SKIP_VERSION_NOTICE"
)

func EnvTruthy(value string) bool {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

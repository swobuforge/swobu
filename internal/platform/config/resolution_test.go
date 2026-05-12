package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestResolveDaemonURL_Preference(t *testing.T) {
	t.Setenv(EnvDaemonURL, "http://env.test:7777")

	if got := ResolveDaemonURL("http://flag.test:8888"); got != "http://flag.test:8888" {
		t.Fatalf("flag daemon url = %q, want %q", got, "http://flag.test:8888")
	}
	if got := ResolveDaemonURL(""); got != "http://env.test:7777" {
		t.Fatalf("env daemon url = %q, want %q", got, "http://env.test:7777")
	}
}

func TestResolveConfigPath_Preference(t *testing.T) {
	envPath := filepath.Join(t.TempDir(), "env-swobu.yaml")
	t.Setenv(EnvConfigPath, envPath)

	flagPath := filepath.Join(t.TempDir(), "flag-swobu.yaml")
	if got := ResolveConfigPath(flagPath); got != flagPath {
		t.Fatalf("flag config path = %q, want %q", got, flagPath)
	}
	if got := ResolveConfigPath(""); got != envPath {
		t.Fatalf("env config path = %q, want %q", got, envPath)
	}
}

func TestResolveTelemetryStatePath_Preference(t *testing.T) {
	envPath := filepath.Join(t.TempDir(), "env-state.json")
	t.Setenv(EnvTelemetryStatePath, envPath)

	flagPath := filepath.Join(t.TempDir(), "flag-state.json")
	if got := ResolveTelemetryStatePath(flagPath); got != flagPath {
		t.Fatalf("flag state path = %q, want %q", got, flagPath)
	}
	if got := ResolveTelemetryStatePath(""); got != envPath {
		t.Fatalf("env state path = %q, want %q", got, envPath)
	}
}

func TestResolveTelemetryEndpoint_Preference(t *testing.T) {
	t.Setenv(EnvTelemetryEndpoint, "http://127.0.0.1:8787")

	if got := ResolveTelemetryEndpoint("https://swobu.com"); got != "http://127.0.0.1:8787" {
		t.Fatalf("env telemetry endpoint = %q, want %q", got, "http://127.0.0.1:8787")
	}
}

func TestResolveTelemetryEndpoint_DefaultWhenEnvMissing(t *testing.T) {
	if got := ResolveTelemetryEndpoint("https://swobu.com"); got != "https://swobu.com" {
		t.Fatalf("default telemetry endpoint = %q, want %q", got, "https://swobu.com")
	}
}

func TestResolveTelemetryExportInterval_DefaultWhenEnvMissing(t *testing.T) {
	if got := ResolveTelemetryExportInterval(15 * time.Minute); got != 15*time.Minute {
		t.Fatalf("default telemetry export interval = %s, want %s", got, 15*time.Minute)
	}
}

func TestResolveTelemetryExportInterval_OverrideSeconds(t *testing.T) {
	t.Setenv(EnvTelemetryExportIntervalSeconds, "90")
	if got := ResolveTelemetryExportInterval(15 * time.Minute); got != 90*time.Second {
		t.Fatalf("telemetry export interval override = %s, want %s", got, 90*time.Second)
	}
}

func TestResolveTelemetryExportInterval_InvalidFallsBackToDefault(t *testing.T) {
	t.Setenv(EnvTelemetryExportIntervalSeconds, "abc")
	if got := ResolveTelemetryExportInterval(15 * time.Minute); got != 15*time.Minute {
		t.Fatalf("invalid telemetry export interval = %s, want %s", got, 15*time.Minute)
	}
}

func TestResolveDaemonRuntimeConfigPath_Preference(t *testing.T) {
	flagPath := filepath.Join(t.TempDir(), "flag-swobu.yaml")
	resolved, err := ResolveDaemonRuntimeConfigPath(flagPath)
	if err != nil {
		t.Fatalf("ResolveDaemonRuntimeConfigPath returned error: %v", err)
	}
	if resolved != flagPath {
		t.Fatalf("resolved path = %q, want %q", resolved, flagPath)
	}
}

func TestResolveDaemonRuntimeConfigPath_EnsuresDefaultWhenFlagOmitted(t *testing.T) {
	envPath := filepath.Join(t.TempDir(), "default-swobu.yaml")
	t.Setenv(EnvConfigPath, envPath)

	resolved, err := ResolveDaemonRuntimeConfigPath("")
	if err != nil {
		t.Fatalf("ResolveDaemonRuntimeConfigPath returned error: %v", err)
	}
	if resolved != envPath {
		t.Fatalf("resolved path = %q, want %q", resolved, envPath)
	}
	if _, statErr := os.Stat(envPath); statErr != nil {
		t.Fatalf("resolved config file not created: %v", statErr)
	}
}

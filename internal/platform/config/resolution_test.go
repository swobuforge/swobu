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
	t.Setenv(EnvSwobuHome, filepath.Join(t.TempDir(), "swobu-home"))

	flagPath := filepath.Join(t.TempDir(), "flag-swobu.yaml")
	if got := ResolveConfigPath(flagPath); got != flagPath {
		t.Fatalf("flag config path = %q, want %q", got, flagPath)
	}
	if got := ResolveConfigPath(""); got != envPath {
		t.Fatalf("env config path = %q, want %q", got, envPath)
	}
}

func TestDefaultConfigPath_UsesSwobuHomeWhenNoExplicitConfigPath(t *testing.T) {
	root := filepath.Join(t.TempDir(), "swobu-home")
	t.Setenv(EnvConfigPath, "")
	t.Setenv(EnvSwobuHome, root)
	want := filepath.Join(root, "config", "swobu.yaml")
	if got := DefaultConfigPath(); got != want {
		t.Fatalf("DefaultConfigPath()=%q want %q", got, want)
	}
}

func TestDefaultTelemetryStatePath_UsesSwobuHome(t *testing.T) {
	withRuntimeGOOS(t, "linux")
	root := filepath.Join(t.TempDir(), "swobu-home")
	t.Setenv(EnvSwobuHome, root)
	want := filepath.Join(root, "state", "telemetry", "state.json")
	if got := DefaultTelemetryStatePath(); got != want {
		t.Fatalf("DefaultTelemetryStatePath()=%q want %q", got, want)
	}
}

func TestDefaultTelemetryStatePath_UsesUserConfigDirStateOnNonLinux(t *testing.T) {
	withRuntimeGOOS(t, "windows")
	t.Setenv(EnvSwobuHome, "")
	t.Setenv(EnvXDGStateHome, "")
	configHome := filepath.Join(t.TempDir(), "config-home")
	t.Setenv("XDG_CONFIG_HOME", configHome)

	want := filepath.Join(configHome, "swobu", "state", "telemetry", "state.json")
	if got := DefaultTelemetryStatePath(); got != want {
		t.Fatalf("DefaultTelemetryStatePath()=%q want %q", got, want)
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
	envPath := filepath.Join(t.TempDir(), "nested-config", "default-swobu.yaml")
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
	dirInfo, err := os.Stat(filepath.Dir(envPath))
	if err != nil {
		t.Fatalf("stat config dir: %v", err)
	}
	if dirInfo.Mode().Perm()&0o077 != 0 {
		t.Fatalf("config dir permissions = %o want no group/other bits", dirInfo.Mode().Perm())
	}
	fileInfo, err := os.Stat(envPath)
	if err != nil {
		t.Fatalf("stat config file: %v", err)
	}
	if fileInfo.Mode().Perm()&0o077 != 0 {
		t.Fatalf("config file permissions = %o want no group/other bits", fileInfo.Mode().Perm())
	}
}

func TestResolveAuthCredentialPolicies(t *testing.T) {
	t.Setenv(EnvAuthCredentialWritePolicy, "FILE")
	if got := ResolveAuthCredentialWritePolicy(); got != "file" {
		t.Fatalf("ResolveAuthCredentialWritePolicy()=%q want file", got)
	}
}

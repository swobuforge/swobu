package config

import (
	"path/filepath"
	"testing"
)

func withRuntimeGOOS(t *testing.T, goos string) {
	t.Helper()
	previous := runtimeGOOS
	runtimeGOOS = goos
	t.Cleanup(func() {
		runtimeGOOS = previous
	})
}

func TestDefaultAuthCredentialFilePath_UsesSwobuHome(t *testing.T) {
	withRuntimeGOOS(t, "linux")
	root := filepath.Join(t.TempDir(), "swobu-home")
	t.Setenv(EnvSwobuHome, root)
	t.Setenv(EnvXDGStateHome, filepath.Join(t.TempDir(), "xdg-state"))

	want := filepath.Join(root, "state", "auth", "chatgpt.json")
	if got := DefaultAuthCredentialFilePath(); got != want {
		t.Fatalf("DefaultAuthCredentialFilePath=%q want %q", got, want)
	}
}

func TestDefaultAuthCredentialFilePath_UsesLinuxXDGStateWhenSwobuHomeMissing(t *testing.T) {
	withRuntimeGOOS(t, "linux")
	t.Setenv(EnvSwobuHome, "")
	xdgStateHome := filepath.Join(t.TempDir(), "xdg-state")
	t.Setenv(EnvXDGStateHome, xdgStateHome)

	want := filepath.Join(xdgStateHome, "swobu", "auth", "chatgpt.json")
	if got := DefaultAuthCredentialFilePath(); got != want {
		t.Fatalf("DefaultAuthCredentialFilePath=%q want %q", got, want)
	}
}

func TestDefaultAuthCredentialFilePath_UsesUserConfigDirStateOnNonLinux(t *testing.T) {
	withRuntimeGOOS(t, "darwin")
	t.Setenv(EnvSwobuHome, "")
	t.Setenv(EnvXDGStateHome, "")
	configHome := filepath.Join(t.TempDir(), "config-home")
	t.Setenv("XDG_CONFIG_HOME", configHome)

	want := filepath.Join(configHome, "swobu", "state", "auth", "chatgpt.json")
	if got := DefaultAuthCredentialFilePath(); got != want {
		t.Fatalf("DefaultAuthCredentialFilePath=%q want %q", got, want)
	}
}

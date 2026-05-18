package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

var runtimeGOOS = runtime.GOOS

func defaultSwobuHome() string {
	if explicit := strings.TrimSpace(os.Getenv(EnvSwobuHome)); explicit != "" { // swobu:io-string source=boundary
		return explicit
	}
	return ""
}

func defaultStateRoot() string {
	if home := defaultSwobuHome(); home != "" {
		return filepath.Join(home, "state")
	}
	if runtimeGOOS == "linux" {
		if xdg := strings.TrimSpace(os.Getenv(EnvXDGStateHome)); xdg != "" { // swobu:io-string source=boundary
			return filepath.Join(xdg, "swobu")
		}
		home, err := os.UserHomeDir()
		if err != nil || strings.TrimSpace(home) == "" { // swobu:io-string source=boundary
			return filepath.Join(".", ".swobu", "state")
		}
		return filepath.Join(home, ".local", "state", "swobu")
	}
	configDir, err := os.UserConfigDir()
	if err == nil && strings.TrimSpace(configDir) != "" { // swobu:io-string source=boundary
		return filepath.Join(configDir, "swobu", "state")
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" { // swobu:io-string source=boundary
		return filepath.Join(".", ".swobu", "state")
	}
	return filepath.Join(home, ".swobu", "state")
}

func DefaultAuthCredentialFilePath() string {
	return filepath.Join(defaultStateRoot(), "auth", "chatgpt.json")
}

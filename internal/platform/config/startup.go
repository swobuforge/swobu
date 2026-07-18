package config

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const defaultAddr = "127.0.0.1:7926"

// StartupConfig is the single source for the local daemon socket. The daemon
// binds to Addr and internal clients derive their HTTP base URL from Addr.
type StartupConfig struct {
	Addr string
}

// ParseAddr validates the local-only daemon address.
func ParseAddr(raw string) (string, error) {
	raw = strings.TrimSpace(raw) // swobu:io-string source=boundary
	host, rawPort, err := net.SplitHostPort(raw)
	if err != nil || host == "" || rawPort == "" {
		return "", fmt.Errorf("address must be a loopback host:port")
	}
	port, err := strconv.Atoi(rawPort)
	if err != nil || port < 0 || port > 65535 {
		return "", fmt.Errorf("address port must be numeric and between 0 and 65535")
	}
	ip := net.ParseIP(host)
	if !strings.EqualFold(host, "localhost") && (ip == nil || !ip.IsLoopback()) {
		return "", fmt.Errorf("address host %q must be loopback", host)
	}
	return raw, nil
}

func DefaultAddr() string { return defaultAddr }

// ResolveStartupConfig applies --addr, SWOBU_ADDR, then the local default.
func ResolveStartupConfig(explicit string) (StartupConfig, error) {
	raw := strings.TrimSpace(explicit) // swobu:io-string source=boundary
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv(EnvAddr)) // swobu:io-string source=boundary
	}
	if raw == "" {
		raw = DefaultAddr()
	}
	addr, err := ParseAddr(raw)
	if err != nil {
		return StartupConfig{}, err
	}
	return StartupConfig{Addr: addr}, nil
}

func BaseURL(addr string) string { return "http://" + addr }

func DefaultConfigPath() string {
	if configPath := strings.TrimSpace(os.Getenv(EnvConfigPath)); configPath != "" {
		return configPath
	}
	if home := defaultSwobuHome(); strings.TrimSpace(home) != "" {
		return filepath.Join(home, "config", "swobu.yaml")
	}
	configDir, err := os.UserConfigDir()
	if err != nil || strings.TrimSpace(configDir) == "" {
		return "swobu.yaml"
	}
	return filepath.Join(configDir, "swobu", "swobu.yaml")
}

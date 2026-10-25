package config

import (
	"crypto/sha256"
	"errors"
	"fmt"
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

// DefaultStateRoot returns the canonical root for Swobu-managed runtime state.
// Callers must keep generated artifacts beneath a concern-specific subtree.
func DefaultStateRoot() string {
	return defaultStateRoot()
}

func DefaultAuthCredentialFilePath() string {
	return filepath.Join(defaultStateRoot(), "secrets.json")
}

// NodeRuntimeRoot is the single location policy for crash-sensitive state.
// Managed Nodes retain their established colocated runtime files. Externally
// managed Nodes must not write beside a read-only mounted YAML file and must
// never share reservations with a different Node owned by the same OS user.
func NodeRuntimeRoot(configPath string, externallyManaged bool) (string, error) {
	if !externallyManaged {
		return filepath.Dir(configPath), nil
	}
	absolute, err := filepath.Abs(configPath)
	if err != nil {
		return "", fmt.Errorf("resolve Node configuration path: %w", err)
	}
	// Normalize symlink aliases before deriving an accounting identity: two
	// paths naming the same mounted config must not create separate budgets.
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve Node configuration identity: %w", err)
	}
	identity := sha256.Sum256([]byte(filepath.Clean(canonical)))
	return filepath.Join(DefaultStateRoot(), "external-nodes", fmt.Sprintf("%x", identity[:12])), nil
}

// ValidateNodeRuntimeOwnership prevents changing managed/read-only mode while
// leaving crash-sensitive accounting or relay state at the other location.
// Explicit operator file transfer is required; automatic copying could split
// an in-flight accounting journal or silently rotate an relay transport identity.
func ValidateNodeRuntimeOwnership(configPath string, externallyManaged bool, chosen string) error {
	other := filepath.Dir(configPath)
	if !externallyManaged {
		var err error
		other, err = NodeRuntimeRoot(configPath, true)
		if err != nil {
			return err
		}
	}
	if filepath.Clean(other) == filepath.Clean(chosen) {
		return nil
	}
	// A mode switch cannot silently abandon a preexisting SQLite history.
	// The SQLite primary file identifies retained history; SQLite itself owns
	// its recovery files. No local ACL or sidecar state machine is required.
	stem := strings.TrimSuffix(filepath.Base(configPath), filepath.Ext(configPath))
	ledgerName := "ledger.sqlite3"
	searchName := "search-evidence.json"
	if externallyManaged {
		ledgerName = stem + ".ledger.sqlite3"
		searchName = stem + ".search-evidence.json"
	}
	for _, name := range []string{"accounting.json", "relay.json", ledgerName, searchName} {
		prior := filepath.Join(other, name)
		if _, err := os.Stat(prior); err == nil {
			return fmt.Errorf("runtime ownership changed: existing %s would be ignored; stop the daemon and explicitly transfer runtime evidence to %s before changing configuration mode", prior, chosen)
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("check runtime evidence %s: %w", prior, err)
		}
	}
	return nil
}

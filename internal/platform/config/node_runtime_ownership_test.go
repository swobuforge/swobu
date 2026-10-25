package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSwitchingConfigurationModeNeverForgetsAccountingOrRelayEvidence(t *testing.T) {
	t.Setenv("SWOBU_HOME", t.TempDir())
	configPath := filepath.Join(t.TempDir(), "swobu.yaml")
	if err := os.WriteFile(configPath, []byte("schema_version: 5\nworkspaces: {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	managedDir, err := NodeRuntimeRoot(configPath, false)
	if err != nil {
		t.Fatal(err)
	}
	externalDir, err := NodeRuntimeRoot(configPath, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateNodeRuntimeOwnership(configPath, true, externalDir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(managedDir, "accounting.json"), []byte(`{"reservations":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateNodeRuntimeOwnership(configPath, true, externalDir); err == nil || !strings.Contains(err.Error(), "accounting.json") {
		t.Fatalf("lost reservation history: %v", err)
	}
	if err := os.Remove(filepath.Join(managedDir, "accounting.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(externalDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(externalDir, "relay.json"), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateNodeRuntimeOwnership(configPath, false, managedDir); err == nil || !strings.Contains(err.Error(), "relay.json") {
		t.Fatalf("lost relay transport state: %v", err)
	}
	// Ledger is runtime evidence too; switching mode must not silently open an
	// unrelated empty history. SQLite owns WAL recovery, not this check.
	if err := os.Remove(filepath.Join(externalDir, "relay.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(managedDir, "swobu.ledger.sqlite3"), []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateNodeRuntimeOwnership(configPath, true, externalDir); err == nil || !strings.Contains(err.Error(), "swobu.ledger.sqlite3") {
		t.Fatalf("managed SQLite lost in mode switch: %v", err)
	}
	if err := os.Remove(filepath.Join(managedDir, "swobu.ledger.sqlite3")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(externalDir, "ledger.sqlite3"), []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateNodeRuntimeOwnership(configPath, false, managedDir); err == nil || !strings.Contains(err.Error(), "ledger.sqlite3") {
		t.Fatalf("external SQLite lost in mode switch: %v", err)
	}

}

func TestSwitchingConfigurationModePreservesSearchEvidence(t *testing.T) {
	t.Setenv("SWOBU_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "swobu.yaml")
	if err := os.WriteFile(path, []byte("schema_version: 5\nworkspaces: {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	managed, _ := NodeRuntimeRoot(path, false)
	external, _ := NodeRuntimeRoot(path, true)
	for _, tc := range []struct {
		prior, chosen, name string
		external            bool
	}{
		{managed, external, "swobu.search-evidence.json", true}, {external, managed, "search-evidence.json", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.MkdirAll(tc.prior, 0700); err != nil {
				t.Fatal(err)
			}
			evidencePath := filepath.Join(tc.prior, tc.name)
			if err := os.WriteFile(evidencePath, []byte("existing"), 0600); err != nil {
				t.Fatal(err)
			}
			defer os.Remove(evidencePath)
			if err := ValidateNodeRuntimeOwnership(path, tc.external, tc.chosen); err == nil || !strings.Contains(err.Error(), tc.name) {
				t.Fatalf("mode switch abandoned Search evidence: %v", err)
			}
		})
	}
}

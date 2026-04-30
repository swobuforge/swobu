package telemetry

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewMetricsEmitter_RejectsBlankEndpoint(t *testing.T) {
	t.Parallel()

	_, err := NewMetricsEmitter(context.Background(), MetricsEmitterConfig{EndpointURL: "   "})
	if err == nil {
		t.Fatal("NewMetricsEmitter returned nil error for blank endpoint")
	}
	if !strings.Contains(err.Error(), "otel endpoint is required") {
		t.Fatalf("error = %q, want missing endpoint detail", err.Error())
	}
}

func TestStore_LoadOrCreate_CorruptStateFailsClosedForEnabledCheck(t *testing.T) {
	t.Parallel()

	statePath := filepath.Join(t.TempDir(), "telemetry", "state.json")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(statePath, []byte("{not-json"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	store := Store{StatePath: statePath}
	if store.isTelemetryEnabled() {
		t.Fatal("isTelemetryEnabled = true with corrupt state, want fail-closed false")
	}

	_, err := store.LoadOrCreate()
	if err == nil {
		t.Fatal("LoadOrCreate returned nil error for corrupt state")
	}
	if !strings.Contains(err.Error(), "decode telemetry state") {
		t.Fatalf("error = %q, want decode context", err.Error())
	}
	if !strings.Contains(err.Error(), "invalid character") {
		t.Fatalf("error = %q, want json parser detail", err.Error())
	}
}

func TestStore_Reset_RemoveFailureIncludesDetails(t *testing.T) {
	t.Parallel()

	statePath := filepath.Join(t.TempDir(), "telemetry-state-dir")
	if err := os.MkdirAll(statePath, 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	blockingFile := filepath.Join(statePath, "keep.txt")
	if err := os.WriteFile(blockingFile, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	store := Store{StatePath: statePath}
	_, err := store.Reset()
	if err == nil {
		t.Fatal("Reset returned nil error for non-empty directory path")
	}
	msg := err.Error()
	if !strings.Contains(msg, "remove telemetry state") {
		t.Fatalf("error = %q, want remove context", msg)
	}
	if !strings.Contains(strings.ToLower(msg), "directory") {
		t.Fatalf("error = %q, want filesystem cause detail", msg)
	}
}

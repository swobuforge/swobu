package telemetry

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func TestStore_LoadOrCreate_PersistsDefaults(t *testing.T) {
	t.Parallel()

	statePath := filepath.Join(t.TempDir(), "telemetry", "state.json")
	now := func() time.Time { return time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC) }
	store := Store{
		StatePath: statePath,
		Now:       now,
		Rand:      bytes.NewReader([]byte{0xaa, 0xbb, 0xcc, 0xdd}),
	}

	state, err := store.LoadOrCreate()
	if err != nil {
		t.Fatalf("LoadOrCreate returned error: %v", err)
	}
	if !state.Enabled {
		t.Fatal("enabled = false, want true")
	}
	if state.AnonymousInstallID != "anon_aabbccdd" {
		t.Fatalf("anonymous_install_id = %q, want %q", state.AnonymousInstallID, "anon_aabbccdd")
	}
	if state.FirstSeenAt != "2026-04-27T12:00:00Z" {
		t.Fatalf("first_seen_at = %q, want %q", state.FirstSeenAt, "2026-04-27T12:00:00Z")
	}
	if state.NoticeShown {
		t.Fatal("notice_shown = true, want false")
	}

	second, err := store.LoadOrCreate()
	if err != nil {
		t.Fatalf("second LoadOrCreate returned error: %v", err)
	}
	if second.AnonymousInstallID != state.AnonymousInstallID {
		t.Fatalf("anonymous_install_id changed from %q to %q", state.AnonymousInstallID, second.AnonymousInstallID)
	}
}

func TestStore_SetEnabled_PersistsToggle(t *testing.T) {
	t.Parallel()

	statePath := filepath.Join(t.TempDir(), "telemetry", "state.json")
	store := Store{
		StatePath: statePath,
		Now:       func() time.Time { return time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC) },
		Rand:      bytes.NewReader([]byte{0x01, 0x02, 0x03, 0x04}),
	}
	if _, err := store.LoadOrCreate(); err != nil {
		t.Fatalf("LoadOrCreate returned error: %v", err)
	}
	updated, err := store.SetEnabled(false)
	if err != nil {
		t.Fatalf("SetEnabled returned error: %v", err)
	}
	if updated.Enabled {
		t.Fatal("enabled = true, want false")
	}

	reloaded, err := store.LoadOrCreate()
	if err != nil {
		t.Fatalf("LoadOrCreate returned error: %v", err)
	}
	if reloaded.Enabled {
		t.Fatal("enabled = true after reload, want false")
	}
}

func TestStore_InspectPreview_UsesCurrentState(t *testing.T) {
	t.Parallel()

	statePath := filepath.Join(t.TempDir(), "telemetry", "state.json")
	store := Store{
		StatePath: statePath,
		Now:       func() time.Time { return time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC) },
		Rand:      bytes.NewReader([]byte{0x0a, 0x0b, 0x0c, 0x0d}),
	}
	if _, err := store.LoadOrCreate(); err != nil {
		t.Fatalf("LoadOrCreate returned error: %v", err)
	}
	if _, err := store.SetEnabled(false); err != nil {
		t.Fatalf("SetEnabled returned error: %v", err)
	}

	raw, err := store.InspectPreview()
	if err != nil {
		t.Fatalf("InspectPreview returned error: %v", err)
	}
	var payload struct {
		SchemaVersion      int    `json:"schema_version"`
		Kind               string `json:"kind"`
		AnonymousInstallID string `json:"anonymous_install_id"`
		TelemetryEnabled   bool   `json:"telemetry_enabled"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if payload.SchemaVersion != 1 {
		t.Fatalf("schema_version = %d, want 1", payload.SchemaVersion)
	}
	if payload.Kind != "install_summary" {
		t.Fatalf("kind = %q, want install_summary", payload.Kind)
	}
	if payload.AnonymousInstallID != "anon_0a0b0c0d" {
		t.Fatalf("anonymous_install_id = %q, want %q", payload.AnonymousInstallID, "anon_0a0b0c0d")
	}
	if payload.TelemetryEnabled {
		t.Fatal("telemetry_enabled = true, want false")
	}
}

func TestStore_InspectPreview_DoNotTrackOverride(t *testing.T) {
	t.Setenv("DO_NOT_TRACK", "1")
	statePath := filepath.Join(t.TempDir(), "telemetry", "state.json")
	store := Store{
		StatePath: statePath,
		Now:       func() time.Time { return time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC) },
		Rand:      bytes.NewReader([]byte{0x0a, 0x0b, 0x0c, 0x0d}),
	}
	if _, err := store.LoadOrCreate(); err != nil {
		t.Fatalf("LoadOrCreate returned error: %v", err)
	}
	raw, err := store.InspectPreview()
	if err != nil {
		t.Fatalf("InspectPreview returned error: %v", err)
	}
	var payload struct {
		TelemetryEnabled bool `json:"telemetry_enabled"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if payload.TelemetryEnabled {
		t.Fatal("telemetry_enabled = true, want false with DO_NOT_TRACK")
	}
}

func TestStore_Reset_RotatesID(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	statePath := filepath.Join(root, "telemetry", "state.json")
	store := Store{
		StatePath: statePath,
		Now: func() time.Time {
			return time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)
		},
		Rand: bytes.NewReader([]byte{
			0x01, 0x02, 0x03, 0x04,
			0x0a, 0x0b, 0x0c, 0x0d,
		}),
	}

	initial, err := store.LoadOrCreate()
	if err != nil {
		t.Fatalf("LoadOrCreate returned error: %v", err)
	}
	reset, err := store.Reset()
	if err != nil {
		t.Fatalf("Reset returned error: %v", err)
	}
	if reset.AnonymousInstallID == initial.AnonymousInstallID {
		t.Fatalf("anonymous_install_id did not rotate: %q", reset.AnonymousInstallID)
	}
	if reset.AnonymousInstallID != "anon_0a0b0c0d" {
		t.Fatalf("anonymous_install_id = %q, want %q", reset.AnonymousInstallID, "anon_0a0b0c0d")
	}
	if reset.NoticeShown {
		t.Fatal("notice_shown = true after reset, want false")
	}
}

func TestStore_MarkNoticeShown_PersistsState(t *testing.T) {
	t.Parallel()

	statePath := filepath.Join(t.TempDir(), "telemetry", "state.json")
	store := Store{
		StatePath: statePath,
		Now:       func() time.Time { return time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC) },
		Rand:      bytes.NewReader([]byte{0x01, 0x02, 0x03, 0x04}),
	}
	state, err := store.LoadOrCreate()
	if err != nil {
		t.Fatalf("LoadOrCreate returned error: %v", err)
	}
	if state.NoticeShown {
		t.Fatal("notice_shown = true before mark, want false")
	}
	updated, err := store.MarkNoticeShown()
	if err != nil {
		t.Fatalf("MarkNoticeShown returned error: %v", err)
	}
	if !updated.NoticeShown {
		t.Fatal("notice_shown = false after mark, want true")
	}
	reloaded, err := store.LoadOrCreate()
	if err != nil {
		t.Fatalf("LoadOrCreate returned error: %v", err)
	}
	if !reloaded.NoticeShown {
		t.Fatal("notice_shown = false after reload, want true")
	}
}

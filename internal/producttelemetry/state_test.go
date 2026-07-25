package producttelemetry

import (
	"path/filepath"
	"testing"
	"time"
)

func TestStore_LoadOrCreateIdentity_PersistsDefaults(t *testing.T) {
	t.Parallel()

	identityPath := filepath.Join(t.TempDir(), "telemetry")
	now := func() time.Time { return time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC) }
	st := store{dir: identityPath, now: now}

	id, err := st.loadOrCreateIdentity()
	if err != nil {
		t.Fatalf("loadOrCreateIdentity returned error: %v", err)
	}
	// Preference defaults to enabled: no preference document exists on a fresh store.
	if pref, _ := st.preference(); !pref.Enabled {
		t.Fatal("preference = false on fresh store, want true")
	}
	if len(id.InstallID) != 32 {
		t.Fatalf("install id = %q (%d chars), want 32 hex", id.InstallID, len(id.InstallID))
	}
	if id.FirstSeenAt != "2026-04-27T12:00:00Z" {
		t.Fatalf("first_seen_at = %q, want %q", id.FirstSeenAt, "2026-04-27T12:00:00Z")
	}

	// identity.json is write-once: a fresh store reads the same identity.
	fresh := store{dir: identityPath, now: func() time.Time { return time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC) }}
	again, err := fresh.loadOrCreateIdentity()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if again.InstallID != id.InstallID || again.FirstSeenAt != id.FirstSeenAt {
		t.Fatalf("identity changed across reload: got %+v, want %+v", again, id)
	}
}

func TestStore_SetEnabled_PersistsToggle(t *testing.T) {
	t.Parallel()

	identityPath := filepath.Join(t.TempDir(), "telemetry")
	st := store{dir: identityPath, now: func() time.Time { return time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC) }}
	if err := st.setEnabled(false); err != nil {
		t.Fatalf("setEnabled(false): %v", err)
	}
	if pref, _ := st.preference(); pref.Enabled {
		t.Fatal("preference = true after opt-out, want false")
	}
	// A fresh store reading the same directory sees the persisted opt-out: the
	// preference document survives process boundaries (the CLI persists, the next
	// daemon start reads it).
	if pref, _ := (store{dir: identityPath}).preference(); pref.Enabled {
		t.Fatal("fresh store did not see the persisted preference document")
	}

	if err := st.setEnabled(true); err != nil {
		t.Fatalf("setEnabled(true): %v", err)
	}
	if pref, _ := st.preference(); !pref.Enabled {
		t.Fatal("preference = false after opt-in, want true")
	}
}

func TestStore_ClaimNotice_PersistsMarker(t *testing.T) {
	t.Parallel()

	identityPath := filepath.Join(t.TempDir(), "telemetry")
	st := store{dir: identityPath, now: func() time.Time { return time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC) }}
	claimed, err := st.claimNotice()
	if err != nil || !claimed {
		t.Fatalf("first claimNotice = (%v, %v), want (true, nil)", claimed, err)
	}
	// A second claim loses: the marker already exists, so it returns false — the
	// race-free property the old check-then-act flow lacked (two concurrent
	// callers cannot both win and print twice).
	claimed, err = st.claimNotice()
	if err != nil || claimed {
		t.Fatalf("second claimNotice = (%v, %v), want (false, nil)", claimed, err)
	}
}

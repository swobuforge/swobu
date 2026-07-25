package producttelemetry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Preference is independent of the identity document: a corrupt identity.json
// cannot be decoded (loadOrCreateIdentity errors), but preference reads only the
// preference document, so a corrupt identity does not flip the preference.
func TestStore_CorruptIdentityDoesNotAffectPreference(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "telemetry")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "identity.json"), []byte("{not-json"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	st := store{dir: dir}
	pref, err := st.preference()
	if err != nil || !pref.Enabled {
		t.Fatalf("preference = (%v, %v) with corrupt identity and no preference doc, want (true, nil)", pref.Enabled, err)
	}
	if _, err := st.loadOrCreateIdentity(); err == nil {
		t.Fatal("loadOrCreateIdentity returned nil for corrupt identity")
	} else if !strings.Contains(err.Error(), "decode telemetry identity") {
		t.Fatalf("error = %q, want decode context", err.Error())
	}
}

// preference fails closed when the preference document cannot be read safely.
func TestStore_PreferenceFailsClosedOnUnreadableDir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission-based fail-closed is not exercisable as root")
	}
	dir := filepath.Join(t.TempDir(), "telemetry")
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.Chmod(filepath.Dir(dir), 0o000); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Dir(dir), 0o755) })

	st := store{dir: dir}
	if _, err := st.preference(); err == nil {
		t.Fatal("preference returned nil error when the preference directory is unreadable, want fail-closed error")
	}
}

// The pseudonymous install id is generated on first creation and stable across
// reloads, and identity.json is never rewritten.
func TestStore_LoadOrCreateIdentity_GeneratesStableInstallID(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "telemetry")
	st := store{dir: dir}

	first, err := st.loadOrCreateIdentity()
	if err != nil {
		t.Fatalf("loadOrCreateIdentity: %v", err)
	}
	if len(first.InstallID) != 32 {
		t.Fatalf("install id = %q (%d chars), want 32 hex", first.InstallID, len(first.InstallID))
	}
	firstSeen := first.FirstSeenAt

	reloaded, err := st.loadOrCreateIdentity()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.InstallID != first.InstallID || reloaded.FirstSeenAt != firstSeen {
		t.Fatal("identity changed across reload; identity.json is not write-once")
	}
}

// Milestone writes (notice) cannot affect the preference: opt out, claim the
// notice, and the preference document must remain disabled.
func TestStore_NoticeMarkCannotAffectPreference(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "telemetry")
	st := store{dir: dir, now: func() time.Time { return time.Unix(1000, 0).UTC() }}
	if err := st.setEnabled(false); err != nil {
		t.Fatalf("setEnabled(false): %v", err)
	}
	if _, err := st.claimNotice(); err != nil {
		t.Fatalf("claimNotice: %v", err)
	}
	if pref, _ := st.preference(); pref.Enabled {
		t.Fatal("notice mark changed the preference; opt-out was lost")
	}
}

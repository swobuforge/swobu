package producttelemetry

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	platformconfig "github.com/swobuforge/swobu/internal/platform/config"
)

// identity is the write-once installation identity: a pseudonymous install id and
// the first-seen timestamp. It is created once (identity.json) and never
// rewritten. The preference and notice facts live in separate files, so no other
// operation performs read-modify-write on the identity document.
//
// InstallID is 128 random bits generated locally (crypto/rand), unrelated to
// machine identifiers, hostname, OS account, or credentials. It is the single
// persistent product-telemetry identifier and is described as pseudonymous, never
// anonymous. It leaves the device only inside the closed product report (see
// product-telemetry.md). A developer who wants a fresh identity deletes the
// telemetry state directory; there is no reset command (rotating the id does not
// delete backend data, so it is not a meaningful privacy control).
type identity struct {
	InstallID   string `json:"install_id"`
	FirstSeenAt string `json:"first_seen_at"`
}

// store owns the on-disk telemetry facts under one directory. It is an internal
// capability: production callers use the package-level Status / SetEnabled /
// ClaimNotice / StartRuntime, which own the store. Tests in this package construct
// a store directly to point at a temp directory.
//
//	telemetry/
//	  identity.json     write-once install id + first_seen_at
//	  preference.json   opt-out preference { enabled, revision } (atomic replace)
//	  notice_shown      first-run notice marker (created once)
//
// The preference is a full-document atomic replacement; the notice marker is
// create-if-absent. Concurrent writers (a CLI and a daemon) can never overwrite
// or lose each other's facts. See product-telemetry.md.
type store struct {
	dir string
	now func() time.Time
}

func newStore() store {
	return store{dir: strings.TrimSpace(platformconfig.DefaultTelemetryStateDir()), now: time.Now}
}

func (s store) identityPath() string    { return filepath.Join(s.dir, "identity.json") }
func (s store) preferencePath() string  { return filepath.Join(s.dir, "preference.json") }
func (s store) noticeShownPath() string { return filepath.Join(s.dir, "notice_shown") }

func (s store) ensureDir() error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("create telemetry state dir: %w", err)
	}
	return nil
}

// loadOrCreateIdentity returns the write-once installation identity, creating it
// on first use. The identity document is created exactly once via atomic exclusive
// creation; concurrent first-callers race to create it and losers read the
// winner's document instead of overwriting. It is never rewritten.
func (s store) loadOrCreateIdentity() (identity, error) {
	path := s.identityPath()
	if data, err := os.ReadFile(path); err == nil {
		var id identity
		if err := json.Unmarshal(data, &id); err != nil {
			return identity{}, fmt.Errorf("decode telemetry identity: %w", err)
		}
		if err := validateIdentity(id); err != nil {
			return identity{}, err
		}
		return id, nil
	} else if !os.IsNotExist(err) {
		return identity{}, fmt.Errorf("read telemetry identity: %w", err)
	}

	if err := s.ensureDir(); err != nil {
		return identity{}, err
	}
	installID, err := newToken()
	if err != nil {
		return identity{}, fmt.Errorf("telemetry install id unavailable: %w", err)
	}
	// now is nil when a store is constructed without a clock (some tests); fall
	// back to the wall clock. The field cannot share its name with a method, so the
	// guard is inline at the single read site.
	now := s.now
	if now == nil {
		now = time.Now
	}
	id := identity{InstallID: installID, FirstSeenAt: now().UTC().Format(time.RFC3339)}
	body, err := json.MarshalIndent(id, "", "  ")
	if err != nil {
		return identity{}, fmt.Errorf("encode telemetry identity: %w", err)
	}
	if err := createExclusive(path, append(body, '\n')); err != nil {
		if errors.Is(err, os.ErrExist) {
			// A concurrent creator won; return its identity (validated).
			if data, rerr := os.ReadFile(path); rerr == nil {
				var existing identity
				if jerr := json.Unmarshal(data, &existing); jerr == nil {
					if verr := validateIdentity(existing); verr == nil {
						return existing, nil
					}
				}
			}
		}
		return identity{}, fmt.Errorf("create telemetry identity: %w", err)
	}
	return id, nil
}

// hexTokenPattern matches the 32-lowercase-hex-character tokens used for the
// install id and the preference revision.
var hexTokenPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// validateIdentity checks facts that cross the file boundary: the install id is
// exactly 32 lowercase hex characters (so a report carries a value the Worker
// accepts), and first_seen_at is RFC3339 (so activation timing is not silently
// lost). A corrupt identity fails closed rather than producing rejected reports.
func validateIdentity(id identity) error {
	if !hexTokenPattern.MatchString(id.InstallID) {
		return fmt.Errorf("telemetry identity install_id is not 32 lowercase hex characters")
	}
	if _, err := time.Parse(time.RFC3339, id.FirstSeenAt); err != nil {
		return fmt.Errorf("telemetry identity first_seen_at is not RFC3339: %w", err)
	}
	return nil
}

// preference is the mutable opt-out document. Each setEnabled writes a complete
// new document with a fresh revision via atomic replacement (no read-modify-write
// of any other field), so concurrent writers cannot lose or tear it. The runtime
// holds the revision under which its reducer's aggregate started and, at the
// upload boundary, discards any aggregate whose revision no longer matches — so an
// off/on/off sequence the daemon never observed in flight still results in the old
// aggregate being discarded before upload. See product-telemetry.md §6.
type preference struct {
	Enabled  bool   `json:"enabled"`
	Revision string `json:"revision"`
}

// setEnabled writes a complete preference document with a fresh revision. It is a
// full atomic replacement, so it never partially overlaps a notice or identity
// write; the revision lets the runtime detect that the preference changed.
func (s store) setEnabled(enabled bool) error {
	if err := s.ensureDir(); err != nil {
		return err
	}
	revision, err := newToken()
	if err != nil {
		return fmt.Errorf("telemetry preference revision unavailable: %w", err)
	}
	body, err := json.MarshalIndent(preference{Enabled: enabled, Revision: revision}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode telemetry preference: %w", err)
	}
	return atomicReplace(s.preferencePath(), append(body, '\n'))
}

// preference returns the persisted opt-out document — storage truth only, no
// process-policy composition. An absent document is the only state allowed to
// yield the implicit initial preference (enabled, empty revision); an existing
// document with an empty or malformed revision is corrupt and fails closed, so it
// cannot defeat the revision-discard property. Callers compose this with
// DO_NOT_TRACK (see Status); the store does not read the environment.
func (s store) preference() (preference, error) {
	data, err := os.ReadFile(s.preferencePath())
	if err != nil {
		if os.IsNotExist(err) {
			return preference{Enabled: true, Revision: ""}, nil
		}
		return preference{}, fmt.Errorf("read telemetry preference: %w", err)
	}
	var p preference
	if err := json.Unmarshal(data, &p); err != nil {
		return preference{}, fmt.Errorf("decode telemetry preference: %w", err)
	}
	if !hexTokenPattern.MatchString(p.Revision) {
		return preference{}, fmt.Errorf("telemetry preference revision is not 32 lowercase hex characters (corrupt preference document)")
	}
	return p, nil
}

// claimNotice atomically claims the first-run notice. It returns true when this
// call created the marker, false when it already existed, and any other error.
// The O_CREATE|O_EXCL create is the single atomic step, so concurrent processes
// (a CLI and a daemon) cannot both observe "not yet shown" and print twice.
func (s store) claimNotice() (bool, error) {
	if err := s.ensureDir(); err != nil {
		return false, err
	}
	f, err := os.OpenFile(s.noticeShownPath(), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("write telemetry notice marker: %w", err)
	}
	return true, f.Close()
}

func DoNotTrackEnabled() bool {
	raw := os.Getenv(platformconfig.EnvDoNotTrack)
	return platformconfig.EnvTruthy(raw)
}

// TelemetryStatus is the effective opt-out state for display: the persisted
// preference and the process-level DO_NOT_TRACK flag. The caller sees why upload
// is currently disabled (either signal) without the store silently folding
// environment, corruption, and preference into one bool.
type TelemetryStatus struct {
	Enabled    bool // persisted preference
	DoNotTrack bool // process environment (DO_NOT_TRACK)
}

// Status reads the persisted preference and the process DO_NOT_TRACK flag. It
// returns storage truth and any read error; it does not swallow corruption as
// "disabled." Callers compose the two signals.
func Status() (TelemetryStatus, error) { return status(newStore()) }

// status is the store-parameterized form; Status runs it against the default
// telemetry state directory, and tests run it against a temp-dir store.
func status(s store) (TelemetryStatus, error) {
	pref, err := s.preference()
	if err != nil {
		return TelemetryStatus{}, err
	}
	return TelemetryStatus{Enabled: pref.Enabled, DoNotTrack: DoNotTrackEnabled()}, nil
}

// SetEnabled persists the opt-out preference and returns the requested persisted
// value. It does not reread the document (avoiding a lossy re-composition): the
// persisted value is exactly what was written.
func SetEnabled(enabled bool) (bool, error) {
	if err := newStore().setEnabled(enabled); err != nil {
		return false, err
	}
	return enabled, nil
}

// ClaimNotice atomically claims the first-run notice against the default
// telemetry state directory.
func ClaimNotice() (bool, error) {
	return newStore().claimNotice()
}

// atomicReplace writes content to target via a uniquely-named temp file then
// rename, so target is never observed partially written. Last writer wins; each
// write is complete, so concurrent writers cannot tear the document. The temp is
// always removed (a no-op after a successful rename).
func atomicReplace(target string, content []byte) error {
	dir := filepath.Dir(target)
	f, err := os.CreateTemp(dir, filepath.Base(target)+".*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.Write(content); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, target)
}

// newToken returns a fresh 128-bit token as 32 lowercase hex characters — used for
// both the install id and the preference revision. Callers must treat "" (entropy
// failure) as a fatal, fail-closed condition.
func newToken() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// createExclusive atomically creates target containing content. It writes a
// uniquely-named temp file (os.CreateTemp, so concurrent writers never collide on
// a temp name) then links it to target; os.Link fails if target already exists,
// so the first caller wins and concurrent losers get os.ErrExist (and read the
// existing fact).
func createExclusive(target string, content []byte) error {
	dir := filepath.Dir(target)
	pattern := filepath.Base(target) + ".*.tmp"
	f, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return err
	}
	tmp := f.Name()
	if _, err := f.Write(content); err != nil {
		f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	linkErr := os.Link(tmp, target)
	_ = os.Remove(tmp) // best-effort temp cleanup
	if linkErr != nil {
		if os.IsExist(linkErr) {
			return os.ErrExist
		}
		return linkErr
	}
	return nil
}

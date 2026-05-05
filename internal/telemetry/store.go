package telemetry

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/swobuforge/swobu/internal/app/operator/controlplane"
	platformconfig "github.com/swobuforge/swobu/internal/platform/config"
)

type State struct {
	Enabled            bool   `json:"enabled"`
	AnonymousInstallID string `json:"anonymous_install_id"`
	FirstSeenAt        string `json:"first_seen_at"`
	NoticeShown        bool   `json:"notice_shown"`
	LastUploadAt       string `json:"last_upload_at,omitempty"`
}

type Store struct {
	StatePath string
	Now       func() time.Time
	Rand      io.Reader
}

func NewStore() Store {
	return Store{
		StatePath: defaultStatePath(),
		Now:       time.Now,
		Rand:      rand.Reader,
	}
}

func (s Store) LoadOrCreate() (State, error) {
	path := strings.TrimSpace(s.StatePath)
	if path == "" {
		path = defaultStatePath()
	}
	now := s.Now
	if now == nil {
		now = time.Now
	}
	data, err := os.ReadFile(path)
	if err == nil {
		var state State
		if err := json.Unmarshal(data, &state); err != nil {
			return State{}, fmt.Errorf("decode telemetry state: %w", err)
		}
		if strings.TrimSpace(state.AnonymousInstallID) == "" {
			state.AnonymousInstallID = newAnonymousInstallID(s.Rand, now)
		}
		if strings.TrimSpace(state.FirstSeenAt) == "" {
			state.FirstSeenAt = now().UTC().Format(time.RFC3339)
		}
		return state, nil
	}
	if !os.IsNotExist(err) {
		return State{}, fmt.Errorf("read telemetry state: %w", err)
	}

	state := State{
		Enabled:            true,
		AnonymousInstallID: newAnonymousInstallID(s.Rand, now),
		FirstSeenAt:        now().UTC().Format(time.RFC3339),
		NoticeShown:        false,
	}
	if err := writeState(path, state); err != nil {
		return State{}, err
	}
	return state, nil
}

func (s Store) SetEnabled(enabled bool) (State, error) {
	state, err := s.LoadOrCreate()
	if err != nil {
		return State{}, err
	}
	state.Enabled = enabled
	if err := writeState(s.StatePath, state); err != nil {
		return State{}, err
	}
	return state, nil
}

func (s Store) Reset() (State, error) {
	path := strings.TrimSpace(s.StatePath)
	if path == "" {
		path = defaultStatePath()
	}
	now := s.Now
	if now == nil {
		now = time.Now
	}

	enabled := true
	if existing, err := s.LoadOrCreate(); err == nil {
		enabled = existing.Enabled
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return State{}, fmt.Errorf("remove telemetry state: %w", err)
	}
	state := State{
		Enabled:            enabled,
		AnonymousInstallID: newAnonymousInstallID(s.Rand, now),
		FirstSeenAt:        now().UTC().Format(time.RFC3339),
		NoticeShown:        false,
	}
	if err := writeState(path, state); err != nil {
		return State{}, err
	}
	return state, nil
}

func (s Store) MarkNoticeShown() (State, error) {
	state, err := s.LoadOrCreate()
	if err != nil {
		return State{}, err
	}
	if state.NoticeShown {
		return state, nil
	}
	state.NoticeShown = true
	if err := writeState(s.StatePath, state); err != nil {
		return State{}, err
	}
	return state, nil
}

func (s Store) InspectPreview() ([]byte, error) {
	state, err := s.LoadOrCreate()
	if err != nil {
		return nil, err
	}
	enabled := state.Enabled
	if isDoNotTrackEnabled() {
		enabled = false
	}
	preview := struct {
		SchemaVersion      int    `json:"schema_version"`
		Kind               string `json:"kind"`
		AnonymousInstallID string `json:"anonymous_install_id"`
		SwobuVersion       string `json:"swobu_version"`
		OS                 string `json:"os"`
		Arch               string `json:"arch"`
		TelemetryEnabled   bool   `json:"telemetry_enabled"`
	}{
		SchemaVersion:      1,
		Kind:               "install_summary",
		AnonymousInstallID: state.AnonymousInstallID,
		SwobuVersion:       controlplane.SwobuVersion(),
		OS:                 runtime.GOOS,
		Arch:               runtime.GOARCH,
		TelemetryEnabled:   enabled,
	}
	out, err := json.Marshal(preview)
	if err != nil {
		return nil, fmt.Errorf("marshal telemetry preview: %w", err)
	}
	return out, nil
}

func defaultStatePath() string {
	return platformconfig.ResolveTelemetryStatePath("")
}

func writeState(path string, state State) error {
	path = strings.TrimSpace(path)
	if path == "" {
		path = defaultStatePath()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create telemetry state dir: %w", err)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode telemetry state: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write telemetry state temp file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("move telemetry state file: %w", err)
	}
	return nil
}

func newAnonymousInstallID(r io.Reader, now func() time.Time) string {
	if r == nil {
		r = rand.Reader
	}
	if now == nil {
		now = time.Now
	}
	buf := make([]byte, 4)
	if _, err := io.ReadFull(r, buf); err == nil {
		return fmt.Sprintf("anon_%x", buf)
	}
	return fmt.Sprintf("anon_%x", now().UnixNano())
}

func isDoNotTrackEnabled() bool {
	return platformconfig.EnvTruthy(os.Getenv(platformconfig.EnvDoNotTrack))
}

func DoNotTrackEnabled() bool {
	return isDoNotTrackEnabled()
}

func (s Store) isTelemetryEnabled() bool {
	if isDoNotTrackEnabled() {
		return false
	}
	state, err := s.LoadOrCreate()
	if err != nil {
		return false
	}
	return state.Enabled
}

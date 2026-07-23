package telemetry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/swobuforge/swobu/internal/app/operator/controlplane"
	platformconfig "github.com/swobuforge/swobu/internal/platform/config"
)

// State carries only the operator's own preference and a first-run notice flag.
// It holds no install identifier (D1): nothing in this struct is transmitted,
// and no persistent pseudonymous id is ever generated.
type State struct {
	Enabled      bool   `json:"enabled"`
	FirstSeenAt  string `json:"first_seen_at"`
	NoticeShown  bool   `json:"notice_shown"`
	LastUploadAt string `json:"last_upload_at,omitempty"`
}

type Store struct {
	StatePath string
	Now       func() time.Time
}

func NewStore() Store {
	return Store{
		StatePath: platformconfig.DefaultTelemetryStatePath(),
		Now:       time.Now,
	}
}

func (s Store) LoadOrCreate() (State, error) {
	path := strings.TrimSpace(s.StatePath) // swobu:io-string source=boundary
	if path == "" {
		path = platformconfig.DefaultTelemetryStatePath()
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
		if strings.TrimSpace(state.FirstSeenAt) == "" { // swobu:io-string source=boundary
			state.FirstSeenAt = now().UTC().Format(time.RFC3339)
		}
		return state, nil
	}
	if !os.IsNotExist(err) {
		return State{}, fmt.Errorf("read telemetry state: %w", err)
	}

	state := State{
		Enabled:     true,
		FirstSeenAt: now().UTC().Format(time.RFC3339),
		NoticeShown: false,
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
	path := strings.TrimSpace(s.StatePath) // swobu:io-string source=boundary
	if path == "" {
		path = platformconfig.DefaultTelemetryStatePath()
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
		Enabled:     enabled,
		FirstSeenAt: now().UTC().Format(time.RFC3339),
		NoticeShown: false,
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
	if DoNotTrackEnabled() {
		enabled = false
	}
	preview := struct {
		SchemaVersion    int    `json:"schema_version"`
		Kind             string `json:"kind"`
		SwobuVersion     string `json:"swobu_version"`
		OS               string `json:"os"`
		Arch             string `json:"arch"`
		TelemetryEnabled bool   `json:"telemetry_enabled"`
	}{
		SchemaVersion:    1,
		Kind:             "install_summary",
		SwobuVersion:     controlplane.SwobuVersion(),
		OS:               runtime.GOOS,
		Arch:             runtime.GOARCH,
		TelemetryEnabled: enabled,
	}
	out, err := json.Marshal(preview)
	if err != nil {
		return nil, fmt.Errorf("marshal telemetry preview: %w", err)
	}
	return out, nil
}

func writeState(path string, state State) error {
	path = strings.TrimSpace(path) // swobu:io-string source=boundary
	if path == "" {
		path = platformconfig.DefaultTelemetryStatePath()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
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

func DoNotTrackEnabled() bool {
	raw := os.Getenv(platformconfig.EnvDoNotTrack)
	return platformconfig.EnvTruthy(raw)
}

func (s Store) isTelemetryEnabled() bool {
	if DoNotTrackEnabled() {
		return false
	}
	state, err := s.LoadOrCreate()
	if err != nil {
		return false
	}
	return state.Enabled
}

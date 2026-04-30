package telemetry

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
)

// Emitter is the telemetry runtime sink contract used by bootstrap.
type Emitter interface {
	Shutdown(context.Context) error
	EmitInstall(context.Context, State, string, string, string)
	EmitCounts(context.Context, string, int64, int64, int64, int64)
}

type stdoutEmitter struct {
	out io.Writer
	mu  sync.Mutex
}

func NewStdoutEmitter(out io.Writer) Emitter {
	if out == nil {
		out = io.Discard
	}
	return &stdoutEmitter{out: out}
}

func (e *stdoutEmitter) Shutdown(context.Context) error { return nil }

func (e *stdoutEmitter) EmitInstall(_ context.Context, state State, swobuVersion, osFamily, arch string) {
	e.write(map[string]any{
		"telemetry_debug":   true,
		"kind":              "install",
		"swobu_version":     strings.TrimSpace(swobuVersion),
		"os":                strings.TrimSpace(osFamily),
		"arch":              strings.TrimSpace(arch),
		"telemetry_enabled": state.Enabled && !DoNotTrackEnabled(),
	})
}

func (e *stdoutEmitter) EmitCounts(_ context.Context, state string, count2xx, count429, count4xx, count5xx int64) {
	e.write(map[string]any{
		"telemetry_debug": true,
		"kind":            "counts",
		"state":           strings.TrimSpace(state),
		"count_2xx":       count2xx,
		"count_429":       count429,
		"count_4xx":       count4xx,
		"count_5xx":       count5xx,
	})
}

func (e *stdoutEmitter) write(payload map[string]any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	_ = json.NewEncoder(e.out).Encode(payload)
}

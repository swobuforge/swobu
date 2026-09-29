package ndjson

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/swobuforge/swobu/internal/domain/executionevidence"
	"github.com/swobuforge/swobu/internal/outputruntime"
)

const (
	DefaultQueueCapacity = 4096
	DefaultBatchSize     = 64
	DefaultBatchWait     = 100 * time.Millisecond
	DefaultDrainBudget   = 5 * time.Second
)

type Config struct {
	Path          string
	QueueCapacity int
	BatchSize     int
	BatchWait     time.Duration
	DrainBudget   time.Duration
}

type State string

const (
	StateHealthy  State = "healthy"
	StateDegraded State = "degraded"
)

type Health struct {
	State         State                   `json:"state"`
	QueueDepth    int                     `json:"queue_depth"`
	QueueCapacity int                     `json:"queue_capacity"`
	Offered       uint64                  `json:"offered"`
	Exported      uint64                  `json:"exported"`
	Dropped       uint64                  `json:"dropped"`
	LastError     outputruntime.ErrorCode `json:"last_error,omitempty"`
}

type Exporter struct {
	path                       string
	worker                     *outputruntime.Worker[executionevidence.CompletedExchange]
	mu                         sync.RWMutex
	state                      State
	lastError                  outputruntime.ErrorCode
	offered, exported, dropped atomic.Uint64
}

func New(cfg Config) (*Exporter, error) {
	if cfg.Path == "" || filepath.IsAbs(cfg.Path) == false {
		return nil, errors.New("NDJSON path must be absolute")
	}
	if cfg.QueueCapacity <= 0 {
		cfg.QueueCapacity = DefaultQueueCapacity
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = DefaultBatchSize
	}
	if cfg.BatchWait <= 0 {
		cfg.BatchWait = DefaultBatchWait
	}
	if cfg.DrainBudget <= 0 {
		cfg.DrainBudget = DefaultDrainBudget
	}
	if err := repairUnterminatedTail(cfg.Path); err != nil {
		return nil, err
	}
	e := &Exporter{path: cfg.Path, state: StateHealthy}
	w, err := outputruntime.NewWorker(outputruntime.WorkerConfig[executionevidence.CompletedExchange]{
		Capacity: cfg.QueueCapacity, Batch: cfg.BatchSize, BatchWait: cfg.BatchWait, Drain: cfg.DrainBudget,
		Handle: func(_ context.Context, batch []executionevidence.CompletedExchange) error { e.write(batch); return nil },
		OnDrop: func(batch []executionevidence.CompletedExchange, _ outputruntime.DropReason) {
			e.recordDroppedBatch(batch)
			e.fail(outputruntime.ErrorExportFailed)
		},
	})
	if err != nil {
		return nil, err
	}
	e.worker = w
	return e, nil
}

func (e *Exporter) Offer(exchange executionevidence.CompletedExchange) bool {
	e.offered.Add(1)
	if !e.worker.Offer(exchange) {
		e.dropped.Add(1)
		e.fail(outputruntime.ErrorQueueOverflow)
		return false
	}
	return true
}
func (e *Exporter) Close() error {
	e.worker.Close()
	return nil
}
func (e *Exporter) Health() Health {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return Health{State: e.state, QueueDepth: e.worker.Depth(), QueueCapacity: e.worker.Capacity(), Offered: e.offered.Load(), Exported: e.exported.Load(), Dropped: e.dropped.Load(), LastError: e.lastError}
}
func (e *Exporter) write(batch []executionevidence.CompletedExchange) {
	if len(batch) == 0 {
		return
	}
	lines := make([][]byte, 0, len(batch))
	for _, x := range batch {
		line, err := MarshalExchange(x)
		if err != nil {
			e.recordDroppedBatch(batch)
			e.fail(outputruntime.ErrorExportFailed)
			return
		}
		lines = append(lines, append(line, '\n'))
	}
	for attempt := 0; ; attempt++ {
		if err := e.appendLines(lines); err == nil {
			break
		} else if attempt >= 2 {
			e.recordDroppedBatch(batch)
			e.fail(outputruntime.ErrorExportFailed)
			return
		}
		e.fail(outputruntime.ErrorExportFailed)
		timer := time.NewTimer(time.Duration(attempt+1) * 25 * time.Millisecond)
		<-timer.C
	}
	e.exported.Add(uint64(len(batch)))
	e.mu.Lock()
	e.state = StateHealthy
	e.lastError = ""
	e.mu.Unlock()
}
func (e *Exporter) appendLines(lines [][]byte) error {
	if err := os.MkdirAll(filepath.Dir(e.path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(e.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if err := writeAll(f, bytes.Join(lines, nil)); err != nil {
		_ = f.Truncate(info.Size())
		return err
	}
	return nil
}

func writeAll(f *os.File, data []byte) error {
	for len(data) > 0 {
		n, err := f.Write(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return errors.New("NDJSON append made no progress")
		}
		data = data[n:]
	}
	return nil
}

func repairUnterminatedTail(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		if info, statErr := os.Stat(path); statErr == nil && info.IsDir() {
			return nil
		}
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.IsDir() || info.Size() == 0 {
		if info.IsDir() {
			return nil
		}
		return err
	}
	last := []byte{0}
	if _, err := f.ReadAt(last, info.Size()-1); err != nil {
		return err
	}
	if last[0] == '\n' {
		return nil
	}
	const chunkSize = int64(64 << 10)
	for end := info.Size(); end > 0; {
		start := end - chunkSize
		if start < 0 {
			start = 0
		}
		chunk := make([]byte, end-start)
		if _, err := f.ReadAt(chunk, start); err != nil {
			return err
		}
		if index := bytes.LastIndexByte(chunk, '\n'); index >= 0 {
			return f.Truncate(start + int64(index) + 1)
		}
		end = start
	}
	return f.Truncate(0)
}

func (e *Exporter) recordDroppedBatch(batch []executionevidence.CompletedExchange) {
	e.dropped.Add(uint64(len(batch)))
}
func (e *Exporter) fail(code outputruntime.ErrorCode) {
	e.mu.Lock()
	e.state = StateDegraded
	e.lastError = code
	e.mu.Unlock()
}

type exchangeRecord struct {
	SchemaVersion int             `json:"schema_version"`
	Type          string          `json:"type"`
	Exchange      exchangeDTO     `json:"exchange"`
	Invocations   []invocationDTO `json:"invocations"`
}
type exchangeDTO struct {
	ID             string `json:"exchange_id"`
	AccessID       string `json:"access_id"`
	Workspace      string `json:"workspace_slug"`
	StartedAt      string `json:"started_at"`
	SettledAt      string `json:"settled_at"`
	DurationNS     int64  `json:"duration_ns"`
	ClientFamily   string `json:"client_family"`
	ClientProtocol string `json:"client_protocol"`
	Operation      string `json:"normalized_operation"`
	Result         string `json:"delivery_result"`
	Status         int    `json:"client_status_code,omitempty"`
	ErrorCode      string `json:"canonical_error_code,omitempty"`
}
type invocationDTO struct {
	ID         string `json:"invocation_id"`
	Ordinal    uint64 `json:"ordinal"`
	Kind       string `json:"kind"`
	StartedAt  string `json:"started_at"`
	SettledAt  string `json:"settled_at"`
	DurationNS int64  `json:"duration_ns"`
	Outcome    string `json:"outcome"`
	Occurrence string `json:"occurrence"`
	Failure    string `json:"failure_class,omitempty"`
	Resource   any    `json:"resource"`
}
type modelResourceDTO struct {
	Route            string `json:"route_name,omitempty"`
	Target           string `json:"target_id"`
	TargetVersion    string `json:"target_version"`
	Provider         string `json:"provider_spec"`
	Protocol         string `json:"provider_protocol"`
	Model            string `json:"provider_model"`
	Purpose          string `json:"purpose"`
	InputTokens      *int   `json:"input_tokens,omitempty"`
	OutputTokens     *int   `json:"output_tokens,omitempty"`
	ReasoningTokens  *int   `json:"reasoning_tokens,omitempty"`
	CacheReadTokens  *int   `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens *int   `json:"cache_write_tokens,omitempty"`
}
type mcpResourceDTO struct {
	Source string `json:"source_tool_key"`
	Tool   string `json:"tool_key"`
}

func MarshalExchange(x executionevidence.CompletedExchange) ([]byte, error) {
	r := exchangeRecord{SchemaVersion: 1, Type: "exchange", Exchange: exchangeDTO{ID: x.ID().String(), AccessID: x.Started().AccessID().String(), Workspace: x.Started().Workspace().String(), StartedAt: x.Started().StartedAt().UTC().Format(time.RFC3339Nano), SettledAt: x.Settled().SettledAt().UTC().Format(time.RFC3339Nano), DurationNS: x.Duration().Nanoseconds(), ClientFamily: string(x.Started().ClientFamily()), ClientProtocol: x.Started().ClientProtocol().String(), Operation: string(x.Started().Operation()), Result: string(x.Settled().Result()), Status: x.Settled().Status(), ErrorCode: string(x.Settled().ErrorCode())}}
	for _, i := range x.Invocations() {
		d := invocationDTO{ID: i.Started().ID().String(), Ordinal: i.Started().Ordinal(), StartedAt: i.Started().StartedAt().UTC().Format(time.RFC3339Nano), SettledAt: i.Settled().SettledAt().UTC().Format(time.RFC3339Nano), DurationNS: i.Duration().Nanoseconds(), Outcome: string(i.Settled().Outcome()), Occurrence: string(i.Settled().Occurrence())}
		switch s := i.Started().(type) {
		case executionevidence.ModelInvocationStarted:
			t := i.Settled().(executionevidence.ModelInvocationSettled)
			d.Kind = "model"
			d.Failure = string(t.Failure())
			u := t.Usage()
			d.Resource = modelResourceDTO{Route: s.Model().Route().String(), Target: s.Model().Target().String(), TargetVersion: fmt.Sprint(uint64(s.Model().Version())), Provider: string(s.Model().Provider()), Protocol: s.Model().Protocol().String(), Model: s.Model().Model().String(), Purpose: string(s.Model().Purpose()), InputTokens: tokenPtr(u.InputTokens()), OutputTokens: tokenPtr(u.OutputTokens()), ReasoningTokens: tokenPtr(u.ReasoningTokens()), CacheReadTokens: tokenPtr(u.CacheReadTokens()), CacheWriteTokens: tokenPtr(u.CacheWriteTokens())}
		case executionevidence.MCPInvocationStarted:
			t := i.Settled().(executionevidence.MCPInvocationSettled)
			d.Kind = "mcp_tool"
			d.Failure = string(t.Failure())
			d.Resource = mcpResourceDTO{Source: s.MCP().Source().String(), Tool: s.MCP().Tool().String()}
		default:
			return nil, errors.New("unsupported Invocation kind")
		}
		r.Invocations = append(r.Invocations, d)
	}
	return json.Marshal(r)
}
func tokenPtr(v int, known bool) *int {
	if !known {
		return nil
	}
	return &v
}

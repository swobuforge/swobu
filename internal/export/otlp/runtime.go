package otlp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	apitrace "go.opentelemetry.io/otel/trace"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/execution"
	"github.com/swobuforge/swobu/internal/domain/executionevidence"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/outputruntime"
)

const (
	DefaultQueueCapacity = 4096
	DefaultDrainBudget   = 5 * time.Second
)

type Config struct {
	QueueCapacity  int
	DrainBudget    time.Duration
	ServiceVersion string
	Endpoint       string
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
	provider                   *sdktrace.TracerProvider
	transport                  sdktrace.SpanExporter
	collector                  *collectingProcessor
	worker                     *outputruntime.Worker[executionevidence.CompletedExchange]
	drain                      time.Duration
	mu                         sync.RWMutex
	state                      State
	lastError                  outputruntime.ErrorCode
	offered, exported, dropped atomic.Uint64
}

type collectingProcessor struct{ spans []sdktrace.ReadOnlySpan }

func (p *collectingProcessor) OnStart(context.Context, sdktrace.ReadWriteSpan) {}
func (p *collectingProcessor) OnEnd(span sdktrace.ReadOnlySpan)                { p.spans = append(p.spans, span) }
func (p *collectingProcessor) Shutdown(context.Context) error                  { return nil }
func (p *collectingProcessor) ForceFlush(context.Context) error                { return nil }

// New receives validated bootstrap configuration and does not inspect ambient
// environment variables.
func New(ctx context.Context, cfg Config) (*Exporter, error) {
	if strings.TrimSpace(cfg.Endpoint) == "" {
		return nil, errors.New("OTLP endpoint is required")
	}
	if cfg.QueueCapacity <= 0 {
		cfg.QueueCapacity = DefaultQueueCapacity
	}
	if cfg.DrainBudget <= 0 {
		cfg.DrainBudget = DefaultDrainBudget
	}
	exp, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(cfg.Endpoint))
	if err != nil {
		return nil, err
	}
	res, err := resource.New(ctx, resource.WithAttributes(semconv.ServiceName("swobu"), semconv.ServiceVersion(cfg.ServiceVersion)))
	if err != nil {
		_ = exp.Shutdown(ctx)
		return nil, err
	}
	result := &Exporter{drain: cfg.DrainBudget, state: StateHealthy}
	result.transport = exp
	result.collector = &collectingProcessor{}
	result.provider = sdktrace.NewTracerProvider(sdktrace.WithResource(res), sdktrace.WithSpanProcessor(result.collector))
	worker, err := outputruntime.NewWorker(outputruntime.WorkerConfig[executionevidence.CompletedExchange]{
		Capacity: cfg.QueueCapacity, Batch: 1, Drain: cfg.DrainBudget,
		Handle: func(ctx context.Context, batch []executionevidence.CompletedExchange) error {
			for _, x := range batch {
				if err := result.project(ctx, x); err != nil {
					return err
				}
			}
			return nil
		},
		OnDrop: func(batch []executionevidence.CompletedExchange, reason outputruntime.DropReason) {
			result.dropped.Add(uint64(len(batch)))
			result.mu.Lock()
			result.state = StateDegraded
			result.lastError = outputruntime.ErrorQueueOverflow
			if reason == outputruntime.DropHandle {
				result.lastError = outputruntime.ErrorExportFailed
			}
			if reason == outputruntime.DropShutdown {
				result.lastError = outputruntime.ErrorShutdownTimeout
			}
			result.mu.Unlock()
		},
	})
	if err != nil {
		_ = result.provider.Shutdown(ctx)
		return nil, err
	}
	result.worker = worker
	return result, nil
}
func (e *Exporter) Offer(x executionevidence.CompletedExchange) bool {
	e.offered.Add(1)
	if !e.worker.Offer(x) {
		e.dropped.Add(1)
		e.mu.Lock()
		e.state = StateDegraded
		e.lastError = outputruntime.ErrorQueueOverflow
		e.mu.Unlock()
		return false
	}
	return true
}
func (e *Exporter) Close() error {
	e.worker.Close()
	ctx, cancel := context.WithTimeout(context.Background(), e.drain)
	defer cancel()
	if err := e.provider.Shutdown(ctx); err != nil {
		e.mu.Lock()
		e.state = StateDegraded
		e.lastError = outputruntime.ErrorShutdownTimeout
		e.mu.Unlock()
		return err
	}
	return nil
}
func (e *Exporter) Health() Health {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return Health{State: e.state, QueueDepth: e.worker.Depth(), QueueCapacity: e.worker.Capacity(), Offered: e.offered.Load(), Exported: e.exported.Load(), Dropped: e.dropped.Load(), LastError: e.lastError}
}
func (e *Exporter) project(ctx context.Context, x executionevidence.CompletedExchange) error {
	tracer := e.provider.Tracer("swobu/m5")
	rootCtx, root := tracer.Start(context.Background(), "swobu "+string(x.Started().Operation()), apitrace.WithSpanKind(apitrace.SpanKindServer), apitrace.WithTimestamp(x.Started().StartedAt()), apitrace.WithAttributes(attribute.String("swobu.exchange.id", x.ID().String()), attribute.String("swobu.access.id", x.Started().AccessID().String()), attribute.String("swobu.workspace.slug", x.Started().Workspace().String()), attribute.String("swobu.client.family", string(x.Started().ClientFamily())), attribute.String("swobu.client.protocol", x.Started().ClientProtocol().String()), attribute.String("swobu.operation", string(x.Started().Operation())), attribute.String("swobu.delivery.result", string(x.Settled().Result()))))
	for _, inv := range x.Invocations() {
		attrs := []attribute.KeyValue{attribute.String("swobu.invocation.id", inv.Started().ID().String()), attribute.Int64("swobu.invocation.ordinal", int64(inv.Started().Ordinal())), attribute.String("swobu.execution.outcome", string(inv.Settled().Outcome())), attribute.String("swobu.execution.state", string(inv.Settled().Occurrence())), attribute.Int64("swobu.duration_ns", inv.Duration().Nanoseconds())}
		spanCtx, span := tracer.Start(rootCtx, "swobu invocation", apitrace.WithSpanKind(apitrace.SpanKindClient), apitrace.WithTimestamp(inv.Started().StartedAt()), apitrace.WithAttributes(attrs...))
		_ = spanCtx
		switch start := inv.Started().(type) {
		case executionevidence.ModelInvocationStarted:
			if id, revision, bound := start.Model().ConnectionIdentity(); bound {
				attrs = append(attrs, attribute.String("swobu.connection.id", id.String()), attribute.String("swobu.connection.revision", fmt.Sprint(revision)))
			}
			settled := inv.Settled().(executionevidence.ModelInvocationSettled)
			if route := start.Model().Route().String(); route != "" {
				attrs = append(attrs, attribute.String("swobu.route.name", route))
			}
			attrs = append(attrs, attribute.String("swobu.target.id", start.Model().Target().String()), attribute.String("swobu.target.version", fmt.Sprint(uint64(start.Model().Version()))), attribute.String("swobu.provider.spec", string(start.Model().Provider())), attribute.String("gen_ai.request.model", start.Model().Model().String()), attribute.String("swobu.invocation.purpose", string(start.Model().Purpose())))
			if failure := string(settled.Failure()); failure != "" {
				attrs = append(attrs, attribute.String("swobu.failure.class", failure))
			}
			if op := genAIOperation(start.Model().Protocol()); op != "" {
				attrs = append(attrs, attribute.String("gen_ai.operation.name", op))
			}
			u := settled.Usage()
			if n, ok := u.InputTokens(); ok {
				attrs = append(attrs, attribute.Int("gen_ai.usage.input_tokens", n))
			}
			if n, ok := u.OutputTokens(); ok {
				attrs = append(attrs, attribute.Int("gen_ai.usage.output_tokens", n))
			}
			if n, ok := u.ReasoningTokens(); ok {
				attrs = append(attrs, attribute.Int("swobu.usage.reasoning_tokens", n))
			}
			if n, ok := u.CacheReadTokens(); ok {
				attrs = append(attrs, attribute.Int("swobu.usage.cache_read_tokens", n))
			}
			if n, ok := u.CacheWriteTokens(); ok {
				attrs = append(attrs, attribute.Int("swobu.usage.cache_write_tokens", n))
			}
		case executionevidence.MCPInvocationStarted:
			if settled, ok := inv.Settled().(executionevidence.MCPInvocationSettled); ok && string(settled.Failure()) != "" {
				attrs = append(attrs, attribute.String("swobu.failure.class", string(settled.Failure())))
			}
			attrs = append(attrs, attribute.String("gen_ai.operation.name", "execute_tool"), attribute.String("mcp.method.name", "tools/call"), attribute.String("swobu.mcp.source_tool_key", start.MCP().Source().String()), attribute.String("swobu.mcp.tool_key", start.MCP().Tool().String()))
		}
		span.SetAttributes(attrs...)
		if inv.Settled().Outcome() != execution.Succeeded {
			span.SetStatus(codes.Error, string(inv.Settled().Outcome()))
		}
		span.End(apitrace.WithTimestamp(inv.Settled().SettledAt()))
	}
	if x.Settled().Result() != delivery.Succeeded {
		root.SetStatus(codes.Error, string(x.Settled().Result()))
	}
	root.End(apitrace.WithTimestamp(x.Settled().SettledAt()))
	if e.collector == nil || e.transport == nil {
		e.exported.Add(1)
		return nil
	}
	spans := e.collector.spans
	e.collector.spans = nil
	if err := e.transport.ExportSpans(ctx, spans); err != nil {
		return err
	}
	e.mu.Lock()
	e.state = StateHealthy
	e.lastError = ""
	e.mu.Unlock()
	e.exported.Add(1)
	return nil
}
func genAIOperation(protocol protocolkind.ProtocolKind) string {
	switch protocol {
	case protocolkind.ChatCompletions, protocolkind.Messages:
		return "chat"
	case protocolkind.GenerateContent:
		return "generate_content"
	default:
		return ""
	}
}

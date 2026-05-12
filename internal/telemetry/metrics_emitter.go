package telemetry

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	otlpmetrichttp "go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	otlptracehttp "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

type MetricsEmitterConfig struct {
	EndpointURL    string
	Headers        map[string]string
	Timeout        time.Duration
	ExportInterval time.Duration
}

type MetricsEmitter struct {
	provider      *sdkmetric.MeterProvider
	traceProvider *sdktrace.TracerProvider
	requestsTotal metric.Int64Counter
	installsTotal metric.Int64Counter
	ticksTotal    metric.Int64Counter
	errorTotal    metric.Int64Counter
	tracer        trace.Tracer
}

var _ Emitter = (*MetricsEmitter)(nil)

func NewMetricsEmitter(ctx context.Context, cfg MetricsEmitterConfig) (*MetricsEmitter, error) {
	endpoint := strings.TrimSpace(cfg.EndpointURL)
	if endpoint == "" {
		return nil, fmt.Errorf("otel endpoint is required")
	}
	opts := []otlpmetrichttp.Option{otlpmetrichttp.WithEndpointURL(endpoint)}
	opts = append(opts, otlpmetrichttp.WithURLPath("/api/v1/metrics"))
	if len(cfg.Headers) > 0 {
		opts = append(opts, otlpmetrichttp.WithHeaders(cfg.Headers))
	}
	if cfg.Timeout > 0 {
		opts = append(opts, otlpmetrichttp.WithTimeout(cfg.Timeout))
	}
	exporter, err := otlpmetrichttp.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("create otel metric exporter: %w", err)
	}
	readerOpts := []sdkmetric.PeriodicReaderOption{}
	if cfg.ExportInterval > 0 {
		readerOpts = append(readerOpts, sdkmetric.WithInterval(cfg.ExportInterval))
	}
	reader := sdkmetric.NewPeriodicReader(exporter, readerOpts...)
	res, err := resource.New(ctx, resource.WithAttributes(attribute.String("service.name", "swobu-telemetry")))
	if err != nil {
		return nil, fmt.Errorf("create otel metric resource: %w", err)
	}
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader), sdkmetric.WithResource(res))
	meter := provider.Meter("github.com/swobuforge/swobu/internal/telemetry")
	traceOpts := []otlptracehttp.Option{otlptracehttp.WithEndpointURL(endpoint)}
	traceOpts = append(traceOpts, otlptracehttp.WithURLPath("/api/v1/traces"))
	if len(cfg.Headers) > 0 {
		traceOpts = append(traceOpts, otlptracehttp.WithHeaders(cfg.Headers))
	}
	if cfg.Timeout > 0 {
		traceOpts = append(traceOpts, otlptracehttp.WithTimeout(cfg.Timeout))
	}
	traceExporter, err := otlptracehttp.New(ctx, traceOpts...)
	if err != nil {
		_ = provider.Shutdown(context.Background())
		return nil, fmt.Errorf("create otel trace exporter: %w", err)
	}
	traceProvider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExporter),
		sdktrace.WithResource(res),
	)

	requestsTotal, err := meter.Int64Counter("swobu_requests_total")
	if err != nil {
		_ = provider.Shutdown(context.Background())
		_ = traceProvider.Shutdown(context.Background())
		return nil, fmt.Errorf("create swobu_requests_total: %w", err)
	}
	installsTotal, err := meter.Int64Counter("swobu_installs_total")
	if err != nil {
		_ = provider.Shutdown(context.Background())
		_ = traceProvider.Shutdown(context.Background())
		return nil, fmt.Errorf("create swobu_installs_total: %w", err)
	}
	ticksTotal, err := meter.Int64Counter("swobu_telemetry_ticks_total")
	if err != nil {
		_ = provider.Shutdown(context.Background())
		_ = traceProvider.Shutdown(context.Background())
		return nil, fmt.Errorf("create swobu_telemetry_ticks_total: %w", err)
	}
	errorTotal, err := meter.Int64Counter("swobu_errors_total")
	if err != nil {
		_ = provider.Shutdown(context.Background())
		_ = traceProvider.Shutdown(context.Background())
		return nil, fmt.Errorf("create swobu_errors_total: %w", err)
	}

	return &MetricsEmitter{
		provider:      provider,
		traceProvider: traceProvider,
		requestsTotal: requestsTotal,
		installsTotal: installsTotal,
		ticksTotal:    ticksTotal,
		errorTotal:    errorTotal,
		tracer:        traceProvider.Tracer("github.com/swobuforge/swobu/internal/telemetry/error"),
	}, nil
}

func (e *MetricsEmitter) Shutdown(ctx context.Context) error {
	if e == nil {
		return nil
	}
	if e.provider != nil {
		_ = e.provider.Shutdown(ctx)
	}
	if e.traceProvider != nil {
		return e.traceProvider.Shutdown(ctx)
	}
	return nil
}

func (e *MetricsEmitter) EmitInstall(ctx context.Context, state State, swobuVersion, osFamily, arch string) {
	if e == nil {
		return
	}
	e.installsTotal.Add(ctx, 1,
		metric.WithAttributes(
			attribute.String("swobu.version", strings.TrimSpace(swobuVersion)),
			attribute.String("os", strings.TrimSpace(osFamily)),
			attribute.String("arch", strings.TrimSpace(arch)),
			attribute.Bool("telemetry_enabled", state.Enabled && !DoNotTrackEnabled()),
		),
	)
}

func (e *MetricsEmitter) EmitCounts(ctx context.Context, state string, count2xx, count429, count4xx, count5xx int64) {
	if e == nil {
		return
	}
	e.ticksTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("state", strings.TrimSpace(state))))
	if count2xx > 0 {
		e.requestsTotal.Add(ctx, count2xx, metric.WithAttributes(attribute.String("result_class", "2xx")))
	}
	if count429 > 0 {
		e.requestsTotal.Add(ctx, count429, metric.WithAttributes(attribute.String("result_class", "429")))
	}
	if count4xx > 0 {
		e.requestsTotal.Add(ctx, count4xx, metric.WithAttributes(attribute.String("result_class", "4xx")))
	}
	if count5xx > 0 {
		e.requestsTotal.Add(ctx, count5xx, metric.WithAttributes(attribute.String("result_class", "5xx")))
		e.errorTotal.Add(ctx, count5xx, metric.WithAttributes(attribute.String("error_class", "5xx")))
	}
}

func (e *MetricsEmitter) EmitErrorTrace(ctx context.Context, errorTrace ErrorTrace) {
	if e == nil || e.tracer == nil {
		return
	}
	_, span := e.tracer.Start(ctx, "swobu.error")
	span.SetAttributes(
		attribute.Int("http.status_code", errorTrace.StatusCode),
		attribute.String("result.class", strings.TrimSpace(errorTrace.ResultClass)),
		attribute.String("provider.family", normalizeProviderFamily(errorTrace.ProviderRoute)),
		attribute.String("operation", strings.TrimSpace(errorTrace.Operation)),
	)
	if errorTrace.DurationMS != nil {
		span.SetAttributes(attribute.Int("duration.ms", *errorTrace.DurationMS))
	}
	if stack := strings.TrimSpace(errorTrace.DebugRawStack); stack != "" {
		span.SetAttributes(attribute.String("debug.raw_stack", stack))
	}
	span.End()
}

package telemetry

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	otlpmetrichttp "go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
)

type MetricsEmitterConfig struct {
	EndpointURL    string
	Headers        map[string]string
	Timeout        time.Duration
	ExportInterval time.Duration
}

// MetricsEmitter uploads anonymous aggregate counters only (Path A: opt-out is
// defensible because the payload is non-personal). There is no trace exporter,
// no error span, no stack, no identifier — see ErrorSignal for the bounded
// error dimensions carried as counter attributes.
type MetricsEmitter struct {
	provider      *sdkmetric.MeterProvider
	requestsTotal metric.Int64Counter
	installsTotal metric.Int64Counter
	ticksTotal    metric.Int64Counter
	errorTotal    metric.Int64Counter
}

var _ Emitter = (*MetricsEmitter)(nil)

func NewMetricsEmitter(ctx context.Context, cfg MetricsEmitterConfig) (*MetricsEmitter, error) {
	endpoint := strings.TrimSpace(cfg.EndpointURL) // swobu:io-string source=boundary
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

	requestsTotal, err := meter.Int64Counter("swobu_requests_total")
	if err != nil {
		_ = provider.Shutdown(context.Background())
		return nil, fmt.Errorf("create swobu_requests_total: %w", err)
	}
	installsTotal, err := meter.Int64Counter("swobu_installs_total")
	if err != nil {
		_ = provider.Shutdown(context.Background())
		return nil, fmt.Errorf("create swobu_installs_total: %w", err)
	}
	ticksTotal, err := meter.Int64Counter("swobu_telemetry_ticks_total")
	if err != nil {
		_ = provider.Shutdown(context.Background())
		return nil, fmt.Errorf("create swobu_telemetry_ticks_total: %w", err)
	}
	errorTotal, err := meter.Int64Counter("swobu_errors_total")
	if err != nil {
		_ = provider.Shutdown(context.Background())
		return nil, fmt.Errorf("create swobu_errors_total: %w", err)
	}

	return &MetricsEmitter{
		provider:      provider,
		requestsTotal: requestsTotal,
		installsTotal: installsTotal,
		ticksTotal:    ticksTotal,
		errorTotal:    errorTotal,
	}, nil
}

func (e *MetricsEmitter) Shutdown(ctx context.Context) error {
	if e == nil {
		return nil
	}
	if e.provider != nil {
		_ = e.provider.Shutdown(ctx)
	}
	return nil
}

func (e *MetricsEmitter) EmitInstall(ctx context.Context, state State, swobuVersion, osFamily, arch string) {
	if e == nil {
		return
	}
	e.installsTotal.Add(ctx, 1,
		metric.WithAttributes(
			attribute.String("swobu.version", strings.TrimSpace(swobuVersion)), // swobu:io-string source=boundary
			attribute.String("os", strings.TrimSpace(osFamily)),                // swobu:io-string source=boundary
			attribute.String("arch", strings.TrimSpace(arch)),                  // swobu:io-string source=boundary
			attribute.Bool("telemetry_enabled", state.Enabled && !DoNotTrackEnabled()),
		),
	)
}

func (e *MetricsEmitter) EmitCounts(ctx context.Context, state string, count2xx, count429, count4xx, count5xx int64) {
	if e == nil {
		return
	}
	e.ticksTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("state", strings.TrimSpace(state)))) // swobu:io-string source=boundary
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
	}
}

// EmitError records a bounded, content-free error signal as aggregate counter
// attributes (result class × provider family × operation × duration bucket).
// It carries no message, stack, route, or identifier — anonymous by construction.
func (e *MetricsEmitter) EmitError(ctx context.Context, signal ErrorSignal) {
	if e == nil {
		return
	}
	e.errorTotal.Add(ctx, 1, metric.WithAttributes(
		attribute.String("result_class", errorDimension(signal.ResultClass)),
		attribute.String("provider_family", errorDimension(signal.ProviderFamily)),
		attribute.String("operation", errorDimension(signal.Operation)),
		attribute.String("duration_bucket", errorDimension(signal.DurationBucket)),
	))
}

// errorDimension trims a bounded error attribute and collapses empties to
// "unknown" so free-form values never reach the payload.
func errorDimension(value string) string {
	v := strings.TrimSpace(value) // swobu:io-string source=boundary
	if v == "" {
		return "unknown"
	}
	return v
}

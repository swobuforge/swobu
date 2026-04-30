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
	EndpointURL string
	Headers     map[string]string
	Timeout     time.Duration
}

type MetricsEmitter struct {
	provider      *sdkmetric.MeterProvider
	requestsTotal metric.Int64Counter
	installsTotal metric.Int64Counter
	ticksTotal    metric.Int64Counter
	errorTotal    metric.Int64Counter
}

var _ Emitter = (*MetricsEmitter)(nil)

func NewMetricsEmitter(ctx context.Context, cfg MetricsEmitterConfig) (*MetricsEmitter, error) {
	endpoint := strings.TrimSpace(cfg.EndpointURL)
	if endpoint == "" {
		return nil, fmt.Errorf("otel endpoint is required")
	}
	opts := []otlpmetrichttp.Option{otlpmetrichttp.WithEndpointURL(endpoint)}
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
	reader := sdkmetric.NewPeriodicReader(exporter)
	res, err := resource.New(ctx, resource.WithAttributes(attribute.String("service.name", "swobu-telemetry")))
	if err != nil {
		return nil, fmt.Errorf("create otel metric resource: %w", err)
	}
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader), sdkmetric.WithResource(res))
	meter := provider.Meter("github.com/metrofun/swobu/internal/telemetry")

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
	if e == nil || e.provider == nil {
		return nil
	}
	return e.provider.Shutdown(ctx)
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

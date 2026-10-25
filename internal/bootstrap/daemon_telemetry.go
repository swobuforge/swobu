package bootstrap

import (
	"context"

	"github.com/swobuforge/swobu/internal/app/operator/controlplane"
	"github.com/swobuforge/swobu/internal/producttelemetry"
)

// startTelemetryRuntime launches the bounded product-telemetry runtime. It is
// downstream observability only and never controls request execution.
func (d *Daemon) startTelemetryRuntime() {
	if d == nil || d.telemetry != nil {
		return
	}
	d.telemetry = producttelemetry.StartRuntime(controlplane.SwobuVersion(), d.logger)
}

func (d *Daemon) stopTelemetryRuntime() {
	if d == nil || d.telemetry == nil {
		return
	}
	d.telemetry.Close()
}

func (d *Daemon) inspectTelemetry(ctx context.Context) ([]byte, error) {
	if d == nil || d.telemetry == nil {
		return []byte("null"), nil
	}
	return d.telemetry.InspectJSON(ctx)
}

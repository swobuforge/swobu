package bootstrap

import (
	"github.com/swobuforge/swobu/internal/app/operator/controlplane"
	trafficevidence "github.com/swobuforge/swobu/internal/domain/trafficevidence"
	"github.com/swobuforge/swobu/internal/producttelemetry"
)

// startTelemetryRuntime launches the product-telemetry runtime for the daemon
// lifetime. The runtime owns its goroutine, the opt-out preference, and upload;
// Daemon holds only the handle. Idempotent.
func (d *Daemon) startTelemetryRuntime() {
	if d == nil || d.telemetry != nil {
		return
	}
	d.telemetry = producttelemetry.StartRuntime(controlplane.SwobuVersion(), d.logger)
}

// observeTelemetryEvent offers a terminal traffic event to the runtime without
// blocking the request path. Nil-safe: the sink is wired before the runtime
// starts, so a request can arrive before d.telemetry is set.
func (d *Daemon) observeTelemetryEvent(event trafficevidence.TrafficEvent) {
	if d == nil || d.telemetry == nil {
		return
	}
	d.telemetry.Observe(event)
}

// stopTelemetryRuntime stops the runtime and waits. No flush (lossy contract).
func (d *Daemon) stopTelemetryRuntime() {
	if d == nil || d.telemetry == nil {
		return
	}
	d.telemetry.Close()
}

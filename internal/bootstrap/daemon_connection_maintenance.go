package bootstrap

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/swobuforge/swobu/internal/connections"
)

// startConnectionMaintenance retries cleanup once per existing staging lease.
// Startup already reconciles immediately. The cadence bounds additional cleanup
// delay to one lease without busy-polling backend failures. It never overlaps
// passes and Close joins the worker before closing stores.
func (d *Daemon) startConnectionMaintenance() {
	ticker := time.NewTicker(connections.CredentialStageLease)
	stop := runConnectionMaintenance(d.logger, ticker.C, func(ctx context.Context, now time.Time) error {
		return d.connectionLifecycle.ReconcileStagedCredentials(ctx, now, retireDaemonConnectionCredentialForConfig(d.configPath))
	})
	d.stopConnectionMaintenance = func() { ticker.Stop(); stop() }
}

// runConnectionMaintenance owns cancellation and completion; the caller owns
// the tick source. Keeping the clock seam here permits lifecycle proof without
// making production cleanup intervals operator-facing configuration.
func runConnectionMaintenance(logger *slog.Logger, ticks <-chan time.Time, reconcile func(context.Context, time.Time) error) func() {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-ctx.Done():
				return
			case now, ok := <-ticks:
				if !ok {
					return
				}
				if ctx.Err() != nil {
					return
				}
				if err := reconcile(ctx, now); err != nil && ctx.Err() == nil {
					logger.Warn("Connection credential cleanup deferred", "event", "connection_credential_cleanup_deferred")
				}
			}
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { cancel(); <-done }) }
}

package cli

import (
	"context"

	"github.com/swobuforge/swobu/internal/bootstrap"
)

func daemonDone(d *bootstrap.Daemon) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		_ = d.Wait(context.Background())
		close(done)
	}()
	return done
}

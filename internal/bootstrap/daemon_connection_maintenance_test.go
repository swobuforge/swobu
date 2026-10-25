package bootstrap

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"
)

func TestConnectionMaintenanceSerializesPassesAndJoinsCancellation(t *testing.T) {
	ticks := make(chan time.Time, 2)
	entered := make(chan struct{}, 2)
	var active atomic.Int32
	var calls atomic.Int32
	stop := runConnectionMaintenance(slog.Default(), ticks, func(ctx context.Context, _ time.Time) error {
		if active.Add(1) != 1 {
			t.Error("overlapping cleanup")
		}
		defer active.Add(-1)
		calls.Add(1)
		entered <- struct{}{}
		<-ctx.Done()
		return ctx.Err()
	})
	ticks <- time.Now()
	<-entered
	ticks <- time.Now()
	stop()
	stop()
	if active.Load() != 0 || calls.Load() != 1 {
		t.Fatal("stop returned before cleanup or consumed queued tick after cancellation")
	}
}

func TestConnectionMaintenanceRetriesSanitizesAndStopsOnClosedTicks(t *testing.T) {
	ticks := make(chan time.Time)
	done := make(chan struct{})
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	calls := 0
	stop := runConnectionMaintenance(logger, ticks, func(context.Context, time.Time) error {
		calls++
		if calls == 1 {
			return errors.New("sensitive backend detail")
		}
		close(done)
		return nil
	})
	ticks <- time.Now()
	ticks <- time.Now()
	<-done
	close(ticks)
	stop()
	if calls != 2 || !bytes.Contains(logs.Bytes(), []byte("connection_credential_cleanup_deferred")) || bytes.Contains(logs.Bytes(), []byte("sensitive")) {
		t.Fatal("maintenance did not retry or leaked backend detail")
	}
}

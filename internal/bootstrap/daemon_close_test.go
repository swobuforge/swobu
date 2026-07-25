package bootstrap

import (
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"
)

// TestDaemonClose_IsIdempotent: Close runs once and returns the cached terminal
// result; a second call does not re-attempt shutdown or cleanup.
func TestDaemonClose_IsIdempotent(t *testing.T) {
	d := &Daemon{
		server:    &http.Server{},
		serveDone: make(chan struct{}),
		logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	d.serveErr = errors.New("serve failed")
	close(d.serveDone)

	first := d.Close()
	second := d.Close()
	if first == nil || first.Error() != second.Error() {
		t.Fatalf("Close not idempotent or lost the terminal error: first=%v second=%v", first, second)
	}
}

// TestDaemonClose_DrainsHandlersBeforeReturning: Close drives Shutdown, which
// waits for active handlers to finish before returning; all cleanup (telemetry,
// config store) follows Shutdown inside Close. So Close must not return while a
// handler is still in-flight. (Telemetry is an opaque *producttelemetry.Runtime
// here; stopTelemetryRuntime is a nil-safe no-op when it is unset.)
func TestDaemonClose_DrainsHandlersBeforeReturning(t *testing.T) {
	handlerStarted := make(chan struct{})
	handlerRelease := make(chan struct{})
	srv := &http.Server{Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		close(handlerStarted)
		<-handlerRelease
	})}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	d := &Daemon{
		server:    srv,
		listener:  ln,
		serveDone: make(chan struct{}),
		logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	go d.serve()
	go func() { _, _ = http.Get("http://" + ln.Addr().String()) }()
	<-handlerStarted

	closed := make(chan struct{})
	go func() { _ = d.Close(); close(closed) }()
	select {
	case <-closed:
		t.Fatal("Close returned while a handler was still in-flight")
	case <-time.After(80 * time.Millisecond):
	}
	close(handlerRelease)
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not return after the handler drained")
	}
}

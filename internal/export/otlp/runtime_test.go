package otlp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/domain/executionevidence"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type failingSpanExporter struct{}

func (failingSpanExporter) ExportSpans(context.Context, []sdktrace.ReadOnlySpan) error {
	return errors.New("collector unavailable")
}
func (failingSpanExporter) Shutdown(context.Context) error { return nil }

func TestSpanExporterSurfacesCollectorFailure(t *testing.T) {
	if err := (failingSpanExporter{}).ExportSpans(context.Background(), nil); err == nil {
		t.Fatal("failing collector was reported as successful")
	}
}

func TestOfferDropsAtExplicitExchangeQueueBoundary(t *testing.T) {
	e, err := New(context.Background(), Config{QueueCapacity: 1, DrainBudget: time.Millisecond, Endpoint: "http://127.0.0.1:4318/v1/traces"})
	if err != nil {
		t.Fatal(err)
	}
	// A zero-value CompletedExchange is never projected because the first offer
	// occupies the bounded queue; the second offer proves non-blocking overflow.
	var exchange executionevidence.CompletedExchange
	if !e.Offer(exchange) {
		t.Fatal("first offer unexpectedly dropped")
	}
	if e.Offer(exchange) {
		t.Fatal("second offer unexpectedly crossed a full queue")
	}
	if got := e.Health().Dropped; got != 1 {
		t.Fatalf("dropped = %d, want 1", got)
	}
	_ = e.Close()
}

func TestExporterRejectsOffersAfterClose(t *testing.T) {
	t.Setenv("SWOBU_OTEL", "otlp")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:4318")
	e, err := New(context.Background(), Config{DrainBudget: time.Millisecond, Endpoint: "http://127.0.0.1:4318/v1/traces"})
	if err != nil {
		t.Fatal(err)
	}
	_ = e.Close()
	if e.Offer(executionevidence.CompletedExchange{}) {
		t.Fatal("offer accepted after close")
	}
}

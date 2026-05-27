package openaifamily

import (
	"context"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// usageEventReader rewrites usage events via provider decoder while preserving
// event order and non-usage payload identity.
type usageEventReader struct {
	canonical.EventReader
	rawUsage RawUsageEnvelope
	adapter  ProviderUsageDecoder
}

func newUsageEventReader(inner canonical.EventReader, rawUsage RawUsageEnvelope, adapter ProviderUsageDecoder) canonical.EventReader {
	if inner == nil || adapter == nil {
		return inner
	}
	return &usageEventReader{
		EventReader: inner,
		rawUsage:    rawUsage,
		adapter:     adapter,
	}
}

func (r *usageEventReader) Next(ctx context.Context) (canonical.Event, error) {
	ev, err := r.EventReader.Next(ctx)
	if err != nil {
		return canonical.Event{}, err
	}
	if ev.Kind != canonical.EventUsage {
		return ev, nil
	}
	payload, ok := ev.Payload.(canonical.UsagePayload)
	if !ok {
		return ev, nil
	}
	normalized, _ := r.adapter.DecodeToCanonical(r.rawUsage, payload.Usage)
	payload.Usage = normalized
	ev.Payload = payload
	return ev, nil
}

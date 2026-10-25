package httpapi

import (
	"errors"
	"net/url"
	"time"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/ledger"
)

// parseRetainedActivityFilter uses the Ledger's existing indexed exchange
// filters. MCP operations have a different history query; accepting these
// fields for MCP would silently claim filters that the backend did not apply.
func parseRetainedActivityFilter(values url.Values, kind string, limit int) (ledger.ExchangeFilter, error) {
	var filter ledger.ExchangeFilter
	requested := values.Has("from") || values.Has("to") || values.Has("principal") || values.Has("result")
	if !requested {
		return filter, nil
	}
	if kind != "exchange" || limit <= 0 {
		return filter, errors.New("Activity filters require retained exchange history")
	}
	for _, item := range []struct {
		key string
		dst *time.Time
	}{
		{"from", &filter.Settled.Start},
		{"to", &filter.Settled.End},
	} {
		if !values.Has(item.key) {
			continue
		}
		parsed, err := time.Parse(time.RFC3339Nano, values.Get(item.key))
		if err != nil || parsed.IsZero() {
			return filter, errors.New("invalid Activity time filter")
		}
		*item.dst = parsed.UTC()
	}
	if values.Has("principal") {
		principal, err := accessidentity.Parse(values.Get("principal"))
		if err != nil {
			return filter, errors.New("invalid Activity Principal filter")
		}
		filter.Access = &principal
	}
	if values.Has("result") {
		result := delivery.ResultKind(values.Get("result"))
		if !delivery.ValidResultKind(result) {
			return filter, errors.New("invalid Activity result filter")
		}
		filter.DeliveryResult = &result
	}
	if err := filter.Validate(); err != nil {
		return filter, err
	}
	return filter, nil
}

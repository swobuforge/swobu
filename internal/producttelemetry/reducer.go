package producttelemetry

import (
	"sort"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	trafficevidence "github.com/swobuforge/swobu/internal/domain/trafficevidence"
	"github.com/swobuforge/swobu/internal/profile"
)

// Duration-histogram bucket indices (fixed six buckets). Every observed request
// lands in exactly one bucket; entries sum to the row count.
const (
	durationBucketUnder100ms = 0
	durationBucket100to500   = 1
	durationBucket500to1s    = 2
	durationBucket1to5s      = 3
	durationBucketOver5s     = 4
	durationBucketUnknown    = 5
)

// ReportReducer accumulates terminal traffic events into the bounded dimensions
// of a ProductReport. It is owned by the single telemetry goroutine, which
// serializes all access — therefore it holds no mutex. It never retains content
// or per-request identifiers (the producer owns the one-terminal-event-per-
// request invariant). See product-telemetry.md.
type reportReducer struct {
	rows          map[trafficRowKey]*trafficRowAccum
	overflowCount int
}

// userAgentProduct is the bounded first whitespace-delimited product token of
// the client User-Agent (see trafficevidence.NormalizeClientHandler) — not the
// full raw header. It is carried verbatim: telemetry does not classify it into a
// client family; that derivation lives in the downstream DuckDB semantic model so
// it can evolve and be recomputed over historical data. See product-telemetry.md.
type trafficRowKey struct {
	userAgentProduct   string
	requestPath        canonical.NormalizedPath
	provider           profile.ProviderID
	statusCode         int
	deliveryKind       delivery.ResultKind
	canonicalErrorCode canonical.ErrorCode
	attemptCount       int
	fallbackRecovered  bool
}

type trafficRowAccum struct {
	count    int
	duration [6]int
	ttfb     [6]int
}

// NewReportReducer returns an empty reducer. The daemon owns the clock used for
// installation-age bucketing; the reducer holds only period aggregates and no
// timestamps (the Worker stamps ingested_at).
func newReportReducer() *reportReducer {
	return &reportReducer{rows: make(map[trafficRowKey]*trafficRowAccum)}
}

// The provider spec and request path are carried verbatim from the evidence.
// Telemetry does not classify them or fold unrecognized values to "other": the
// constructor has already proved each is a canonical vocabulary member, so an
// unrecognized value is an upstream invariant violation rejected at the source,
// not a telemetry category. The coarse protocol dimension is derived downstream,
// never invented here. See product-telemetry.md.

// boundUserAgentProduct truncates the client token to the report vocabulary's
// maximum length so every reducer-constructed report is byte-bounded (see
// TestProductReport_MaximalEncodesUnderCeiling). The evidence carries the raw
// token; only this projection edge bounds it.
func boundUserAgentProduct(token string) string {
	runes := []rune(token) // swobu:io-string source=boundary
	if len(runes) <= productReportUserAgentProductMaxRunes {
		return token
	}
	return string(runes[:productReportUserAgentProductMaxRunes])
}

// Observe folds one terminal traffic event into the accumulator. Non-terminal
// events are ignored. Activation is installation-lifetime State overlaid by the
// runtime, never derived here.
func (r *reportReducer) Observe(event trafficevidence.TrafficEvent) {
	if event.EventKind() != trafficevidence.EventKindProviderTerminal {
		return
	}
	key := trafficRowKey{
		userAgentProduct:   boundUserAgentProduct(string(event.ClientHandler())),
		requestPath:        event.RequestPath(),
		provider:           event.ProviderSpec(),
		statusCode:         event.StatusCode(),
		deliveryKind:       event.DeliveryKind(),
		canonicalErrorCode: event.CanonicalErrorCode(),
		attemptCount:       event.AttemptCount(),
		fallbackRecovered:  event.FallbackRecovered(),
	}
	accum := r.rowAccum(key)
	if accum == nil {
		r.overflowCount++
		return
	}
	accum.count++
	ms, hasDuration := event.Timing().DurationMillis()
	accum.duration[durationBucketIndex(ms, hasDuration)]++
	ttfbMS, hasTTFB := event.Timing().TTFBMillis()
	accum.ttfb[durationBucketIndex(ttfbMS, hasTTFB)]++
}

func (r *reportReducer) rowAccum(key trafficRowKey) *trafficRowAccum {
	if acc, ok := r.rows[key]; ok {
		return acc
	}
	// Bound traffic-row cardinality at productReportMaxTrafficRows distinct rows;
	// a key beyond the cap returns nil and Observe folds it into overflowCount,
	// reported as a scalar rather than a fabricated domain row.
	if len(r.rows) < productReportMaxTrafficRows {
		acc := &trafficRowAccum{}
		r.rows[key] = acc
		return acc
	}
	return nil
}

// Empty reports whether the accumulator has any observed traffic.
func (r *reportReducer) Empty() bool {
	return len(r.rows) == 0 && r.overflowCount == 0
}

// Snapshot builds a deterministic ProductReport from the accumulated state. The
// caller supplies install/runtime identity; the reducer owns only aggregates.
// Snapshot does not clear state — call Reset to start a new period.
func (r *reportReducer) snapshot(installID, version, osFamily, arch string) productReport {
	rows := make([]reportTrafficRow, 0, len(r.rows))
	for key, acc := range r.rows {
		rows = append(rows, reportTrafficRow{
			UserAgentProduct:   key.userAgentProduct,
			RequestPath:        key.requestPath,
			Provider:           key.provider,
			StatusCode:         key.statusCode,
			DeliveryKind:       key.deliveryKind,
			CanonicalErrorCode: key.canonicalErrorCode,
			AttemptCount:       key.attemptCount,
			FallbackRecovered:  key.fallbackRecovered,
			Count:              acc.count,
			DurationMS:         acc.duration,
			TTFBMS:             acc.ttfb,
		})
	}
	sortTrafficRows(rows)

	return productReport{
		Schema:        productReportSchemaVersion,
		InstallID:     installID,
		Runtime:       reportRuntime{Version: version, OS: osFamily, Arch: arch},
		Traffic:       rows,
		OverflowCount: r.overflowCount,
	}
}

// Reset clears the accumulator. Called after an upload attempt regardless of
// outcome (product telemetry is lossy: a failed upload loses that period).
func (r *reportReducer) Reset() {
	r.rows = make(map[trafficRowKey]*trafficRowAccum)
	r.overflowCount = 0
}

func durationBucketIndex(ms int, ok bool) int {
	if !ok {
		return durationBucketUnknown
	}
	switch {
	case ms < 100:
		return durationBucketUnder100ms
	case ms < 500:
		return durationBucket100to500
	case ms < 1000:
		return durationBucket500to1s
	case ms < 5000:
		return durationBucket1to5s
	default:
		return durationBucketOver5s
	}
}

func sortTrafficRows(rows []reportTrafficRow) {
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.Provider != b.Provider {
			return a.Provider < b.Provider
		}
		if a.UserAgentProduct != b.UserAgentProduct {
			return a.UserAgentProduct < b.UserAgentProduct
		}
		if a.RequestPath != b.RequestPath {
			return a.RequestPath < b.RequestPath
		}
		if a.DeliveryKind != b.DeliveryKind {
			return a.DeliveryKind < b.DeliveryKind
		}
		if a.CanonicalErrorCode != b.CanonicalErrorCode {
			return a.CanonicalErrorCode < b.CanonicalErrorCode
		}
		if a.StatusCode != b.StatusCode {
			return a.StatusCode < b.StatusCode
		}
		if a.AttemptCount != b.AttemptCount {
			return a.AttemptCount < b.AttemptCount
		}
		return !a.FallbackRecovered && b.FallbackRecovered
	})
}

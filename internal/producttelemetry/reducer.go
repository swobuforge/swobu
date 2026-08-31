package producttelemetry

import (
	"sort"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
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

type trafficRowKey struct {
	clientFamily        reportClientFamily
	requestedModel      reportModelClass
	resolvedModel       reportModelClass
	clientProtocol      trafficevidence.ClientProtocol
	targetProtocol      protocolkind.ProtocolKind
	operation           trafficevidence.NormalizedOp
	provider            profile.ProviderID
	result              trafficevidence.ResultClass
	statusCode          int
	deliveryKind        delivery.ResultKind
	canonicalErrorCode  canonical.ErrorCode
	attemptCount        int
	fallbackRecovered   bool
	continuityRecovered bool
	crossProtocol       bool
	wireMutated         bool
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
// Observe folds one terminal traffic event into the accumulator. Non-terminal
// events are ignored. Activation is installation-lifetime State overlaid by the
// runtime, never derived here.
func (r *reportReducer) Observe(event trafficevidence.TrafficEvent) {
	if event.EventKind() != trafficevidence.EventKindProviderTerminal {
		return
	}
	key := trafficRowKey{
		clientFamily:        reportClientFamily(event.ClientFamily()),
		requestedModel:      classifyRequestedModel(event.ModelRequested(), event.WorkspaceRouteModelID()),
		resolvedModel:       classifyResolvedModel(event.WorkspaceRouteModelID(), event.ProviderModel()),
		clientProtocol:      event.ClientProtocol(),
		targetProtocol:      event.TargetProtocol(),
		operation:           event.NormalizedOp(),
		provider:            event.ProviderSpec(),
		result:              event.Result(),
		statusCode:          event.StatusCode(),
		deliveryKind:        event.DeliveryKind(),
		canonicalErrorCode:  event.CanonicalErrorCode(),
		attemptCount:        event.AttemptCount(),
		fallbackRecovered:   event.FallbackRecovered(),
		continuityRecovered: event.ContinuityRecovered(),
		crossProtocol:       event.ClientProtocol() != trafficevidence.ClientProtocolUnknown && string(event.ClientProtocol()) != event.TargetProtocol().String(),
		wireMutated:         hasWireMutation(event.Mutations()),
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
			ClientFamily:        key.clientFamily,
			RequestedModel:      key.requestedModel,
			ResolvedModel:       key.resolvedModel,
			ClientProtocol:      key.clientProtocol,
			TargetProtocol:      key.targetProtocol,
			Operation:           key.operation,
			Provider:            key.provider,
			Result:              key.result,
			StatusCode:          key.statusCode,
			DeliveryKind:        key.deliveryKind,
			CanonicalErrorCode:  key.canonicalErrorCode,
			AttemptCount:        key.attemptCount,
			FallbackRecovered:   key.fallbackRecovered,
			ContinuityRecovered: key.continuityRecovered,
			CrossProtocol:       key.crossProtocol,
			WireMutated:         key.wireMutated,
			Count:               acc.count,
			DurationMS:          acc.duration,
			TTFBMS:              acc.ttfb,
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

func classifyRequestedModel(requested string, route string) reportModelClass {
	switch {
	case requested == "":
		return reportModelUnknown
	case requested == "default":
		return reportModelDefault
	case route != "" && requested == route:
		return reportModelConfigured
	default:
		return reportModelCustom
	}
}

func classifyResolvedModel(route string, providerModel string) reportModelClass {
	if route != "" && providerModel != "" {
		return reportModelConfigured
	}
	return reportModelUnknown
}

// Reset clears the active accumulator after it is frozen into an immutable
// pending report, or when consent is revoked.
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
		if a.ClientFamily != b.ClientFamily {
			return a.ClientFamily < b.ClientFamily
		}
		if a.RequestedModel != b.RequestedModel {
			return a.RequestedModel < b.RequestedModel
		}
		if a.ResolvedModel != b.ResolvedModel {
			return a.ResolvedModel < b.ResolvedModel
		}
		if a.ClientProtocol != b.ClientProtocol {
			return a.ClientProtocol < b.ClientProtocol
		}
		if a.TargetProtocol != b.TargetProtocol {
			return a.TargetProtocol < b.TargetProtocol
		}
		if a.Operation != b.Operation {
			return a.Operation < b.Operation
		}
		if a.Result != b.Result {
			return a.Result < b.Result
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
		if a.FallbackRecovered != b.FallbackRecovered {
			return !a.FallbackRecovered
		}
		if a.ContinuityRecovered != b.ContinuityRecovered {
			return !a.ContinuityRecovered
		}
		if a.CrossProtocol != b.CrossProtocol {
			return !a.CrossProtocol
		}
		return !a.WireMutated && b.WireMutated
	})
}

func hasWireMutation(mutations []trafficevidence.Mutation) bool {
	for _, mutation := range mutations {
		if mutation.HasChanges() {
			return true
		}
	}
	return false
}

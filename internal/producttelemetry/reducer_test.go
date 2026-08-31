package producttelemetry

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	trafficevidence "github.com/swobuforge/swobu/internal/domain/trafficevidence"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/routing"
)

func terminalTrafficEvent(t *testing.T, requestID, clientHandler, providerSpec, requestPath string, result trafficevidence.ResultClass, statusCode, durationMs int, deliveryKind, canonicalErrorCode string, attemptCount int, fallbackRecovered bool) trafficevidence.TrafficEvent {
	return terminalTrafficEventForFamily(t, requestID, clientHandler, providerSpec, requestPath, trafficevidence.ClientFamilyCodex, result, statusCode, durationMs, deliveryKind, canonicalErrorCode, attemptCount, fallbackRecovered)
}

func terminalTrafficEventForFamily(t *testing.T, requestID, clientHandler, providerSpec, requestPath string, clientFamily trafficevidence.ClientFamily, result trafficevidence.ResultClass, statusCode, durationMs int, deliveryKind, canonicalErrorCode string, attemptCount int, fallbackRecovered bool) trafficevidence.TrafficEvent {
	t.Helper()
	id, err := trafficevidence.ParseRequestID(requestID)
	if err != nil {
		t.Fatalf("ParseRequestID: %v", err)
	}
	route, err := trafficevidence.NewRoute(providerSpec, "m")
	if err != nil {
		t.Fatalf("NewRoute: %v", err)
	}
	timing, err := trafficevidence.NewTimingWithOptional(nil, &durationMs)
	if err != nil {
		t.Fatalf("NewTimingWithOptional: %v", err)
	}
	event, err := trafficevidence.NewTerminalTrafficEvent(trafficevidence.TrafficEventInput{
		RequestID:      id,
		Workspace:      "default",
		ClientHandler:  trafficevidence.ClientHandler(clientHandler),
		ClientFamily:   clientFamily,
		ClientProtocol: trafficevidence.ClientProtocol("responses"),
		RequestPath:    canonical.NormalizedPath(requestPath),
		ProviderSpec:   profile.ProviderID(providerSpec),
		Route:          route,
		Timing:         timing,
		TargetProtocol: protocolkind.Responses,
		TargetVersion:  routing.TargetVersion(1),
	}, trafficevidence.TerminalOutcome{
		Result:             result,
		StatusCode:         statusCode,
		DeliveryKind:       delivery.ResultKind(deliveryKind),
		CanonicalErrorCode: canonical.ErrorCode(canonicalErrorCode),
		AttemptCount:       attemptCount,
		FallbackRecovered:  fallbackRecovered,
	})
	if err != nil {
		t.Fatalf("NewTerminalTrafficEvent: %v", err)
	}
	return event
}

func TestReportReducer_MergesByDimension(t *testing.T) {
	r := newReportReducer()
	// Two requests with identical dimensions — including the same client
	// Product/Version — merge into one row (count 2). Distinct Product/Version
	// tokens are distinct rows (see TestReportReducer_UserAgentIsADimension). The
	// producer owns the one-terminal-event-per-request invariant.
	r.Observe(terminalTrafficEvent(t, "req_1", "Codex/1.2", "openai", "/responses", trafficevidence.ResultClassSuccess, 200, 50, "succeeded", "", 1, false))
	r.Observe(terminalTrafficEvent(t, "req_2", "Codex/1.2", "openai", "/responses", trafficevidence.ResultClassSuccess, 200, 600, "succeeded", "", 1, false))

	report := r.snapshot("install-1", "0.1.0", "linux", "amd64")
	if len(report.Traffic) != 1 {
		t.Fatalf("rows=%d, want 1 (merged by dimension)", len(report.Traffic))
	}
	row := report.Traffic[0]
	if row.Provider != "openai" || row.ClientFamily != reportClientFamilyCodex || row.ClientProtocol != "responses" || row.TargetProtocol != protocolkind.Responses {
		t.Fatalf("row = %+v", row)
	}
	if row.Count != 2 {
		t.Fatalf("count=%d, want 2", row.Count)
	}
	if row.DurationMS[0] != 1 || row.DurationMS[2] != 1 {
		t.Fatalf("duration histogram = %v, want bucket0=1 bucket2=1", row.DurationMS)
	}
	if report.OverflowCount != 0 {
		t.Fatalf("overflow_count=%d, want 0", report.OverflowCount)
	}
	if report.InstallID != "install-1" {
		t.Fatalf("install id = %q", report.InstallID)
	}
}

func TestReportReducer_ProjectsBoundedClientProductSeparatelyFromProtocolAndProvider(t *testing.T) {
	tests := []struct {
		family trafficevidence.ClientFamily
		want   reportClientFamily
	}{
		{trafficevidence.ClientFamilyCodex, reportClientFamilyCodex},
		{trafficevidence.ClientFamilyClaudeCode, reportClientFamilyClaudeCode},
		{trafficevidence.ClientFamilyCline, reportClientFamilyCline},
		{trafficevidence.ClientFamilyOpenCode, reportClientFamilyOpenCode},
		{trafficevidence.ClientFamilyAider, reportClientFamilyAider},
		{trafficevidence.ClientFamilyOther, reportClientFamilyOther},
		{trafficevidence.ClientFamilyUnknown, reportClientFamilyUnknown},
	}
	for _, tt := range tests {
		r := newReportReducer()
		r.Observe(terminalTrafficEventForFamily(t, "req_1", "must-not-be-classified-here", "openai", "/responses", tt.family, trafficevidence.ResultClassSuccess, 200, 50, "succeeded", "", 1, false))
		row := r.snapshot("install-1", "0.1.0", "linux", "amd64").Traffic[0]
		if row.ClientFamily != tt.want || row.ClientProtocol != "responses" || row.Provider != "openai" {
			t.Fatalf("family %q projected row %+v", tt.family, row)
		}
	}
}

func TestReportReducer_ProjectsOnlyBoundedModelCategories(t *testing.T) {
	tests := []struct {
		name          string
		requested     string
		route         string
		providerModel string
		wantRequested reportModelClass
		wantResolved  reportModelClass
	}{
		{"public default", "default", "route-a", "private-upstream-model", reportModelDefault, reportModelConfigured},
		{"configured route", "route-a", "route-a", "private-upstream-model", reportModelConfigured, reportModelConfigured},
		{"arbitrary alias", "customer-secret-model-alias", "route-a", "private-upstream-model", reportModelCustom, reportModelConfigured},
		{"missing evidence", "", "", "", reportModelUnknown, reportModelUnknown},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := classifyRequestedModel(test.requested, test.route); got != test.wantRequested {
				t.Fatalf("requested class = %q, want %q", got, test.wantRequested)
			}
			if got := classifyResolvedModel(test.route, test.providerModel); got != test.wantResolved {
				t.Fatalf("resolved class = %q, want %q", got, test.wantResolved)
			}
		})
	}
}

func TestProductReportDoesNotEncodeArbitraryModelStrings(t *testing.T) {
	report := productReport{Traffic: []reportTrafficRow{{
		RequestedModel: reportModelCustom,
		ResolvedModel:  reportModelConfigured,
		TargetProtocol: protocolkind.Responses,
		Count:          1,
	}}}
	body, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"customer-secret-model-alias", "private-upstream-model"} {
		if strings.Contains(string(body), forbidden) {
			t.Fatalf("report exposed arbitrary model string %q: %s", forbidden, body)
		}
	}
}

func TestReportReducer_UserAgentIsNotADimension(t *testing.T) {
	r := newReportReducer()
	// Two requests identical except for the client Product/Version must NOT merge:
	// the version is part of the observed client identity. Mapping a token to a
	// family is a downstream decision, not a reducer decision.
	r.Observe(terminalTrafficEvent(t, "req_1", "Codex/1.2", "openai", "/responses", trafficevidence.ResultClassSuccess, 200, 50, "succeeded", "", 1, false))
	r.Observe(terminalTrafficEvent(t, "req_2", "Codex/1.3", "openai", "/responses", trafficevidence.ResultClassSuccess, 200, 50, "succeeded", "", 1, false))

	rows := r.snapshot("install-1", "0.1.0", "linux", "amd64").Traffic
	if len(rows) != 1 || rows[0].Count != 2 {
		t.Fatalf("rows=%+v, want one semantic count-2 row", rows)
	}
}

func TestReportReducer_ResetClearsAccumulator(t *testing.T) {
	r := newReportReducer()
	r.Observe(terminalTrafficEvent(t, "req_1", "Codex/1", "openai", "/responses", trafficevidence.ResultClassSuccess, 200, 50, "succeeded", "", 1, false))
	if r.Empty() {
		t.Fatal("expected non-empty after observe")
	}
	r.Reset()
	if !r.Empty() {
		t.Fatal("expected empty after reset")
	}
	if len(r.snapshot("install-1", "0.1.0", "linux", "amd64").Traffic) != 0 {
		t.Fatal("expected zero traffic rows after reset")
	}
}

// TestReportReducer_ProjectsTerminalFacts asserts the row carries the raw
// terminal facts (status_code, delivery_kind, canonical_error_code) verbatim.
// The analytical failure taxonomy is derived downstream, never on the client.
func TestReportReducer_ProjectsTerminalFacts(t *testing.T) {
	r := newReportReducer()
	r.Observe(terminalTrafficEvent(t, "req_1", "Codex/1", "openai", "/responses",
		trafficevidence.ResultClassBackendError, 502, 40, "exchange_failed", "", 1, false))

	row := r.snapshot("install-1", "0.1.0", "linux", "amd64").Traffic[0]
	if row.StatusCode != 502 {
		t.Fatalf("status_code = %d, want 502", row.StatusCode)
	}
	if row.DeliveryKind != "exchange_failed" {
		t.Fatalf("delivery_kind = %q, want exchange_failed", row.DeliveryKind)
	}
	if row.CanonicalErrorCode != "" {
		t.Fatalf("canonical_error_code = %q, want empty for a backend failure", row.CanonicalErrorCode)
	}
}

func TestReportReducer_ProjectsTTFBAndSuccessDelivery(t *testing.T) {
	r := newReportReducer()
	// TTFB 40ms and duration 40ms both land in the under-100ms bucket. Success
	// carries delivery_kind "succeeded" and no canonical error code.
	ttfb := 40
	id, _ := trafficevidence.ParseRequestID("req_1")
	route, _ := trafficevidence.NewRoute("openai", "m")
	timing, err := trafficevidence.NewTimingWithOptional(&ttfb, ptrInt(40))
	if err != nil {
		t.Fatalf("timing: %v", err)
	}
	event, err := trafficevidence.NewTerminalTrafficEvent(trafficevidence.TrafficEventInput{
		RequestID: id, Workspace: "default", ClientHandler: "Codex/1", RequestPath: "/responses",
		ProviderSpec: "openai", Route: route, Timing: timing,
		TargetProtocol: protocolkind.Responses, TargetVersion: routing.TargetVersion(1),
	}, trafficevidence.TerminalOutcome{Result: trafficevidence.ResultClassSuccess, StatusCode: 200, DeliveryKind: "succeeded"})
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	r.Observe(event)

	row := r.snapshot("install-1", "0.1.0", "linux", "amd64").Traffic[0]
	if row.DeliveryKind != "succeeded" {
		t.Fatalf("success delivery_kind = %q, want succeeded", row.DeliveryKind)
	}
	if row.CanonicalErrorCode != "" {
		t.Fatalf("success canonical_error_code = %q, want empty", row.CanonicalErrorCode)
	}
	if row.TTFBMS[0] != 1 {
		t.Fatalf("ttfb histogram = %v, want bucket0=1", row.TTFBMS)
	}
	if row.DurationMS[0] != 1 {
		t.Fatalf("duration histogram = %v, want bucket0=1", row.DurationMS)
	}
}

func TestReportReducer_ProjectsRoutingDimensions(t *testing.T) {
	r := newReportReducer()
	// Two attempts on distinct candidates that ultimately handed off: a fallback
	// recovered the request. attempt_count 2 buckets to "2".
	r.Observe(terminalTrafficEvent(t, "req_1", "Codex/1", "openai", "/responses",
		trafficevidence.ResultClassSuccess, 200, 30, "succeeded", "", 2, true))

	row := r.snapshot("install-1", "0.1.0", "linux", "amd64").Traffic[0]
	if row.AttemptCount != 2 {
		t.Fatalf("attempt_count = %d, want 2", row.AttemptCount)
	}
	if !row.FallbackRecovered {
		t.Fatal("fallback_recovered = false, want true")
	}
}

func TestReportReducer_FoldsCardinalityIntoOverflow(t *testing.T) {
	r := newReportReducer()
	// Drive > cap distinct dimension tuples (protocol × status code × attempts).
	// Beyond the cap, surplus folds into the overflow_count scalar rather than a
	// fabricated domain row.
	clientFamilies := []string{"responses", "messages", "chat_completions", "other"}
	attempts := []int{1, 2, 3, 5}
	statuses := []int{400, 401, 404, 408, 422, 429, 500, 502, 503, 504}
	n := 0
	for _, family := range clientFamilies {
		for _, a := range attempts {
			for _, s := range statuses {
				r.Observe(terminalTrafficEventForFamily(t, "req", "Codex/1", "openai", "/responses",
					trafficevidence.ClientFamily(family), trafficevidence.ResultClassBackendError, s, 10, "exchange_failed", "", a, false))
				n++
			}
		}
	}
	if n <= productReportMaxTrafficRows {
		t.Fatalf("test setup produced only %d distinct tuples, need > %d to exercise the cap", n, productReportMaxTrafficRows)
	}
	report := r.snapshot("install-1", "0.1.0", "linux", "amd64")
	if len(report.Traffic) > productReportMaxTrafficRows {
		t.Fatalf("rows=%d, must not exceed the %d-row ceiling", len(report.Traffic), productReportMaxTrafficRows)
	}
	if report.OverflowCount <= 0 {
		t.Fatalf("overflow_count=%d, want >0 (surplus must fold into the scalar)", report.OverflowCount)
	}
}

// TestProductReport_MaximalEncodesUnderCeiling proves the byte invariant with the
// genuine worst-case LEGAL report, not a favorable sample. Every variable-length
// field takes its widest legal value:
//   - 74 rows (the cap).
//   - runtime.version is 64 '<' runes. The schema permits '<', and the real
//     uploader uses json.Marshal, which HTML-escapes each '<' to six bytes.
//   - wide semantic dimensions and the longest closed values: client family,
//     operation, provider "openrouter", delivery_kind "checkpoint_commit_failed",
//     canonical_error_code "UNSUPPORTED_DELIVERY_MODE", os "dragonfly",
//     arch "sparc64", installation_age_bucket "90d_plus".
//   - every numeric field is at MaxInt32; the six-bucket histograms are spread to
//     their widest digits while still summing exactly to count (so the row
//     satisfies the relational invariant, unlike a uniform fill).
//
// Every string field has an explicit maxLength, so this covers the entire legal
// space. The row cap was chosen so this report encodes to ≤ productReportMaxBytes
// with margin; the uploader's size check is a defensive assertion, not normal
// control flow.
func TestProductReport_MaximalEncodesUnderCeiling(t *testing.T) {
	// json.Marshal HTML-escapes '<' to < (6 bytes), the widest legal encoding
	// for both the UA token and the version token.
	wideVersion := strings.Repeat("<", productReportVersionMaxBytes)
	// Spread MaxInt32 across six buckets at maximum digit width (one 10-digit +
	// five 9-digit), summing exactly to count.
	sumToCountMax := [6]int{1_000_000_000, 229_496_729, 229_496_729, 229_496_729, 229_496_729, 229_496_731}
	rows := make([]reportTrafficRow, productReportMaxTrafficRows)
	for i := range rows {
		rows[i] = reportTrafficRow{
			ClientFamily:        "chat_completions",
			ClientProtocol:      "chat_completions",
			TargetProtocol:      protocolkind.ChatCompletions,
			Operation:           "chat_completion",
			Provider:            profile.ProviderSpecOpenRouter,
			Result:              trafficevidence.ResultClassBackendError,
			StatusCode:          599,
			DeliveryKind:        delivery.CheckpointCommitFailed,
			CanonicalErrorCode:  canonical.ErrorCodeUnsupportedDelivery,
			AttemptCount:        productReportAttemptCountMax,
			FallbackRecovered:   false,
			ContinuityRecovered: true,
			CrossProtocol:       false,
			WireMutated:         true,
			Count:               productReportCountMax,
			DurationMS:          sumToCountMax,
			TTFBMS:              sumToCountMax,
		}
	}
	body, err := json.Marshal(productReport{
		Schema:                productReportSchemaVersion,
		ReportID:              "fedcba9876543210fedcba9876543210",
		ReportCreatedAt:       "2026-08-31T12:00:00Z",
		InstallID:             "0123456789abcdef0123456789abcdef",
		Runtime:               reportRuntime{Version: wideVersion, OS: "dragonfly", Arch: "sparc64"},
		InstallationAgeBucket: "90d_plus",
		Traffic:               rows,
		OverflowCount:         productReportOverflowMax,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if len(body) > productReportMaxBytes {
		t.Fatalf("maximal report encodes to %d bytes, exceeds %d-byte ceiling", len(body), productReportMaxBytes)
	}
	t.Logf("maximal report (%d rows) = %d bytes (ceiling %d, margin %d)",
		productReportMaxTrafficRows, len(body), productReportMaxBytes, productReportMaxBytes-len(body))
}

// TestReportReducer_OutputValidatesAtHighCount drives the reducer with a large,
// realistic burst and confirms the snapshot validates. It is evidence for the
// operational bound (MaxInt32 maxima are not reached at realistic rates), NOT a
// proof that every constructible state validates — the reducer does not clamp, so
// a structural guarantee would require flush-before-overflow, not this test.
func TestReportReducer_OutputValidatesAtHighCount(t *testing.T) {
	r := newReportReducer()
	const burst = 1_000_000 // 7 digits; well under MaxInt32 (~99k/s for 6h would be required to reach it).
	for range burst {
		r.Observe(terminalTrafficEvent(t, "req", "Codex/1.2", "openai", "/responses",
			trafficevidence.ResultClassSuccess, 200, 50, "succeeded", "", 1, false))
	}
	report := r.snapshot("0123456789abcdef0123456789abcdef", "0.1.0", "linux", "amd64")
	if len(report.Traffic) != 1 || report.Traffic[0].Count != burst {
		t.Fatalf("count=%d, want %d in one row", report.Traffic[0].Count, burst)
	}
	if report.Traffic[0].Count >= productReportCountMax {
		t.Fatalf("count %d reached the schema maximum %d", report.Traffic[0].Count, productReportCountMax)
	}
}

func ptrInt(v int) *int { return &v }

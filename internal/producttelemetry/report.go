package producttelemetry

import (
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/profile"
)

const (
	// productReportSchemaVersion is the closed-schema version this client emits.
	// The Worker rejects any other value.
	productReportSchemaVersion = 1
	// productReportMaxBytes bounds a serialized report. The Worker also rejects
	// payloads above 64 KiB; the client enforces the same ceiling before sending.
	productReportMaxBytes = 64 * 1024
	// productReportMaxTrafficRows bounds traffic-row cardinality. Distinct keys
	// beyond this count fold into the report-level overflow_count scalar rather
	// than dropping the report or fabricating a domain row. String widths and row
	// cardinality establish the byte bound, assuming numeric counters remain within
	// the published operational maxima (MaxInt32). See
	// TestProductReport_MaximalEncodesUnderCeiling.
	productReportMaxTrafficRows = 74
	// productReportUserAgentProductMaxRunes bounds the client token. Together with
	// the row cap and the numeric maxima it bounds the report size (the producer
	// emits only the canonical request paths, so request_path needs no rune cap).
	// The reducer enforces the UA cap at the projection edge; the evidence carries
	// the raw values.
	productReportUserAgentProductMaxRunes = 64
	// productReportVersionMaxBytes bounds the version token by bytes; validReportVersion
	// checks byte length and permits printable ASCII only (every accepted byte is one
	// character), matching the V1 schema's version pattern. An invalid canonical build
	// value fails closed at startup rather than collecting a period the Worker rejects.
	productReportVersionMaxBytes = 64
	// The numeric maxima are MaxInt32. This is an operational bound, not a proof:
	// the reducer does not clamp, so a counter could in principle exceed it. The
	// bound rests on a capacity assumption — a single dimension tuple reaching
	// 2,147,483,647 events in one 6-hour period would need ~99,000 events/s
	// sustained, which is not a realistic per-installation rate. If that assumption
	// ever stops holding, make it structural (flush before a counter exceeds the
	// maximum) rather than raising the bound. MaxInt32 is a safe exact JavaScript
	// integer (well under 2^53) and a clean DuckDB INTEGER; it bounds the
	// adversarial public input the Worker must accept.
	productReportCountMax        = 2_147_483_647 // math.MaxInt32
	productReportAttemptCountMax = 2_147_483_647
	productReportOverflowMax     = 2_147_483_647
)

// validReportVersion reports whether v satisfies the version bound: 1–64 printable
// ASCII characters, no space. It is equivalent to the V1 schema's version pattern —
// every accepted character is one ASCII byte, so byte length equals character
// count. It is the startup gate: an invalid canonical build value disables
// telemetry for the lifetime (fail closed) rather than collecting a period the
// Worker will then reject.
func validReportVersion(v string) bool {
	if len(v) == 0 || len(v) > productReportVersionMaxBytes {
		return false
	}
	for i := 0; i < len(v); i++ {
		if v[i] < 0x21 || v[i] > 0x7E { // printable ASCII [!-~], excluding space
			return false
		}
	}
	return true
}

// ProductReport is the closed, content-free payload. Fixed keys, bounded enums,
// fixed histogram buckets, integer counters, ≤ 64 KiB. The Worker is the closed
// boundary that rejects anything outside the versioned JSON Schema; this struct
// only ever carries approved fields. See product-telemetry.md.
type productReport struct {
	Schema                int                `json:"schema"`
	InstallID             string             `json:"install_id"`
	Runtime               reportRuntime      `json:"runtime"`
	InstallationAgeBucket string             `json:"installation_age_bucket"`
	Traffic               []reportTrafficRow `json:"traffic"`
	OverflowCount         int                `json:"overflow_count"`
}

type reportRuntime struct {
	Version string `json:"version"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`
}

// ReportTrafficRow is one aggregate traffic cell: a closed dimension tuple, a
// request count, and fixed six-bucket duration and TTFB histograms whose entries
// each sum to Count (every request lands in exactly one bucket; the last bucket
// holds requests with no recorded timing). The dimension fields carry owned domain
// types — canonical.NormalizedPath, profile.ProviderID,
// delivery.ResultKind, canonical.ErrorCode — end to end; the
// terminal-event constructor validates each against its vocabulary
// (NewTerminalTrafficEvent), so a non-canonical value is rejected at the source
// rather than carried to the report. They serialize to JSON strings at the marshal
// edge and are never reduced to untyped strings earlier. status_code is a raw int;
// the analytical failure taxonomy (and the coarse success/cancelled/failure
// outcome) is derived downstream, never on the client. See product-telemetry.md.
type reportTrafficRow struct {
	UserAgentProduct   string                   `json:"user_agent_product"`
	RequestPath        canonical.NormalizedPath `json:"request_path"`
	Provider           profile.ProviderID       `json:"provider"`
	StatusCode         int                      `json:"status_code"`
	DeliveryKind       delivery.ResultKind      `json:"delivery_kind"`
	CanonicalErrorCode canonical.ErrorCode      `json:"canonical_error_code"`
	AttemptCount       int                      `json:"attempt_count"`
	FallbackRecovered  bool                     `json:"fallback_recovered"`
	Count              int                      `json:"count"`
	DurationMS         [6]int                   `json:"duration_ms"`
	TTFBMS             [6]int                   `json:"ttfb_ms"`
}

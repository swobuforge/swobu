package evidence

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/swobuforge/swobu/internal/evidence"
)

func TestProjectStatus_RecentTrafficUsesCanonicalTimingAndTokenUsageObjects(t *testing.T) {
	store := NewStore(StoreConfig{})
	requestID, err := evidence.ParseRequestID("req_shape")
	if err != nil {
		t.Fatalf("ParseRequestID returned error: %v", err)
	}
	route, err := evidence.NewRoute("backend-a", "")
	if err != nil {
		t.Fatalf("NewRoute returned error: %v", err)
	}
	ttfb := 12
	dur := 34
	timing, err := evidence.NewTimingWithOptional(&ttfb, &dur)
	if err != nil {
		t.Fatalf("NewTimingWithOptional returned error: %v", err)
	}
	in := 120
	out := 9
	cacheRead := 70
	cacheWrite := 5
	usage, err := evidence.NewTokenUsageWithOptional(&in, &out, &cacheRead, &cacheWrite)
	if err != nil {
		t.Fatalf("NewTokenUsageWithOptional returned error: %v", err)
	}
	event, err := evidence.NewTerminalTrafficEvent(evidence.TrafficEventInput{Endpoint: "acme",
		RequestID:    requestID,
		Route:        route,
		Result:       evidence.ResultClassSuccess,
		StatusCode:   200,
		Timing:       timing,
		TokenUsage:   usage,
		ClientFamily: evidence.ClientFamily("responses"),
		Mutations: []evidence.Mutation{{
			Stage:         "encode",
			Transform:     "openaifamily.CacheAffinityWireTransform",
			Changed:       true,
			ChangedFields: []string{"prompt_cache_key"},
		}},
		ExchangeDiagnostics: []string{"high_transform_noop_ratio:4/5"},
		StageReports: []evidence.StageReport{{
			Stage:   "provider.wire.out",
			Carrier: "wire_document",
			Applied: []string{"openaifamily.CacheAffinityWireTransform"},
			Mutated: true,
		}},
	})
	if err != nil {
		t.Fatalf("NewTerminalTrafficEvent returned error: %v", err)
	}
	store.Append(context.Background(), event)

	projection := store.ProjectStatus(ProjectionInput{State: "healthy", Scope: ProjectionScope{Kind: ProjectionScopeAll}})
	raw, err := json.Marshal(projection)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	rowsRaw, ok := decoded["recent_traffic"].([]any)
	if !ok || len(rowsRaw) != 1 {
		t.Fatalf("decoded recent_traffic shape = %#v, want one row", decoded["recent_traffic"])
	}
	row, ok := rowsRaw[0].(map[string]any)
	if !ok {
		t.Fatalf("decoded row shape = %#v, want map", rowsRaw[0])
	}
	if _, ok := row["timing"]; !ok {
		t.Fatalf("row missing timing object: %#v", row)
	}
	if _, ok := row["token_usage"]; !ok {
		t.Fatalf("row missing token_usage object: %#v", row)
	}
	if _, ok := row["wire_transform_mutations"]; !ok {
		t.Fatalf("row missing wire_transform_mutations: %#v", row)
	}
	if _, ok := row["exchange_diagnostics"]; !ok {
		t.Fatalf("row missing exchange_diagnostics: %#v", row)
	}
	if _, ok := row["exchange_stage_reports"]; !ok {
		t.Fatalf("row missing exchange_stage_reports: %#v", row)
	}
	stageReportsRaw, ok := row["exchange_stage_reports"].([]any)
	if !ok || len(stageReportsRaw) != 1 {
		t.Fatalf("row exchange_stage_reports shape = %#v, want one entry", row["exchange_stage_reports"])
	}
	stageReport, ok := stageReportsRaw[0].(map[string]any)
	if !ok {
		t.Fatalf("stage report entry shape = %#v, want map", stageReportsRaw[0])
	}
	if got, _ := stageReport["stage"].(string); got != "provider.wire.out" {
		t.Fatalf("stage report stage = %#v, want provider.wire.out", stageReport["stage"])
	}
	if got, _ := stageReport["carrier"].(string); got != "wire_document" {
		t.Fatalf("stage report carrier = %#v, want wire_document", stageReport["carrier"])
	}
	if got, _ := stageReport["mutated"].(bool); !got {
		t.Fatalf("stage report mutated = %#v, want true", stageReport["mutated"])
	}
	appliedRaw, ok := stageReport["applied"].([]any)
	if !ok || len(appliedRaw) != 1 {
		t.Fatalf("stage report applied = %#v, want one id", stageReport["applied"])
	}
	if got, _ := appliedRaw[0].(string); got != "openaifamily.CacheAffinityWireTransform" {
		t.Fatalf("stage report applied[0] = %#v, want openaifamily.CacheAffinityWireTransform", appliedRaw[0])
	}
	for _, forbidden := range []string{"ttfb_millis", "dur_millis", "input_tokens", "output_tokens", "cache_read_tokens", "cache_write_tokens"} {
		if _, ok := row[forbidden]; ok {
			t.Fatalf("row still exposes flattened field %q: %#v", forbidden, row)
		}
	}
}

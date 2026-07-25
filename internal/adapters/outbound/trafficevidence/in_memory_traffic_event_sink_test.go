package trafficevidence

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/trafficevidence"
)

func TestProjectStatus_RecentTrafficUsesCanonicalTimingAndTokenUsageObjects(t *testing.T) {
	store := NewTrafficEventStore(StoreConfig{})
	requestID, err := trafficevidence.ParseRequestID("req_shape")
	if err != nil {
		t.Fatalf("ParseRequestID returned error: %v", err)
	}
	route, err := trafficevidence.NewRoute("backend-a", "")
	if err != nil {
		t.Fatalf("NewRoute returned error: %v", err)
	}
	ttfb := 12
	dur := 34
	timing, err := trafficevidence.NewTimingWithOptional(&ttfb, &dur)
	if err != nil {
		t.Fatalf("NewTimingWithOptional returned error: %v", err)
	}
	in := 120
	out := 9
	cacheRead := 70
	cacheWrite := 5
	usage, err := trafficevidence.NewTokenUsageWithOptional(&in, &out, &cacheRead, &cacheWrite)
	if err != nil {
		t.Fatalf("NewTokenUsageWithOptional returned error: %v", err)
	}
	event, err := trafficevidence.NewTerminalTrafficEvent(trafficevidence.TrafficEventInput{Workspace: "acme", RequestPath: "/responses",
		RequestID:     requestID,
		Route:         route,
		Timing:        timing,
		TokenUsage:    usage,
		ClientFamily:  trafficevidence.ClientFamily("responses"),
		ProviderSpec:  "anthropic",
		ProviderModel: "claude-sonnet-4-6",
		Mutations: []trafficevidence.Mutation{{
			Stage:         "encode",
			PatchID:       "p.encode",
			Changed:       true,
			ChangedFields: []string{"prompt_cache_key"},
		}},
		ExchangeDiagnostics: []string{"high_patch_noop_ratio:4/5"},
		StageReports: []trafficevidence.StageReport{{
			Stage:   "provider.wire.out",
			Carrier: "wire_document",
			Applied: []string{"p.encode"},
			Mutated: true,
		}},
	}, trafficevidence.TerminalOutcome{Result: trafficevidence.ResultClassSuccess, StatusCode: 200, DeliveryKind: "succeeded"})
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
	if row["provider_spec"] != "anthropic" || row["provider_model"] != "claude-sonnet-4-6" {
		t.Fatalf("row resolved target = %#v/%#v", row["provider_spec"], row["provider_model"])
	}
	if _, ok := row["wire_patch_mutations"]; !ok {
		t.Fatalf("row missing wire_patch_mutations: %#v", row)
	}
	mutationsRaw, ok := row["wire_patch_mutations"].([]any)
	if !ok || len(mutationsRaw) != 1 {
		t.Fatalf("row wire_patch_mutations shape = %#v, want one mutation", row["wire_patch_mutations"])
	}
	mutation, ok := mutationsRaw[0].(map[string]any)
	if !ok {
		t.Fatalf("wire_patch_mutations entry shape = %#v, want map", mutationsRaw[0])
	}
	for _, key := range []string{"leg", "patch_id", "changed", "changed_fields"} {
		if _, ok := mutation[key]; !ok {
			t.Fatalf("wire_patch_mutations entry missing %q: %#v", key, mutation)
		}
	}
	if got, _ := mutation["leg"].(string); got != "encode" {
		t.Fatalf("wire_patch_mutations entry leg = %#v, want encode", mutation["leg"])
	}
	if got, _ := mutation["patch_id"].(string); got != "p.encode" {
		t.Fatalf("wire_patch_mutations entry patch_id = %#v, want p.encode", mutation["patch_id"])
	}
	if got, _ := mutation["changed"].(bool); !got {
		t.Fatalf("wire_patch_mutations entry changed = %#v, want true", mutation["changed"])
	}
	changedFields, ok := mutation["changed_fields"].([]any)
	if !ok || len(changedFields) != 1 || changedFields[0] != "prompt_cache_key" {
		t.Fatalf("wire_patch_mutations entry changed_fields = %#v, want one changed field", mutation["changed_fields"])
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
	if got, _ := appliedRaw[0].(string); got != "p.encode" {
		t.Fatalf("stage report applied[0] = %#v, want p.encode", appliedRaw[0])
	}
	for _, forbidden := range []string{"ttfb_millis", "dur_millis", "input_tokens", "output_tokens", "cache_read_tokens", "cache_write_tokens"} {
		if _, ok := row[forbidden]; ok {
			t.Fatalf("row still exposes flattened field %q: %#v", forbidden, row)
		}
	}
}

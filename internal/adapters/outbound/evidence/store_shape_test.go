package evidence

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/runtimeevidence"
)

func TestProjectStatus_RecentTrafficUsesCanonicalTimingAndTokenUsageObjects(t *testing.T) {
	store := NewStore(StoreConfig{})
	requestID, err := runtimeevidence.ParseRequestID("req_shape")
	if err != nil {
		t.Fatalf("ParseRequestID returned error: %v", err)
	}
	route, err := runtimeevidence.NewRoute("backend-a", "")
	if err != nil {
		t.Fatalf("NewRoute returned error: %v", err)
	}
	ttfb := 12
	dur := 34
	timing, err := runtimeevidence.NewTimingWithOptional(&ttfb, &dur)
	if err != nil {
		t.Fatalf("NewTimingWithOptional returned error: %v", err)
	}
	in := 120
	out := 9
	cacheRead := 70
	cacheWrite := 5
	usage, err := runtimeevidence.NewTokenUsageWithOptional(&in, &out, &cacheRead, &cacheWrite)
	if err != nil {
		t.Fatalf("NewTokenUsageWithOptional returned error: %v", err)
	}
	event, err := runtimeevidence.NewTerminalTrafficEvent(runtimeevidence.TrafficEventInput{
		RequestID:     requestID,
		Endpoint:      "acme",
		Route:         route,
		Result:        runtimeevidence.ResultClassSuccess,
		StatusCode:    200,
		Timing:        timing,
		TokenUsage:    usage,
		IngressFamily: runtimeevidence.IngressFamily("responses"),
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
	for _, forbidden := range []string{"ttfb_millis", "dur_millis", "input_tokens", "output_tokens", "cache_read_tokens", "cache_write_tokens"} {
		if _, ok := row[forbidden]; ok {
			t.Fatalf("row still exposes flattened field %q: %#v", forbidden, row)
		}
	}
}

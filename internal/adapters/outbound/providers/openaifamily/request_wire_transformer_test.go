package openaifamily

import (
	"encoding/json"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/transform"
)

func TestProviderWireOutTransforms_AppliesStagedModulesFromProfileFactRecord(t *testing.T) {
	in := carrier.WireDocument{
		Raw: []byte(`{"tools":[{"type":"namespace","name":"mcp.search"}],"model":"gpt-x","unsupported":1}`),
	}
	out, reports, stageReports, losses, notices, evidence, err := transform.ApplyProviderWireOutStage(in, newTransformRegistry(ProfileFactRecord{
		CacheAffinityKey:       "repo-alpha",
		CacheAffinityRetention: "ephemeral",
	}))
	if err != nil {
		t.Fatalf("ApplyProviderWireOutTransforms() error = %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(out.Raw, &payload); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if _, ok := payload["unsupported"]; !ok {
		t.Fatalf("unsupported field should remain until strictjson policy lane is re-enabled: %#v", payload)
	}
	if len(reports) == 0 || len(stageReports) != 1 || stageReports[0].Stage != "provider_wire_out" {
		t.Fatalf("unexpected transform reporting reports=%#v stages=%#v", reports, stageReports)
	}
	if len(losses) != 0 {
		t.Fatalf("losses should be empty for cache-affinity transform: %#v", losses)
	}
	if len(notices) != 0 {
		t.Fatalf("notices should be empty for cache-affinity transform: %#v", notices)
	}
	if len(evidence) != 0 {
		t.Fatalf("evidence should be empty for cache-affinity transform: %#v", evidence)
	}
}

func TestTransformFactNotices_AddsCacheRetentionWarningFromFacts(t *testing.T) {
	notices := transformFactNotices(ProfileFactRecord{CacheRetentionUnsupported: true})
	if len(notices) != 1 || notices[0].Code != "cache_retention_unsupported" {
		t.Fatalf("unexpected notices: %#v", notices)
	}
}

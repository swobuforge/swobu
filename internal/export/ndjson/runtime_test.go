package ndjson

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/clientfamily"
	"github.com/swobuforge/swobu/internal/domain/execution"
	"github.com/swobuforge/swobu/internal/domain/executionevidence"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestCommittedSchemaIsValidJSON(t *testing.T) {
	path := filepath.Join("..", "..", "..", "contracts", "execution-export", "exchange-v1.schema.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document any
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatalf("committed schema is invalid JSON: %v", err)
	}
}

func TestCommittedSchemaValidatesInvocationUnion(t *testing.T) {
	path := filepath.Join("..", "..", "..", "contracts", "execution-export", "exchange-v1.schema.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	base := map[string]any{"schema_version": float64(1), "type": "exchange", "exchange": map[string]any{"exchange_id": "ex_test", "access_id": "local", "workspace_slug": "acme", "started_at": "2026-01-01T00:00:00Z", "settled_at": "2026-01-01T00:00:01Z", "duration_ns": float64(1), "client_family": "codex", "client_protocol": "responses", "normalized_operation": "responses", "delivery_result": "succeeded"}}
	base["invocations"] = []any{}
	common := map[string]any{"invocation_id": "inv_test_1", "ordinal": float64(1), "started_at": "2026-01-01T00:00:00Z", "settled_at": "2026-01-01T00:00:01Z", "duration_ns": float64(1), "outcome": "succeeded", "occurrence": "occurred"}
	model := map[string]any{}
	for k, v := range base {
		model[k] = v
	}
	model["invocations"] = []any{map[string]any{"invocation_id": common["invocation_id"], "ordinal": common["ordinal"], "started_at": common["started_at"], "settled_at": common["settled_at"], "duration_ns": common["duration_ns"], "outcome": common["outcome"], "occurrence": common["occurrence"], "kind": "model", "resource": map[string]any{"target_id": "target", "target_version": "1", "provider_spec": "openai", "provider_protocol": "responses", "provider_model": "gpt", "purpose": "exchange_attempt"}}}
	mcp := map[string]any{}
	for k, v := range base {
		mcp[k] = v
	}
	mcp["invocations"] = []any{map[string]any{"invocation_id": common["invocation_id"], "ordinal": common["ordinal"], "started_at": common["started_at"], "settled_at": common["settled_at"], "duration_ns": common["duration_ns"], "outcome": common["outcome"], "occurrence": common["occurrence"], "kind": "mcp_tool", "resource": map[string]any{"source_tool_key": "server", "tool_key": "search"}}}
	for name, value := range map[string]any{"zero": base, "model": model, "mcp": mcp} {
		if err := resolved.Validate(value); err != nil {
			t.Errorf("%s fixture rejected: %v", name, err)
		}
	}
	bad := map[string]any{}
	for k, v := range model {
		bad[k] = v
	}
	bad["invocations"] = []any{map[string]any{"invocation_id": common["invocation_id"], "ordinal": common["ordinal"], "started_at": common["started_at"], "settled_at": common["settled_at"], "duration_ns": common["duration_ns"], "outcome": common["outcome"], "occurrence": common["occurrence"], "kind": "model", "resource": map[string]any{"source_tool_key": "server", "tool_key": "search"}}}
	if err := resolved.Validate(bad); err == nil {
		t.Fatal("mismatched invocation union unexpectedly validated")
	}
}

func fixture(t *testing.T) executionevidence.CompletedExchange {
	t.Helper()
	id, _ := execution.NewExchangeID()
	ws, _ := routing.ParseWorkspaceSlug("acme")
	start, _ := executionevidence.NewExchangeStarted(id, accessidentity.Local, ws, time.Now(), clientfamily.Codex, protocolkind.Responses, canonical.NormalizedPathResponses)
	settle, _ := executionevidence.NewExchangeSettled(id, time.Now().Add(time.Second), delivery.Succeeded, 200, "")
	x, err := executionevidence.NewCompletedExchange(start, nil, settle)
	if err != nil {
		t.Fatal(err)
	}
	return x
}

func TestMarshalExchangeIsVersionedAndContentFree(t *testing.T) {
	raw, err := MarshalExchange(fixture(t))
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["type"] != "exchange" || decoded["schema_version"] != float64(1) {
		t.Fatalf("record=%v", decoded)
	}
	if strings.Contains(string(raw), "prompt") || strings.Contains(string(raw), "completion") || strings.Contains(string(raw), "content") {
		t.Fatalf("prohibited content in %s", raw)
	}
}

func TestMarshalExchangeGoldenShapeUsesClosedResourceKinds(t *testing.T) {
	raw, err := MarshalExchange(fixture(t))
	if err != nil {
		t.Fatal(err)
	}
	var record struct {
		Invocations []struct {
			Kind     string         `json:"kind"`
			Resource map[string]any `json:"resource"`
		} `json:"invocations"`
	}
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatal(err)
	}
	if len(record.Invocations) != 0 {
		t.Fatalf("zero-invocation fixture unexpectedly had invocations: %s", raw)
	}
	model := modelResourceDTO{Route: "primary", Target: "target", TargetVersion: "1", Provider: "openai", Protocol: "responses", Model: "model", Purpose: "exchange_attempt"}
	encoded, err := json.Marshal(model)
	if err != nil {
		t.Fatal(err)
	}
	var shape map[string]any
	if err := json.Unmarshal(encoded, &shape); err != nil {
		t.Fatal(err)
	}
	if shape["route_name"] != "primary" || shape["target_version"] != "1" {
		t.Fatalf("model resource shape=%v", shape)
	}
	mcp, err := json.Marshal(mcpResourceDTO{Source: "server", Tool: "tool"})
	if err != nil {
		t.Fatal(err)
	}
	var mcpShape map[string]any
	if err := json.Unmarshal(mcp, &mcpShape); err != nil {
		t.Fatal(err)
	}
	if len(mcpShape) != 2 || mcpShape["source_tool_key"] != "server" || mcpShape["tool_key"] != "tool" {
		t.Fatalf("mcp resource shape=%v", mcpShape)
	}
}

func TestExporterWritesNewlineTerminatedRecordAndClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "execution.jsonl")
	e, err := New(Config{Path: path, BatchSize: 1, BatchWait: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if !e.Offer(fixture(t)) {
		t.Fatal("offer dropped")
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
			if data[len(data)-1] != '\n' {
				t.Fatal("record was not newline terminated")
			}
			break
		}
		time.Sleep(time.Millisecond)
	}
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
}
func TestExporterOfferIsNonBlockingAtQueueBoundary(t *testing.T) {
	e, err := New(Config{Path: filepath.Join(t.TempDir(), "x.jsonl"), QueueCapacity: 1, BatchSize: 64, BatchWait: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	x := fixture(t)
	_ = e.Offer(x)
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestExporterRejectsOffersAfterClose(t *testing.T) {
	e, err := New(Config{Path: filepath.Join(t.TempDir(), "x.jsonl")})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	if e.Offer(fixture(t)) {
		t.Fatal("offer accepted after close")
	}
}

func TestExporterFailureStaysBoundedAndSurfacesHealth(t *testing.T) {
	// A directory is an absolute, valid configuration path that cannot be
	// opened as an append-only export file. The worker must degrade without
	// blocking the producer or growing memory without bound.
	e, err := New(Config{Path: t.TempDir(), QueueCapacity: 32, BatchSize: 8, BatchWait: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	x := fixture(t)
	for i := 0; i < 2000; i++ {
		e.Offer(x)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if h := e.Health(); h.State == StateDegraded && h.Dropped > 0 {
			if h.QueueCapacity != 32 || h.QueueDepth > h.QueueCapacity || h.LastError == "" {
				t.Fatalf("invalid degraded health: %+v", h)
			}
			_ = e.Close()
			return
		}
		time.Sleep(time.Millisecond)
	}
	_ = e.Close()
	t.Fatalf("export failure did not become visible: %+v", e.Health())
}

func TestNewTruncatesUnterminatedTailBeforeAppending(t *testing.T) {
	path := filepath.Join(t.TempDir(), "execution.jsonl")
	if err := os.WriteFile(path, []byte("{\"complete\":true}\n{\"partial\":"), 0o600); err != nil {
		t.Fatal(err)
	}
	e, err := New(Config{Path: path, BatchSize: 1, BatchWait: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if !e.Offer(fixture(t)) {
		t.Fatal("offer dropped")
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if e.Health().Exported == 1 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(lines) != 2 || lines[0] != "{\"complete\":true}" {
		t.Fatalf("repaired output=%q", data)
	}
	for _, line := range lines {
		var value any
		if err := json.Unmarshal([]byte(line), &value); err != nil {
			t.Fatalf("invalid line %q: %v", line, err)
		}
	}
}

package responses

import (
	"encoding/json"
	"net/http"
	"os"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
	"github.com/swobuforge/swobu/internal/wire"
)

// TestCodex0147WaitRegressionTransformDelta documents the deterministic
// fidelity candidate only. It is not evidence that flat addressing caused the
// model's invalid wait selection; that claim requires the frozen live A/B.
func TestCodex0147WaitRegressionTransformDelta(t *testing.T) {
	t.Parallel()

	before := decodeCodexWaitFixture(t, "testdata/codex_0147_wait/client_0146_swobu.json")
	after := decodeCodexWaitFixture(t, "testdata/codex_0147_wait/client_0147_swobu.json")

	beforeTools := canonicaltest.Tools(before.Request.Request)
	if len(beforeTools) != 1 || beforeTools[0].Kind() != canonical.ToolKindFunction || beforeTools[0].Key().Namespace() != canonical.ToolNamespaceRequest {
		t.Fatalf("0.146 canonical tools = %#v, want request-scoped wait function", beforeTools)
	}
	afterTools := canonicaltest.Tools(after.Request.Request)
	if len(afterTools) != 1 || afterTools[0].Kind() != canonical.ToolKindNamespace {
		t.Fatalf("0.147 canonical tools = %#v, want functions namespace", afterTools)
	}
	namespace, _ := afterTools[0].Namespace()
	children := namespace.Tools()
	if len(children) != 1 || children[0].Key().Namespace() != "functions" || children[0].Key().Name() != "wait" {
		t.Fatalf("0.147 namespace children = %#v, want functions.wait", children)
	}

	names, namingChanges, err := provider.BuildAttemptToolNames(after.Request.Request)
	if err != nil {
		t.Fatal(err)
	}
	wireName, err := names.WireName(children[0].Key())
	if err != nil {
		t.Fatal(err)
	}
	if wireName == "wait" {
		t.Fatal("0.147 functions.wait unexpectedly retained a flat literal name")
	}
	if !hasCodexWaitChange(namingChanges, canonical.RequestToolsName) {
		t.Fatalf("attempt naming changes = %#v, want request.tools.name approximation", namingChanges)
	}

	changes := append([]compat.Change(nil), namingChanges...)
	document, err := EncodeCarrierWithChanges(
		EncodeInput{Request: after.Request.Request, ToolNames: names},
		delivery.StreamingDelivery(delivery.FramingSSE),
		&changes,
		"codex-0147-wait",
		EncodeOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Tools []ProviderRequestTool `json:"tools"`
	}
	if err := json.Unmarshal(document.RawBytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Tools) != 1 || payload.Tools[0].Type != "function" || payload.Tools[0].Name != wireName {
		t.Fatalf("0.147 provider tools = %#v, want flat alias %q", payload.Tools, wireName)
	}
	if !hasCodexWaitChange(changes, canonical.RequestTools) {
		t.Fatalf("provider changes = %#v, want request.tools approximation", changes)
	}
}

func decodeCodexWaitFixture(t *testing.T, path string) wire.ClientDecodeResult {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	headers := http.Header{}
	headers.Set(responsesLiteHeader, "true")
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(
		carrier.NewDocument(protocolkind.Responses, "application/json", headers, raw, carrier.Meta{}),
	)
	if err != nil {
		t.Fatal(err)
	}
	return decoded
}

func hasCodexWaitChange(changes []compat.Change, capability canonical.CapabilityPath) bool {
	for _, change := range changes {
		if change.Capability == capability && change.Kind == compat.Approximation {
			return true
		}
	}
	return false
}

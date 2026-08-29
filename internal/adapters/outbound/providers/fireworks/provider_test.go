package fireworks

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/session"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

type credentialResolver struct{}

func (credentialResolver) ResolveCredential(context.Context, string, string) (string, error) {
	return "fireworks-token", nil
}

func TestRuntimeCapturesResponsesContinuationAndPreservesOtherProtocols(t *testing.T) {
	bundle := NewRuntime(nil, credentialResolver{})
	for _, kind := range []protocolkind.ProtocolKind{protocolkind.ChatCompletions, protocolkind.Responses, protocolkind.Messages} {
		t.Run(string(kind), func(t *testing.T) {
			target := fireworksTarget(kind)
			backend, err := bundle.BackendResolver.ResolveBackend(target)
			if err != nil {
				t.Fatal(err)
			}
			codec, ok := backend.Codec.(protocolcodec.Codec)
			if !ok {
				t.Fatalf("codec = %T, want protocolcodec.Codec", backend.Codec)
			}
			if got, want := codec.ResponsesDialect.CaptureResponsesContinuation, kind == protocolkind.Responses; got != want {
				t.Fatalf("CaptureResponsesContinuation = %t, want %t", got, want)
			}
		})
	}
}

func TestRuntimeLeavesFireworksModelDiscoveryManual(t *testing.T) {
	if _, err := NewRuntime(nil, credentialResolver{}).Discovery.ProbeTarget(context.Background(), provider.TargetSnapshot{}); err == nil {
		t.Fatal("manual Fireworks model authoring must not probe a generic catalog")
	}
}

func TestModelIdentitiesPassThroughWithoutFireworksClassification(t *testing.T) {
	for _, model := range []string{
		"accounts/fireworks/models/model-1",
		"accounts/acme/models/model-1",
		"accounts/acme/deployments/deploy-1",
		"accounts/acme/routers/router-1",
		"model-1#deployment-1",
	} {
		t.Run(model, func(t *testing.T) {
			backend := fireworksResponsesBackend(t)
			request := canonical.NewCanonicalRequest(canonical.RequestParams{
				Model: canonical.Specify(model),
				Items: []canonical.CanonicalItem{
					canonicaltest.Message(t, canonical.MessageRoleUser, "hello"),
				},
			})
			document, _, err := backend.Codec.Encode(provider.Request{Canonical: request, Delivery: delivery.BufferedDelivery()})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(document.RawBytes()), fmt.Sprintf(`"model":%q`, model)) {
				t.Fatalf("model identity changed on wire: %s", document.RawBytes())
			}
		})
	}
}

func TestResponsesContinuationUsesMatchingTargetAndStoreFalseFallsBackToHistory(t *testing.T) {
	backend := fireworksResponsesBackend(t)
	turnOne := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("accounts/acme/deployments/deploy-1"),
		Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "turn one")},
	})
	turnOneResponse := canonicaltest.ResponseWithRef(t, canonical.ResponseRef{
		SwobuID: "previous", Responses: &canonical.ResponsesContinuation{
			ProviderResponseID: "resp_1", TargetID: backend.Target.TargetID, TargetVersion: backend.Target.TargetVersion,
		},
	}, "model", []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleAssistant, "answer one")}, canonical.Completed("completed"), canonical.NewUnknownTokenUsage())
	turnTwo := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("accounts/acme/deployments/deploy-1"), Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "turn two")}, PreviousResponse: &canonical.ResponseRef{SwobuID: "previous"},
	})
	prepared, err := session.Resume(turnTwo, session.Checkpoint{Request: turnOne, Response: turnOneResponse})
	if err != nil {
		t.Fatal(err)
	}
	previous, ok := prepared.PreviousHistory(backend.Target.TargetID, backend.Target.TargetVersion)
	if !ok {
		t.Fatal("matching Fireworks target did not expose Responses continuation")
	}
	document, _, err := backend.Codec.Encode(provider.Request{Canonical: prepared.Request(), Delivery: delivery.BufferedDelivery(), PreviousHistory: &previous})
	if err != nil {
		t.Fatal(err)
	}
	wire := string(document.RawBytes())
	if !strings.Contains(wire, `"previous_response_id":"resp_1"`) || strings.Contains(wire, "turn one") || strings.Contains(wire, "answer one") || !strings.Contains(wire, "turn two") {
		t.Fatalf("native continuation wire = %s", wire)
	}

	storeFalse := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("accounts/acme/deployments/deploy-1"), Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "turn two")}, PreviousResponse: &canonical.ResponseRef{SwobuID: "previous"}, Store: canonical.Specify(false),
	})
	fallback, err := session.Resume(storeFalse, session.Checkpoint{Request: turnOne, Response: canonicaltest.Response(t, "previous", "model", []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleAssistant, "answer one")}, canonical.Completed("completed"))})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := fallback.PreviousHistory(backend.Target.TargetID, backend.Target.TargetVersion); ok {
		t.Fatal("store:false exposed native Fireworks continuation")
	}
	document, _, err = backend.Codec.Encode(provider.Request{Canonical: fallback.Request(), Delivery: delivery.BufferedDelivery()})
	if err != nil {
		t.Fatal(err)
	}
	wire = string(document.RawBytes())
	if strings.Contains(wire, "previous_response_id") || !strings.Contains(wire, `"store":false`) || !strings.Contains(wire, "turn one") || !strings.Contains(wire, "answer one") || !strings.Contains(wire, "turn two") {
		t.Fatalf("store:false fallback wire = %s", wire)
	}
}

func fireworksResponsesBackend(t *testing.T) provider.Backend {
	t.Helper()
	backend, err := NewRuntime(nil, credentialResolver{}).BackendResolver.ResolveBackend(fireworksTarget(protocolkind.Responses))
	if err != nil {
		t.Fatal(err)
	}
	return backend
}

func fireworksTarget(kind protocolkind.ProtocolKind) provider.TargetSnapshot {
	target := provider.NewTargetSnapshot("fireworks", "fireworks", "https://api.fireworks.ai/inference/v1", "env:FIREWORKS_API_KEY", kind, string(kind), delivery.BufferedDelivery())
	target.Model = "accounts/acme/deployments/deploy-1"
	return target
}

var _ providersruntime.Discovery = NewRuntime(nil, nil).Discovery

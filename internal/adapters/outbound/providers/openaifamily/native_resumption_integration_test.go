package openaifamily

import (
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/session"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

// TestOfficialOpenAIResponsesUsesVersionedCanonicalRefinement proves the
// capture-by-default Responses contract that ChatGPT opts out of: official
// OpenAI captures the provider response ID as a reusable ResponsesContinuation,
// so session routing derives a preferred attempt for a matching target version and the
// wire request carries previous_response_id with no replayed history. This is
// the positive mirror of the ChatGPT regression in the chatgpt package.
func TestResponsesContinuationConsumptionUsesVersionedCanonicalRefinement(t *testing.T) {
	target := provider.NewTargetSnapshot(
		"official-openai",
		"openai",
		"https://api.openai.com/v1",
		"env:OPENAI_API_KEY",
		protocolkind.Responses,
		"",
		"responses",
	)
	target.Model = "gpt-test"
	backend, err := NewExecutor(nil, stubCredentialResolver{}, NewOpenAIPolicy()).ResolveBackend(target)
	if err != nil {
		t.Fatal(err)
	}
	turnOne := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("gpt-test"),
		Items: []canonical.CanonicalItem{
			canonicaltest.Message(t, canonical.MessageRoleUser, "turn one"),
		},
	})
	turnOneResponse := canonicaltest.ResponseWithRef(t, canonical.ResponseRef{
		SwobuID: "swobu_previous", Responses: &canonical.ResponsesContinuation{
			ProviderResponseID: "provider_response_from_previous_target_version", TargetID: backend.Target.TargetID, TargetVersion: backend.Target.TargetVersion,
		},
	}, "gpt-test", []canonical.CanonicalItem{
		canonicaltest.Message(t, canonical.MessageRoleAssistant, "answer one"),
	}, canonical.Completed("completed"), canonical.NewUnknownTokenUsage())
	turnTwo := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:            canonical.Specify("gpt-test"),
		Items:            []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "turn two")},
		PreviousResponse: &canonical.ResponseRef{SwobuID: "swobu_previous"},
	})
	prepared, err := session.Resume(turnTwo, session.Checkpoint{Request: turnOne, Response: turnOneResponse})
	if err != nil {
		t.Fatal(err)
	}

	id, start, end, ok := prepared.ResponsesPrevious(backend.Target.TargetID, backend.Target.TargetVersion)
	if !ok {
		t.Fatal("matching target version did not expose Responses continuation data")
	}
	request := provider.Request{
		Canonical: prepared.Request(), Delivery: delivery.BufferedDelivery(),
		ResponsesPrevious: &provider.ResponsesPrevious{ProviderResponseID: id, OmitStart: start, OmitEnd: end},
	}
	document, _, err := backend.Codec.Encode(request)
	if err != nil {
		t.Fatal(err)
	}
	wire := string(document.RawBytes())
	if !strings.Contains(wire, `"previous_response_id":"provider_response_from_previous_target_version"`) {
		t.Fatalf("native resumption missing: %s", wire)
	}
	if strings.Contains(wire, "turn one") || strings.Contains(wire, "answer one") || !strings.Contains(wire, "turn two") {
		t.Fatalf("native resumption did not send only the current delta: %s", wire)
	}
}

func TestOfficialOpenAIStoreFalseUsesFullHistoryWithoutNativeContinuation(t *testing.T) {
	t.Parallel()

	target := provider.NewTargetSnapshot(
		"official-openai",
		"openai",
		"https://api.openai.com/v1",
		"env:OPENAI_API_KEY",
		protocolkind.Responses,
		"",
		"responses",
	)
	target.Model = "gpt-test"
	backend, err := NewExecutor(nil, stubCredentialResolver{}, NewOpenAIPolicy()).ResolveBackend(target)
	if err != nil {
		t.Fatal(err)
	}
	turnOne := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:     canonical.Specify("gpt-test"),
		Items:     []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "turn one")},
		Responses: canonical.NewResponsesRequestRefinement(canonical.Specify(false)),
	})
	turnOneResponse := canonicaltest.Response(
		t,
		"swobu_previous",
		"gpt-test",
		[]canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleAssistant, "answer one")},
		canonical.Completed("completed"),
	)
	checkpoint := session.Checkpoint{Request: turnOne, Response: turnOneResponse}
	turnTwo := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:            canonical.Specify("gpt-test"),
		Items:            []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "turn two")},
		PreviousResponse: &canonical.ResponseRef{SwobuID: "swobu_previous"},
		Responses:        canonical.NewResponsesRequestRefinement(canonical.Specify(false)),
	})
	prepared, err := session.Resume(turnTwo, checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	selected := prepared.Request()
	if _, _, _, ok := prepared.ResponsesPrevious(backend.Target.TargetID, backend.Target.TargetVersion); ok {
		t.Fatal("store:false response identity exposed native continuation")
	}
	document, _, err := backend.Codec.Encode(provider.Request{Canonical: selected, Delivery: delivery.BufferedDelivery()})
	if err != nil {
		t.Fatal(err)
	}
	wire := string(document.RawBytes())
	if strings.Contains(wire, "previous_response_id") {
		t.Fatalf("store:false OpenAI request carried native continuation: %s", wire)
	}
	if !strings.Contains(wire, `"store":false`) {
		t.Fatalf("store:false was not forwarded: %s", wire)
	}
	if !strings.Contains(wire, "turn one") || !strings.Contains(wire, "answer one") || !strings.Contains(wire, "turn two") {
		t.Fatalf("store:false OpenAI request did not carry full history: %s", wire)
	}
}

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

func TestOfficialOpenAIResponsesUsesVersionedCanonicalRefinement(t *testing.T) {
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
	semantic := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("gpt-test"),
		Items: []canonical.CanonicalItem{
			canonicaltest.Message(t, canonical.MessageRoleUser, "turn one"),
			canonicaltest.Message(t, canonical.MessageRoleAssistant, "answer one"),
			canonicaltest.Message(t, canonical.MessageRoleUser, "turn two"),
		},
	})
	delta := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("gpt-test"), Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "turn two")}, PreviousResponse: &canonical.ResponseRef{
		SwobuID: "swobu_previous", Responses: &canonical.ResponsesContinuation{
			ProviderResponseID: "provider_response_from_previous_target_version", TargetID: backend.Target.TargetID, TargetVersion: backend.Target.TargetVersion,
		},
	}})
	prepared := session.ResolvedRequest{
		Full:  semantic,
		Delta: delta,
	}

	request := provider.Request{Canonical: prepared.ForTarget(backend.Target), Delivery: delivery.BufferedDelivery()}
	if previous, ok := request.Canonical.PreviousResponse(); !ok || previous.Responses == nil {
		t.Fatal("matching target version did not reuse canonical Responses refinement")
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

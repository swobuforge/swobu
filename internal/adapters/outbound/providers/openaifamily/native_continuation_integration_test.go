package openaifamily

import (
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/replay"
)

func TestOfficialOpenAIResponsesUsesVersionedNativeContinuation(t *testing.T) {
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
	if backend.CaptureContinuation == nil {
		t.Fatal("official OpenAI Responses did not opt into native continuation")
	}

	semantic := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: "gpt-test",
		Items: []canonical.CanonicalItem{
			canonical.NewTextItem(canonical.ItemAuthorUser, "turn one"),
			canonical.NewTextItem(canonical.ItemAuthorAssistant, "answer one"),
			canonical.NewTextItem(canonical.ItemAuthorUser, "turn two"),
		},
	})
	delta := canonical.NewCanonicalRequest(canonical.RequestParams{Model: "gpt-test", InputText: "turn two"})
	prepared := replay.Prepared{
		Semantic: semantic,
		Delta:    delta,
		Base:     &replay.Record{Native: backend.Target.NativeContinuation("provider_response_from_previous_target_version")},
	}

	request := prepared.ForBackend(backend, delivery.BufferedDelivery())
	if request.Continuation == nil {
		t.Fatal("matching target version did not reuse provider-native state")
	}
	document, _, err := backend.Codec.Encode(request)
	if err != nil {
		t.Fatal(err)
	}
	wire := string(document.RawBytes())
	if !strings.Contains(wire, `"previous_response_id":"provider_response_from_previous_target_version"`) {
		t.Fatalf("native continuation missing: %s", wire)
	}
	if strings.Contains(wire, "turn one") || strings.Contains(wire, "answer one") || !strings.Contains(wire, "turn two") {
		t.Fatalf("native continuation did not send only the current delta: %s", wire)
	}
}

package gemini

import (
	"context"
	"net/http"
	"testing"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
)

type credentialResolver struct{}

func (credentialResolver) ResolveCredential(context.Context, string, string) (string, error) {
	return "gemini-token", nil
}

func TestRuntimeAdmitsOnlyExactGeminiInteractionsTarget(t *testing.T) {
	bundle := NewRuntime(http.DefaultClient, credentialResolver{})
	target := geminiTarget()
	backend, err := bundle.BackendResolver.ResolveBackend(target)
	if err != nil {
		t.Fatal(err)
	}
	if !backend.Target.Equal(target) {
		t.Fatalf("backend target = %#v, want %#v", backend.Target, target)
	}
	falseResponses := provider.NewTargetSnapshot("gemini", "gemini", "https://generativelanguage.googleapis.com/v1", "env:GEMINI_API_KEY", protocolkind.Responses, "responses", delivery.BufferedDelivery())
	falseResponses.Model = "operator-selected-model"
	if _, err := bundle.BackendResolver.ResolveBackend(falseResponses); err == nil {
		t.Fatal("Gemini runtime accepted a false Responses identity")
	}
	otherProvider := provider.NewTargetSnapshot("openai", "openai", "https://api.openai.com/v1", "env:OPENAI_API_KEY", protocolkind.Interactions, "interactions_stream", delivery.StreamingDelivery(delivery.FramingSSE))
	otherProvider.Model = "operator-selected-model"
	if _, err := bundle.BackendResolver.ResolveBackend(otherProvider); err == nil {
		t.Fatal("Gemini runtime accepted another provider's target")
	}
}

func geminiTarget() provider.TargetSnapshot {
	target := provider.NewTargetSnapshot("gemini", string(profile.ProviderSpecGemini), "https://generativelanguage.googleapis.com/v1", "env:GEMINI_API_KEY", protocolkind.Interactions, "interactions_stream", delivery.StreamingDelivery(delivery.FramingSSE))
	target.Model = "operator-selected-model"
	return target
}

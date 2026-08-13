package gemini

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

type liveCredentialResolver struct{ token string }

func (r liveCredentialResolver) ResolveCredential(context.Context, string, string) (string, error) {
	return r.token, nil
}

func TestGeminiNativeTextAndGoogleSearchLive(t *testing.T) {
	if strings.TrimSpace(os.Getenv("SWOBU_LIVE_GEMINI")) != "1" {
		t.Skip("set SWOBU_LIVE_GEMINI=1 to run Gemini native live certification") // swobu:lint ignore no-test-skip because=live provider spend requires explicit opt-in
	}
	authMode := strings.TrimSpace(os.Getenv("SWOBU_LIVE_GEMINI_AUTH"))
	credentialRef := "env:GEMINI_API_KEY"
	var credentialProvider liveCredentialResolver
	if authMode == "adc" {
		credentialRef = ""
	} else {
		key := strings.TrimSpace(os.Getenv("GEMINI_API_KEY"))
		if key == "" {
			t.Skip("set GEMINI_API_KEY or SWOBU_LIVE_GEMINI_AUTH=adc to run Gemini native live certification") // swobu:lint ignore no-test-skip because=live provider credential is operator-supplied
		}
		credentialProvider.token = key
	}

	runtime := NewRuntime(http.DefaultClient, credentialProvider)
	target := provider.NewTargetSnapshot(
		"gemini-live", string(profile.ProviderSpecGemini),
		"https://generativelanguage.googleapis.com/v1", credentialRef,
		protocolkind.Interactions, profile.FrameSSEEvent, "interactions_stream",
	)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	probe, err := runtime.Discovery.ProbeTarget(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	if len(probe.Deployments) == 0 {
		t.Fatal("Gemini live discovery returned no models")
	}
	model := strings.TrimSpace(os.Getenv("SWOBU_LIVE_GEMINI_MODEL"))
	if model == "" {
		t.Fatal("set SWOBU_LIVE_GEMINI_MODEL to one model ID returned by live discovery")
	}
	foundModel := false
	for _, deployment := range probe.Deployments {
		if deployment.Name == model {
			foundModel = true
			break
		}
	}
	if !foundModel {
		t.Fatalf("SWOBU_LIVE_GEMINI_MODEL %q was not returned by live discovery", model)
	}
	target.Model = model

	text := executeGeminiLive(t, ctx, runtime.BackendResolver, target, canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify(target.Model),
		Items: []canonical.CanonicalItem{
			canonicaltest.Message(t, canonical.MessageRoleUser, "Reply with exactly SWOBU_GEMINI_LIVE_OK."),
		},
	}), "text")
	if !responseContainsText(text, "SWOBU_GEMINI_LIVE_OK") {
		t.Fatal("Gemini live text response did not contain the requested marker")
	}

	search := executeGeminiLive(t, ctx, runtime.BackendResolver, target, canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify(target.Model),
		Items: []canonical.CanonicalItem{
			canonicaltest.ToolDeclarations(t, canonical.NewWebSearchDeclaration()),
			canonicaltest.Message(t, canonical.MessageRoleUser, "Use Google Search to identify the official Swobu website and answer with a cited source."),
		},
	}), "search")
	assertLiveSearchLifecycle(t, search)

	t.Logf("Gemini live certification passed with discovered model %q", target.Model)
}

func executeGeminiLive(
	t *testing.T,
	ctx context.Context,
	resolver provider.BackendResolver,
	target provider.TargetSnapshot,
	request canonical.CanonicalRequest,
	label string,
) canonical.CanonicalResponse {
	t.Helper()
	backend, err := resolver.ResolveBackend(target)
	if err != nil {
		t.Fatal(err)
	}
	names, _, err := provider.BuildAttemptToolNames(request)
	if err != nil {
		t.Fatal(err)
	}
	providerRequest := provider.Request{
		ExchangeID: "gemini_live_" + label,
		Canonical:  request,
		Delivery:   delivery.StreamingDelivery(delivery.FramingSSE),
		ToolNames:  names,
	}
	document, _, err := backend.Codec.Encode(providerRequest)
	if err != nil {
		t.Fatal(err)
	}
	ingress, err := backend.Transport.Send(ctx, document)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := backend.Codec.Decode(ctx, providerRequest, ingress)
	if err != nil {
		t.Fatal(err)
	}
	bound := canonical.NewBoundResponseIdentityStream(decoded.Stream, canonical.ResponseBinding{
		SwobuID: canonical.NewSwobuResponseID(fmt.Sprintf("resp_gemini_live_%s", label)),
	})
	closed, err := canonical.ReadClosedEnvelope(ctx, canonical.NewValidatedResponseStream(bound), canonical.EnvResponse)
	if err != nil {
		t.Fatal(err)
	}
	response, err := closed.ProjectResponse()
	if err != nil {
		t.Fatal(err)
	}
	return response.Clone()
}

func responseContainsText(response canonical.CanonicalResponse, want string) bool {
	for _, item := range response.Items() {
		message, ok := item.Message()
		if !ok {
			continue
		}
		for _, part := range message.Content() {
			text, ok := part.Text()
			if ok && strings.Contains(text.Text(), want) {
				return true
			}
		}
	}
	return false
}

func assertLiveSearchLifecycle(t *testing.T, response canonical.CanonicalResponse) {
	t.Helper()
	foundCall, foundResult, foundCitation := false, false, false
	for _, item := range response.Items() {
		if call, ok := item.ToolCall(); ok && call.Tool() == canonical.WebSearchToolKey() {
			foundCall = true
		}
		if result, ok := item.ToolResult(); ok {
			if search, ok := result.WebSearch(); ok {
				if _, failed := search.Failure(); failed {
					continue
				}
				foundResult = true
			}
		}
		if message, ok := item.Message(); ok {
			for _, part := range message.Content() {
				if len(part.Citations()) > 0 {
					foundCitation = true
				}
			}
		}
	}
	if !foundCall || !foundResult || !foundCitation {
		t.Fatalf("Gemini live Search lifecycle incomplete: call=%t result=%t citation=%t", foundCall, foundResult, foundCitation)
	}
}

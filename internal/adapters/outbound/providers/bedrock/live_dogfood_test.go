//go:build integration_live

package bedrock

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/adapters/outbound/credentials"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestLiveBedrockMantleCatalogAuthenticationPrecedence(t *testing.T) {
	if strings.TrimSpace(os.Getenv("SWOBU_LIVE_BEDROCK_DOGFOOD")) != "1" {
		t.Skip("set SWOBU_LIVE_BEDROCK_DOGFOOD=1 to probe live Bedrock Mantle auth modes")
	}
	region := firstNonEmpty(os.Getenv("AWS_REGION"), os.Getenv("AWS_DEFAULT_REGION"), "us-east-1")
	endpoint := strings.TrimSpace(os.Getenv("SWOBU_BEDROCK_MANTLE_ENDPOINT"))
	if endpoint == "" {
		t.Fatal("set SWOBU_BEDROCK_MANTLE_ENDPOINT to the API URL published for the selected model")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	exec := NewExecutor(http.DefaultClient)
	exec.credentials = credentials.NewEnvResolver()

	if strings.TrimSpace(os.Getenv("AWS_BEARER_TOKEN_BEDROCK")) != "" {
		t.Run("explicit_api_key", func(t *testing.T) {
			assertLiveCatalog(t, ctx, exec, region, endpoint, "explicit_api_key", "env:AWS_BEARER_TOKEN_BEDROCK")
		})
	}
	t.Run("aws_identity", func(t *testing.T) {
		assertLiveCatalog(t, ctx, exec, region, endpoint, "aws_identity", "")
	})
}

func assertLiveCatalog(t *testing.T, ctx context.Context, exec BackendAdapter, region string, endpoint string, authMode string, credentialRef string) {
	t.Helper()
	target := provider.NewBedrockTargetSnapshot(
		"live-bedrock-dogfood",
		endpoint,
		credentialRef,
		protocolkind.Responses,
		profile.FrameHTTPJSONBody,
		"responses",
		// The signing region is the durable fact, fixed at construction so the
		// SigV4 catalog call signs with the authored region (never empty), which
		// would otherwise be rejected as "scoped to a valid region".
		region)

	models, err := exec.ListDeployments(ctx, target)
	if err != nil {
		t.Fatalf("%s catalog probe failed: %v", authMode, err)
	}
	if len(models) == 0 {
		t.Fatalf("%s catalog probe returned no models", authMode)
	}
	t.Logf("%s catalog probe returned %d models", authMode, len(models))
}

// TestLiveBedrockMantleSendSignsBedrockRegion proves the Slice 050 invariant: a
// Send over the openai-compat namespace (/openai/v1) signs the request with
// target.BedrockRegion and the preserved bedrockSigningService ("bedrock"), and
// the real Mantle backend accepts it. This is the live proof that SigV4 region
// is now drawn from the durable region fact rather than parsed from the endpoint
// host (the endpoint namespace no longer touches signing).
//
// Gated behind SWOBU_LIVE_BEDROCK_DOGFOOD=1. The model and its AWS-published
// API URL are both explicit; Swobu never maps model identity to a namespace.
func TestLiveBedrockMantleSendSignsBedrockRegion(t *testing.T) {
	if strings.TrimSpace(os.Getenv("SWOBU_LIVE_BEDROCK_DOGFOOD")) != "1" {
		t.Skip("set SWOBU_LIVE_BEDROCK_DOGFOOD=1 to probe live Bedrock Mantle SigV4 Send")
	}
	region := firstNonEmpty(os.Getenv("AWS_REGION"), os.Getenv("AWS_DEFAULT_REGION"), "us-east-1")
	model := firstNonEmpty(os.Getenv("SWOBU_BEDROCK_MODEL"), "openai.gpt-oss-120b")
	endpoint := strings.TrimSpace(os.Getenv("SWOBU_BEDROCK_MANTLE_ENDPOINT"))
	if endpoint == "" {
		t.Fatal("set SWOBU_BEDROCK_MANTLE_ENDPOINT to the API URL published for the selected model")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	target := provider.NewBedrockTargetSnapshot(
		"live-bedrock-send",
		endpoint,
		"",
		protocolkind.Responses,
		profile.FrameHTTPJSONBody,
		"responses",
		// The signing region is the durable fact, NOT parsed from the host, fixed
		// at construction. Use a distinct field to prove it drives signing
		// regardless of how the endpoint namespace was authored.
		region)
	target.Model = model

	exec := NewExecutor(http.DefaultClient)
	body := carrier.NewDocument(
		protocolkind.Responses,
		"application/json",
		nil,
		[]byte(`{"model": "`+model+`", "input": [{"type":"message","role":"user","content":[{"type":"input_text","text":"ping"}]}], "store": false}`),
		carrier.Meta{},
	)
	ingress, err := exec.Send(ctx, target, body)
	if err != nil {
		t.Fatalf("live Send over %s failed (signing region %q, service %q): %v", endpoint, region, bedrockSigningService, err)
	}
	defer func() {
		if s, ok := ingress.(provider.StreamIngress); ok {
			_ = s.Stream.Body.Close()
		}
	}()
	t.Logf("live Send over %s succeeded with signing region %q, service %q", endpoint, region, bedrockSigningService)
}

// TestLiveBedrockMantleResponsesReasoningReplayRoundTrip reproduces the replay
// half of codex#28902 against xai.grok-4.3 (the permanent closed-reasoning proxy
// vehicle, on /openai/v1): turn one emits encrypted reasoning; Swobu decodes it
// into a canonical reasoning item carrying its rs_ id; a tool result is appended
// and Swobu re-encodes the next store=false request; the real Mantle backend
// accepts the reasoning item with its preserved id. This is the live proof for
// RFC G2 §7.1–§7.5 ("preserve only") — the id survives ingress → decode → encode
// verbatim, and empty-id replay (which grok accepts) is never rejected.
//
// Gated behind SWOBU_LIVE_BEDROCK_DOGFOOD=1 and grok-4.3 (closed-reasoning
// emits real rs_ ids). Override SWOBU_BEDROCK_MODEL to pick a different
// closed-reasoning model.
func TestLiveBedrockMantleResponsesReasoningReplayRoundTrip(t *testing.T) {
	if strings.TrimSpace(os.Getenv("SWOBU_LIVE_BEDROCK_DOGFOOD")) != "1" {
		t.Skip("set SWOBU_LIVE_BEDROCK_DOGFOOD=1 to probe live Bedrock Mantle reasoning replay")
	}
	region := firstNonEmpty(os.Getenv("AWS_REGION"), os.Getenv("AWS_DEFAULT_REGION"), "us-east-1")
	// This proof uses the API base documented for the selected model rather than
	// deriving a namespace from model ownership or licensing.
	model := firstNonEmpty(os.Getenv("SWOBU_BEDROCK_MODEL"), "xai.grok-4.3")
	endpoint := firstNonEmpty(
		os.Getenv("SWOBU_BEDROCK_MANTLE_ENDPOINT"),
		"https://bedrock-mantle."+region+".api.aws/openai/v1",
	)
	if endpoint == "" {
		t.Fatal("Bedrock Mantle endpoint is empty")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	exec := NewExecutor(http.DefaultClient)
	exec.credentials = credentials.NewEnvResolver()

	credentialRef := ""
	if strings.TrimSpace(os.Getenv("AWS_BEARER_TOKEN_BEDROCK")) != "" {
		credentialRef = "env:AWS_BEARER_TOKEN_BEDROCK"
	}
	target := provider.NewBedrockTargetSnapshot(
		"live-bedrock-replay",
		endpoint,
		credentialRef,
		protocolkind.Responses,
		profile.FrameHTTPJSONBody,
		"responses",
		region)
	target.Model = model

	backend, err := exec.ResolveBackend(target)
	if err != nil {
		t.Fatalf("ResolveBackend: %v", err)
	}

	// Turn one: elicit encrypted reasoning by asking for reasoning effort.
	turnOne := carrier.NewDocument(
		protocolkind.Responses,
		"application/json",
		nil,
		[]byte(`{"model": "`+model+`", "reasoning": {"effort":"low", "summary":"auto"}, "input": [{"type":"message","role":"user","content":[{"type":"input_text","text":"Think briefly, then say the word pong."}]}], "include": ["reasoning.encrypted_content"], "store": false}`),
		carrier.Meta{},
	)
	ingress, err := backend.Transport.Send(ctx, turnOne)
	if err != nil {
		t.Fatalf("turn one live Send failed over %s: %v", endpoint, err)
	}
	defer func() {
		if s, ok := ingress.(provider.StreamIngress); ok {
			_ = s.Stream.Body.Close()
		}
	}()

	// Decode turn one through the mantleResponsesCodec seam. The reasoning item
	// must carry its preserved rs_ id into the canonical value. The decode request
	// is target-agnostic; only its Canonical state + buffered Delivery matter.
	decoded, err := backend.Codec.Decode(ctx, provider.Request{
		Canonical: canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify(model)}),
		Delivery:  delivery.BufferedDelivery(),
	}, ingress)
	if err != nil {
		t.Fatalf("turn one decode failed: %v", err)
	}
	var turnOneReasoningID string
	var turnOneEncrypted string
	for {
		event, err := decoded.Stream.Next(ctx)
		if err != nil {
			break
		}
		if event.Kind != canonical.EventItemCompleted {
			continue
		}
		item := event.Payload.(canonical.ItemEvent).Payload.(canonical.ItemCompletedPayload).Item
		reasoning, ok := item.Reasoning()
		if !ok {
			continue
		}
		if replay, ok := reasoning.Opaque().Responses(); ok {
			turnOneEncrypted = replay.EncryptedContent
			turnOneReasoningID = replay.ItemID
		}
	}
	if turnOneEncrypted == "" {
		t.Skipf("model %s returned no encrypted reasoning over %s; cannot exercise replay", model, endpoint)
	}
	t.Logf("turn one reasoning id=%q encrypted-content-bytes=%d", turnOneReasoningID, len(turnOneEncrypted))

	// Turn two: re-encode the preserved reasoning item (id + encrypted content)
	// into the next store=false request. Encode the canonical value built from
	// the decoded replay; the seam must emit the id verbatim, no rewrite.
	opaque, err := canonical.NewResponsesOpaqueThinking(canonical.ResponsesReasoningReplay{EncryptedContent: turnOneEncrypted, ItemID: turnOneReasoningID})
	if err != nil {
		t.Fatalf("reconstruct opaque thinking: %v", err)
	}
	summary, err := canonical.NewReasoningPart(canonical.ReasoningPartSummary, "previous turn")
	if err != nil {
		t.Fatal(err)
	}
	reasoningItem, err := canonical.NewReasoningItem([]canonical.ReasoningPart{summary}, opaque)
	if err != nil {
		t.Fatal(err)
	}
	turnTwoRequest := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify(model),
		Items: []canonical.CanonicalItem{reasoningItem, canonicaltest.Message(t, canonical.MessageRoleUser, "now say the word quux")},
	})
	names, _, err := provider.BuildAttemptToolNames(turnTwoRequest)
	if err != nil {
		t.Fatal(err)
	}
	turnTwoDoc, _, err := backend.Codec.Encode(provider.Request{Canonical: turnTwoRequest, ToolNames: names})
	if err != nil {
		t.Fatalf("turn two encode failed: %v", err)
	}
	if turnOneReasoningID != "" && !bytes.Contains(turnTwoDoc.RawBytes(), []byte(`"id":"`+turnOneReasoningID+`"`)) {
		t.Fatalf("turn two encode dropped the preserved reasoning id %q: %s", turnOneReasoningID, turnTwoDoc.RawBytes())
	}
	if bytes.Contains(turnTwoDoc.RawBytes(), []byte(`"id":"rsn_`)) && !bytes.Contains(turnTwoDoc.RawBytes(), []byte(`"id":"`+turnOneReasoningID+`"`)) {
		t.Fatalf("turn two encode rewrote the reasoning id: %s", turnTwoDoc.RawBytes())
	}

	// Fire turn two at the real backend. grok-4.3 enforces no prefix rule, so it
	// must accept the replayed reasoning item (status 2xx), proving the preserved
	// id is provider-acceptable — the empirical claim RFC §8.2/§8.4 rests on.
	turnTwoIngress, err := backend.Transport.Send(ctx, turnTwoDoc)
	if err != nil {
		t.Fatalf("turn two live Send rejected the replayed reasoning (id %q): %v", turnOneReasoningID, err)
	}
	defer func() {
		if s, ok := turnTwoIngress.(provider.StreamIngress); ok {
			_ = s.Stream.Body.Close()
		}
	}()
	t.Logf("turn two live Send accepted replayed reasoning id %q over %s", turnOneReasoningID, endpoint)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

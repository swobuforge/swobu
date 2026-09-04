package opencodezen

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/adapters/outbound/credentials"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/domain/thread"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

const openCodeZenFreeCharacterizationModel = "mimo-v2.5-free"

type openCodeZenCharacterizationEvidence struct {
	Schema             string `json:"schema"`
	Provider           string `json:"provider"`
	ProviderSpec       string `json:"provider_spec"`
	Endpoint           string `json:"endpoint"`
	Protocol           string `json:"protocol"`
	Delivery           string `json:"delivery"`
	Model              string `json:"model"`
	ObservedAt         string `json:"observed_at"`
	Source             string `json:"source"`
	RequestCount       int    `json:"request_count"`
	SuccessfulDecodes  int    `json:"successful_decodes"`
	SameThreadStable   bool   `json:"same_thread_stable"`
	DifferentDistinct  bool   `json:"different_thread_distinct"`
	RawIdentityAbsent  bool   `json:"raw_identity_absent"`
	SameProjectionSHA  string `json:"same_projection_sha256"`
	OtherProjectionSHA string `json:"other_projection_sha256"`
}

func TestLiveOpenCodeZenFreeModelAcceptsProjectedThreadIdentity(t *testing.T) {
	if os.Getenv("SWOBU_LIVE_OPENCODE_ZEN") != "1" {
		t.Skip("set SWOBU_LIVE_OPENCODE_ZEN=1 to run the live free-model characterization")
	}
	if os.Getenv("OPENCODE_ZEN_API_KEY") == "" {
		t.Fatal("OPENCODE_ZEN_API_KEY is required for live OpenCode Zen characterization")
	}
	evidencePath := os.Getenv("SWOBU_OPENCODE_ZEN_EVIDENCE_PATH")
	if evidencePath == "" {
		t.Fatal("SWOBU_OPENCODE_ZEN_EVIDENCE_PATH is required so the sanitized observation has an explicit destination")
	}

	client := &http.Client{Timeout: 60 * time.Second}
	target := provider.NewTargetSnapshot(
		"opencode-zen-live-characterization",
		string(profile.ProviderSpecOpenCodeZen),
		"https://opencode.ai/zen/v1",
		"env:OPENCODE_ZEN_API_KEY",
		protocolkind.ChatCompletions,
		"chat_completions",
		delivery.BufferedDelivery(),
	)
	target.Model = openCodeZenFreeCharacterizationModel
	backend, err := NewRuntime(client, credentials.NewEnvResolver()).BackendResolver.ResolveBackend(target)
	if err != nil {
		t.Fatal(err)
	}

	const rawIdentity = "swobu-live-characterization-thread-a"
	firstThread, err := thread.Derive("client/x-opencode-session/v1", "characterization-workspace", rawIdentity)
	if err != nil {
		t.Fatal(err)
	}
	secondThread, err := thread.Derive("client/x-opencode-session/v1", "characterization-workspace", "swobu-live-characterization-thread-b")
	if err != nil {
		t.Fatal(err)
	}

	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify(openCodeZenFreeCharacterizationModel),
		Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "Reply with OK.")},
	})
	var projected []string
	for _, threadID := range []thread.ID{firstThread, firstThread, secondThread} {
		attempt := provider.Request{
			Attempt:   provider.AttemptContext{ThreadID: threadID},
			Canonical: request,
			Delivery:  delivery.BufferedDelivery(),
		}
		document, _, err := backend.Codec.Encode(attempt)
		if err != nil {
			t.Fatal(err)
		}
		projected = append(projected, document.Header.Get("X-Opencode-Session"))
		ingress, err := backend.Transport.Send(context.Background(), document)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := backend.Codec.Decode(context.Background(), attempt, ingress)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := canonical.ReadClosedEnvelope(context.Background(), decoded.Stream, canonical.EnvResponse); err != nil {
			t.Fatalf("decode live OpenCode Zen response: %v", err)
		}
	}

	if len(projected) != 3 || projected[0] == "" || projected[0] != projected[1] || projected[0] == projected[2] {
		t.Fatalf("projected OpenCode sessions do not satisfy continuity contract")
	}
	if projected[0] == rawIdentity || projected[1] == rawIdentity || projected[2] == rawIdentity {
		t.Fatal("raw client identity reached the OpenCode provider document")
	}
	evidence := openCodeZenCharacterizationEvidence{
		Schema:             "swobu.provider-characterization/v1",
		Provider:           "OpenCode Zen",
		ProviderSpec:       string(profile.ProviderSpecOpenCodeZen),
		Endpoint:           "https://opencode.ai/zen/v1/chat/completions",
		Protocol:           "chat_completions",
		Delivery:           "buffered",
		Model:              openCodeZenFreeCharacterizationModel,
		ObservedAt:         time.Now().UTC().Format(time.RFC3339),
		Source:             "live",
		RequestCount:       len(projected),
		SuccessfulDecodes:  len(projected),
		SameThreadStable:   projected[0] == projected[1],
		DifferentDistinct:  projected[0] != projected[2],
		RawIdentityAbsent:  projected[0] != rawIdentity && projected[1] != rawIdentity && projected[2] != rawIdentity,
		SameProjectionSHA:  sha256String(projected[0]),
		OtherProjectionSHA: sha256String(projected[2]),
	}
	raw, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	if err := os.MkdirAll(filepath.Dir(evidencePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(evidencePath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestOpenCodeZenThreadProjectionHasLiveFreeModelEvidence(t *testing.T) {
	raw, err := os.ReadFile("testdata/characterization/opencode-zen-free-chat-thread-projection-live-2026-09-04.json")
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			t.Fatal("missing sanitized OpenCode Zen live characterization evidence")
		}
		t.Fatal(err)
	}
	var evidence openCodeZenCharacterizationEvidence
	if err := json.Unmarshal(raw, &evidence); err != nil {
		t.Fatal(err)
	}
	if evidence.Schema != "swobu.provider-characterization/v1" || evidence.ProviderSpec != string(profile.ProviderSpecOpenCodeZen) || evidence.Source != "live" {
		t.Fatalf("unexpected evidence identity: %#v", evidence)
	}
	if evidence.Model != openCodeZenFreeCharacterizationModel || evidence.Protocol != "chat_completions" || evidence.Delivery != "buffered" {
		t.Fatalf("evidence does not describe the bounded free-model rail: %#v", evidence)
	}
	if evidence.RequestCount != 3 || evidence.SuccessfulDecodes != 3 || !evidence.SameThreadStable || !evidence.DifferentDistinct || !evidence.RawIdentityAbsent {
		t.Fatalf("evidence does not prove the Thread projection contract: %#v", evidence)
	}
	if len(evidence.SameProjectionSHA) != sha256.Size*2 || len(evidence.OtherProjectionSHA) != sha256.Size*2 || evidence.SameProjectionSHA == evidence.OtherProjectionSHA {
		t.Fatalf("evidence projection digests are invalid")
	}
	if _, err := time.Parse(time.RFC3339, evidence.ObservedAt); err != nil {
		t.Fatalf("invalid observation timestamp: %v", err)
	}
}

func sha256String(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

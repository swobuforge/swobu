package target_config

import (
	"strings"
	"testing"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
	"github.com/swobuforge/swobu/internal/profile"
)

func TestSelectProviderBedrockDoesNotInferPersistedRegionFromEnvironment(t *testing.T) {
	t.Setenv("AWS_REGION", "eu-west-2")
	w := NewTargetConfig("dev", readmodel.RouteReadModel{ID: "aws"}, nil, nil)

	w.SelectProvider(string(profile.ProviderSpecBedrock))

	if got := w.Draft.Get().Locator; got != "" {
		t.Fatalf("region = %q, want explicit operator selection", got)
	}
	if got := w.BaseURL.Get(); got != "" {
		t.Fatalf("base URL = %q, want empty until region selection", got)
	}
}

func TestBedrockAuthenticationChooserRendersSharedSourceGrammar(t *testing.T) {
	w := NewTargetConfig("dev", readmodel.RouteReadModel{ID: "aws"}, nil, nil)
	w.Draft.Set(readmodel.TargetDraft{ProviderSpec: string(profile.ProviderSpecBedrock), Locator: "us-east-1"})
	w.Open()
	r := newCredentialRow(w, true)
	h, err := testkit.NewHarness(r)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()
	h.Open()
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	frame := h.Frame()
	for _, want := range []string{"environment variable", "file", "paste credential"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("frame missing %q:\n%s", want, frame)
		}
	}
}

func TestBedrockAuthenticationDisplayUsesCredentialRefKind(t *testing.T) {
	cases := map[string]string{
		"env:MY_BEDROCK_TOKEN":                 "environment · MY_BEDROCK_TOKEN",
		"file:~/.config/secrets/bedrock-token": "file · ~/.config/secrets/bedrock-token",
		"secret:bedrock-production":            "stored credential",
	}
	for ref, want := range cases {
		if got := credentialRefDisplay(ref); got != want {
			t.Fatalf("display(%q)=%q want %q", ref, got, want)
		}
	}
}

func TestBedrockEnvironmentSelectionStoresReferenceWithoutProviderValidation(t *testing.T) {
	t.Setenv("MY_BEDROCK_TOKEN", "")
	w := NewTargetConfig("dev", readmodel.RouteReadModel{ID: "aws"}, nil, nil)
	w.Draft.Update(func(d readmodel.TargetDraft) readmodel.TargetDraft {
		d.ProviderSpec = string(profile.ProviderSpecBedrock)
		return d
	})
	r := newCredentialRow(w, false)
	r.saveEnv("MY_BEDROCK_TOKEN")
	if got := w.Error.Get(); got != "" {
		t.Fatalf("error=%q want shared field to defer resolution", got)
	}
	if got := w.Draft.Get().CredentialRef; got != "env:MY_BEDROCK_TOKEN" {
		t.Fatalf("credential=%q", got)
	}
}

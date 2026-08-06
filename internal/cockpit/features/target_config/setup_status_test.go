package target_config

import (
	"strings"
	"testing"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
)

func TestProviderMatrixProjectsSemanticSetupStatus(t *testing.T) {
	tests := []struct {
		name       string
		provider   profile.ProviderID
		locator    string
		credential string
		want       setupStatus
	}{
		{"openai missing credential", profile.ProviderSpecOpenAI, "", "", setupMissingCredential},
		{"openai ready", profile.ProviderSpecOpenAI, "", "env:OPENAI_API_KEY", setupReady},
		{"anthropic missing credential", profile.ProviderSpecAnthropic, "", "", setupMissingCredential},
		{"openrouter missing credential", profile.ProviderSpecOpenRouter, "", "", setupMissingCredential},
		{"ollama default ready", profile.ProviderSpecOllama, "", "", setupReady},
		{"custom missing backend", profile.ProviderSpecCustom, "", "", setupMissingLocator},
		{"custom remote missing credential", profile.ProviderSpecCustom, "https://example.com/v1", "", setupMissingCredential},
		{"custom loopback ready", profile.ProviderSpecCustom, "http://127.0.0.1:8080/v1", "", setupReady},
		{"azure missing project", profile.ProviderSpecAzure, "", "", setupMissingLocator},
		{"azure missing credential", profile.ProviderSpecAzure, "https://example.services.ai.azure.com/api/projects/demo", "", setupMissingCredential},
		{"azure ready", profile.ProviderSpecAzure, "https://example.services.ai.azure.com/api/projects/demo", "env:AZURE_OPENAI_API_KEY", setupReady},
		{"bedrock missing region", profile.ProviderSpecBedrock, "", "", setupMissingLocator},
		{"bedrock AWS identity ready", profile.ProviderSpecBedrock, profile.EffectiveBedrockAPIURL("eu-west-1", "", protocolkind.Responses), "", setupReady},
		{"chatgpt signed out", profile.ProviderSpecChatGPT, "", "", setupMissingInteractiveAuth},
		{"chatgpt signed in", profile.ProviderSpecChatGPT, "", "secret:chatgpt/session", setupReady},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := NewTargetConfig("dev", readmodel.RouteReadModel{ID: "chat"}, nil, nil)
			config.SelectProvider(string(test.provider))
			if test.locator != "" {
				if test.provider == profile.ProviderSpecBedrock {
					config.Draft.Update(func(d readmodel.TargetDraft) readmodel.TargetDraft {
						d.Locator = profile.BedrockMantleRegionFromEndpoint(test.locator)
						return d
					})
					config.Catalog.Set(catalogOperationState{Result: readmodel.ModelCatalogReadModel{Deployments: []readmodel.ModelDeploymentReadModel{{ID: "model"}}}})
				} else {
					config.BaseURL.Set(test.locator)
				}
			}
			config.Draft.Update(func(d readmodel.TargetDraft) readmodel.TargetDraft {
				d.CredentialRef = test.credential
				return d
			})
			if got := config.setupState().Status; got != test.want {
				t.Fatalf("status = %v, want %v", got, test.want)
			}
		})
	}
}

func TestBedrockReadinessUsesCatalogSuccessNotIdentityEnrichment(t *testing.T) {
	w := NewTargetConfig("dev", readmodel.RouteReadModel{ID: "chat"}, nil, nil)
	w.SelectProvider(string(profile.ProviderSpecBedrock))
	w.Draft.Update(func(d readmodel.TargetDraft) readmodel.TargetDraft { d.Locator = "eu-west-2"; return d })
	if got := w.setupState().Status; got != setupReady {
		t.Fatalf("region-present status = %v, want ready to probe", got)
	}
	w.Catalog.Set(catalogOperationState{Result: readmodel.ModelCatalogReadModel{Deployments: []readmodel.ModelDeploymentReadModel{{ID: "model"}}, BedrockAuthentication: readmodel.BedrockAuthenticationEvidence{Authentication: readmodel.BedrockAuthenticationAWSIdentity, AWSIdentity: &readmodel.AWSIdentityReadModel{State: "identity_probe_failed", Error: "STS unavailable"}}}})
	if got := w.setupState().Status; got != setupReady {
		t.Fatalf("catalog success with STS failure status = %v, want ready", got)
	}
}

func TestMissingSetupTailUsesNeutralDependentRowGrammar(t *testing.T) {
	config := NewTargetConfig("dev", readmodel.RouteReadModel{ID: "chat"}, nil, nil)
	config.SelectProvider(string(profile.ProviderSpecOpenAI))

	frame := testkit.RenderMountedTrimmed(t, TargetConfigTail(config), 100, 8)
	for _, want := range []string{"model", "waiting for setup", "protocol", "waiting for model", "create", "complete setup"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("frame missing %q:\n%s", want, frame)
		}
	}
	for _, forbidden := range []string{"blocked", " first", "setup first"} {
		if strings.Contains(frame, forbidden) {
			t.Fatalf("frame contains obsolete prerequisite copy %q:\n%s", forbidden, frame)
		}
	}
	for _, line := range strings.Split(frame, "\n") {
		if (strings.Contains(line, "waiting for setup") || strings.Contains(line, "waiting for model")) && strings.Contains(line, "↵") {
			t.Fatalf("dependent waiting row must not advertise an action:\n%s", frame)
		}
	}
}

func TestMissingCredentialOwnsFirstVisibleSelection(t *testing.T) {
	config := NewTargetConfig("dev", readmodel.RouteReadModel{ID: "chat"}, nil, nil)
	config.Open()
	config.SelectProvider(string(profile.ProviderSpecOpenAI))
	harness, err := testkit.NewHarness(config)
	if err != nil {
		t.Fatal(err)
	}
	defer harness.Close()
	harness.Open()

	frame := harness.Frame()
	if !strings.Contains(frame, "> credential") {
		t.Fatalf("missing credential must own first visible selection:\n%s", frame)
	}
	if strings.Count(frame, ">") != 1 {
		t.Fatalf("expected one visible selection marker:\n%s", frame)
	}

	// Enter must open the owning credential field, not a dependent tail row.
	harness.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	if frame = harness.Frame(); !strings.Contains(frame, "environment variable") {
		t.Fatalf("credential selection did not open its source menu:\n%s", frame)
	}
}

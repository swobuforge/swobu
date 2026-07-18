package workspaces

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/configstore"
	"github.com/swobuforge/swobu/internal/routing"
)

func testService(t *testing.T) (Service, *configstore.Store, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "swobu.yaml")
	if err := os.WriteFile(path, []byte("schema_version: 1\nworkspaces: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := configstore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	service, err := NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	return service, store, path
}
func openAITarget(id string) TargetDraft {
	return TargetDraft{ID: id, Model: "gpt-5", Protocol: "responses", Connection: Connection{OpenAI: &CredentialConnection{Credential: "env:OPENAI_API_KEY"}}}
}

func TestNewServiceRejectsNilStore(t *testing.T) {
	if _, err := NewService(nil); err == nil {
		t.Fatal("NewService(nil) unexpectedly succeeded")
	}
}

func TestOperatorTargetDraftFinalizesEveryConnectionArm(t *testing.T) {
	for name, test := range map[string]struct {
		protocol   string
		connection Connection
		provider   routing.Provider
	}{
		"openai":     {"responses", Connection{OpenAI: &CredentialConnection{Credential: "env:OPENAI_API_KEY"}}, routing.ProviderOpenAI},
		"anthropic":  {"messages", Connection{Anthropic: &CredentialConnection{Credential: "env:ANTHROPIC_API_KEY"}}, routing.ProviderAnthropic},
		"openrouter": {"chat_completions", Connection{OpenRouter: &CredentialConnection{Credential: "env:OPENROUTER_API_KEY"}}, routing.ProviderOpenRouter},
		"chatgpt":    {"responses_stream", Connection{ChatGPT: &CredentialConnection{Credential: "secretfile:chatgpt/default"}}, routing.ProviderChatGPT},
		"ollama":     {"chat_completions", Connection{Ollama: &OllamaConnection{}}, routing.ProviderOllama},
		"azure":      {"responses", Connection{Azure: &AzureConnection{ProjectEndpoint: "https://example.services.ai.azure.com/api/projects/prod", Credential: "env:AZURE_KEY"}}, routing.ProviderAzure},
		"bedrock":    {"responses_stream", Connection{Bedrock: &BedrockConnection{Region: "eu-west-2"}}, routing.ProviderBedrock},
		"custom":     {"chat_completions", Connection{Custom: &CustomConnection{BaseURL: "https://example.test/v1", Header: &CustomHeader{Name: "Authorization", Credential: "env:CUSTOM_KEY"}}}, routing.ProviderCustom},
	} {
		t.Run(name, func(t *testing.T) {
			target, err := (TargetDraft{ID: "target", Model: "model", Protocol: test.protocol, Connection: test.connection}).routingTarget()
			if err != nil {
				t.Fatal(err)
			}
			if target.Provider() != test.provider {
				t.Fatalf("provider = %q, want %q", target.Provider(), test.provider)
			}
		})
	}
}

func TestServiceSemanticCommandsReturnCommittedHierarchy(t *testing.T) {
	service, _, _ := testService(t)
	ctx := context.Background()
	workspace, err := service.CreateWorkspace(ctx, CreateWorkspace{Slug: "dev", InitialRoute: "chat", Target: openAITarget("a")})
	if err != nil {
		t.Fatal(err)
	}
	if workspace.DefaultRoute != "chat" || len(workspace.Routes) != 1 || len(workspace.Routes[0].Tiers) != 1 {
		t.Fatalf("workspace = %#v", workspace)
	}
	workspace, err = service.CreateTarget(ctx, CreateTarget{Workspace: "dev", Route: "chat", Target: openAITarget("b")})
	if err != nil {
		t.Fatal(err)
	}
	if len(workspace.Routes[0].Tiers) != 2 {
		t.Fatalf("tiers = %#v", workspace.Routes[0].Tiers)
	}
	balanceWith := "b"
	workspace, err = service.CreateTarget(ctx, CreateTarget{Workspace: "dev", Route: "chat", Target: openAITarget("c"), Placement: Placement{BalanceWith: &balanceWith}})
	if err != nil {
		t.Fatal(err)
	}
	if len(workspace.Routes[0].Tiers) != 2 || len(workspace.Routes[0].Tiers[1].Targets) != 2 {
		t.Fatalf("balanced tiers = %#v", workspace.Routes[0].Tiers)
	}
	workspace, err = service.DeleteTarget(ctx, DeleteTarget{Workspace: "dev", Route: "chat", TargetID: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(workspace.Routes[0].Tiers) != 1 || workspace.Routes[0].Tiers[0].Targets[0].ID != "b" {
		t.Fatalf("promoted tiers = %#v", workspace.Routes[0].Tiers)
	}
}

func TestServiceSetCredentialPreservesTargetFactsAcrossRestart(t *testing.T) {
	service, store, path := testService(t)
	ctx := context.Background()
	_, err := service.CreateWorkspace(ctx, CreateWorkspace{Slug: "dev", InitialRoute: "chat", Target: openAITarget("a")})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := service.SetCredential(ctx, SetCredential{Workspace: "dev", Route: "chat", TargetID: "a", Credential: "env:ROTATED"})
	if err != nil {
		t.Fatal(err)
	}
	target := workspace.Routes[0].Tiers[0].Targets[0]
	if target.Model != "gpt-5" || target.Protocol != "responses" || target.Provider != "openai" || target.Connection.OpenAI == nil || target.Connection.OpenAI.Credential != "env:ROTATED" {
		t.Fatalf("target = %#v", target)
	}
	if len(workspace.Routes[0].Tiers) != 1 || len(workspace.Routes[0].Tiers[0].Targets) != 1 {
		t.Fatalf("credential update changed tier placement: %#v", workspace.Routes[0].Tiers)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := configstore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	persisted, _ := reopened.Config().Workspace(mustSlug(t, "dev"))
	route, _ := persisted.Route(mustRoute(t, "chat"))
	actual := route.Tiers()[0].Targets()[0]
	connection, ok := actual.Connection().(routing.OpenAIConnection)
	if actual.Model().String() != "gpt-5" || actual.Protocol().String() != "responses" || actual.Provider() != routing.ProviderOpenAI || !ok || connection.Credential().String() != "env:ROTATED" {
		t.Fatalf("persisted target = %#v", actual)
	}
}

func TestServiceDeletePrimaryPromotesFallbackAcrossRestart(t *testing.T) {
	service, store, path := testService(t)
	ctx := context.Background()
	if _, err := service.CreateWorkspace(ctx, CreateWorkspace{Slug: "dev", InitialRoute: "chat", Target: openAITarget("primary")}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateTarget(ctx, CreateTarget{Workspace: "dev", Route: "chat", Target: openAITarget("fallback")}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.DeleteTarget(ctx, DeleteTarget{Workspace: "dev", Route: "chat", TargetID: "primary"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := configstore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	workspace, ok := reopened.Config().Workspace(mustSlug(t, "dev"))
	if !ok {
		t.Fatal("workspace missing after restart")
	}
	route, ok := workspace.Route(mustRoute(t, "chat"))
	if !ok {
		t.Fatal("route missing after restart")
	}
	tiers := route.Tiers()
	if len(tiers) != 1 || len(tiers[0].Targets()) != 1 || tiers[0].Targets()[0].ID().String() != "fallback" {
		t.Fatalf("restarted primary tier = %#v, want fallback promoted", tiers)
	}
}
func mustSlug(t *testing.T, raw string) routing.WorkspaceSlug {
	t.Helper()
	value, err := routing.ParseWorkspaceSlug(raw)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
func mustRoute(t *testing.T, raw string) routing.RouteName {
	t.Helper()
	value, err := routing.ParseRouteName(raw)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestServiceMapsLastTargetConflict(t *testing.T) {
	service, _, _ := testService(t)
	ctx := context.Background()
	_, _ = service.CreateWorkspace(ctx, CreateWorkspace{Slug: "dev", InitialRoute: "chat", Target: openAITarget("a")})
	_, err := service.DeleteTarget(ctx, DeleteTarget{Workspace: "dev", Route: "chat", TargetID: "a"})
	var command CommandError
	if !errors.As(err, &command) || command.Code != Conflict {
		t.Fatalf("error = %#v", err)
	}
}

func TestCommandErrorCollapsesDomainConflictSentinels(t *testing.T) {
	for _, sentinel := range []error{
		routing.ErrConflict,
		routing.ErrLastTarget,
		routing.ErrLastRoute,
		routing.ErrDefaultReplacementRequired,
	} {
		var command CommandError
		if err := commandError(fmt.Errorf("%w: specific reason", sentinel)); !errors.As(err, &command) || command.Code != Conflict || !strings.Contains(command.Message, "specific reason") {
			t.Errorf("commandError(%v) = %#v", sentinel, err)
		}
	}
}

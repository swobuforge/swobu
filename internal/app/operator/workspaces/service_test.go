package workspaces

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/configstore"
	"github.com/swobuforge/swobu/internal/profile"
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
	return TargetDraft{ID: id, Model: "gpt-5", Protocol: "responses", Connection: StandardConnection("openai", "", "env:OPENAI_API_KEY")}
}

// TestConnectionJSONRoundTripCoversEveryProfile proves that the public
// provider-keyed JSON contract is dynamic at the profile boundary. Each known
// provider keeps its key and strict child grammar without adding a DTO field or
// marshal arm for catalog membership.
func TestConnectionJSONRoundTripCoversEveryProfile(t *testing.T) {
	for _, entry := range profile.All() {
		t.Run(string(entry.ProviderID), func(t *testing.T) {
			source := operatorConnectionForProfile(t, entry)
			raw, err := json.Marshal(source)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}

			var decoded Connection
			if err := json.Unmarshal(raw, &decoded); err != nil {
				t.Fatalf("unmarshal: %v\n%s", err, raw)
			}
			roundTripped, err := json.Marshal(decoded)
			if err != nil {
				t.Fatalf("re-marshal: %v", err)
			}
			if string(roundTripped) != string(raw) {
				t.Fatalf("provider connection changed during JSON round trip:\nfirst:  %s\nsecond: %s", raw, roundTripped)
			}

			var document map[string]json.RawMessage
			if err := json.Unmarshal(raw, &document); err != nil {
				t.Fatalf("decode public JSON: %v", err)
			}
			if len(document) != 1 || document[string(entry.ProviderID)] == nil {
				t.Fatalf("public JSON = %s; want exactly the %q provider key", raw, entry.ProviderID)
			}

			var child map[string]json.RawMessage
			if err := json.Unmarshal(document[string(entry.ProviderID)], &child); err != nil {
				t.Fatalf("decode provider payload: %v", err)
			}
			child["future_field"] = json.RawMessage(`true`)
			document[string(entry.ProviderID)], err = json.Marshal(child)
			if err != nil {
				t.Fatalf("encode unknown-field payload: %v", err)
			}
			unknownFieldJSON, err := json.Marshal(document)
			if err != nil {
				t.Fatalf("encode unknown-field document: %v", err)
			}
			var rejected Connection
			if err := json.Unmarshal(unknownFieldJSON, &rejected); err == nil || !strings.Contains(err.Error(), "unknown field") {
				t.Fatalf("unknown child field error = %v, want strict rejection", err)
			}
		})
	}
}

func TestRunPodOperatorJSONNormalizesEndpointAndFinalizesStandardConnection(t *testing.T) {
	var connection Connection
	if err := json.Unmarshal([]byte(`{"runpod":{"base_url":"abc123","credential":"env:RUNPOD_API_KEY"}}`), &connection); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(connection)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"runpod":{"base_url":"https://api.runpod.ai/v2/abc123/openai/v1","credential":"env:RUNPOD_API_KEY"}}`
	if string(raw) != want {
		t.Fatalf("Runpod JSON = %s, want %s", raw, want)
	}

	finalized, err := connection.RoutingConnection()
	if err != nil {
		t.Fatal(err)
	}
	standard, ok := finalized.(routing.StandardConnection)
	if !ok {
		t.Fatalf("Runpod connection type = %T, want routing.StandardConnection", finalized)
	}
	locator, ok := standard.Locator()
	if !ok || locator.String() != "https://api.runpod.ai/v2/abc123/openai/v1" {
		t.Fatalf("Runpod locator = %q/%t", locator.String(), ok)
	}
}

// TestConnectionDocumentCarriesOnlyShapeDraft guards the operator JSON N+1
// boundary. The public document stays provider-keyed at the wire edge, while
// its Go value has one shape-oriented draft rather than one field per provider.
func TestConnectionDocumentCarriesOnlyShapeDraft(t *testing.T) {
	typeOfConnection := reflect.TypeOf(Connection{})
	if typeOfConnection.NumField() != 1 {
		t.Fatalf("Connection field count = %d, want one shape draft", typeOfConnection.NumField())
	}
	field := typeOfConnection.Field(0)
	if field.Name != "draft" || field.Type != reflect.TypeOf(routing.ConnectionDraft{}) {
		t.Fatalf("Connection field = %s %s, want draft %s", field.Name, field.Type, reflect.TypeOf(routing.ConnectionDraft{}))
	}
}

func TestFixedStandardProvidersRejectBaseURLKeyByPresence(t *testing.T) {
	for _, entry := range profile.All() {
		if entry.ConnectionShape != routing.ConnectionShapeStandard || entry.Locator.Kind != profile.LocatorFixed {
			continue
		}
		for _, baseURL := range []string{"", "https://override.example/v1"} {
			t.Run(string(entry.ProviderID)+"/base_url="+baseURL, func(t *testing.T) {
				raw := fmt.Sprintf(`{"%s":{"base_url":%q,"credential":"env:TEST_TOKEN"}}`, entry.ProviderID, baseURL)
				var connection Connection
				if err := json.Unmarshal([]byte(raw), &connection); err == nil || !strings.Contains(err.Error(), `field "base_url" is not recognized`) {
					t.Fatalf("fixed-provider base_url error = %v", err)
				}
			})
		}
	}
}

func TestCredentialUnsupportedStandardProvidersRejectCredentialKey(t *testing.T) {
	for _, entry := range profile.All() {
		if entry.ConnectionShape != routing.ConnectionShapeStandard || entry.Credential.Requirement != profile.CredentialUnsupported {
			continue
		}
		t.Run(string(entry.ProviderID), func(t *testing.T) {
			raw := fmt.Sprintf(`{"%s":{"base_url":"https://provider.example/v1","credential":"env:UNSUPPORTED_TOKEN"}}`, entry.ProviderID)
			var connection Connection
			if err := json.Unmarshal([]byte(raw), &connection); err == nil || !strings.Contains(err.Error(), `field "credential" is not recognized`) {
				t.Fatalf("unsupported credential key error = %v", err)
			}
		})
	}
}

func TestFuturecloudFixedLocatorJSONGrammarNeedsNoOperatorDTOField(t *testing.T) {
	future := profile.Profile{
		ProviderID:      "futurecloud",
		ConnectionShape: routing.ConnectionShapeStandard,
		Locator:         profile.LocatorSpec{Kind: profile.LocatorFixed, Default: "https://api.futurecloud.example/v1"},
		Credential:      profile.CredentialSpec{Requirement: profile.CredentialRequired, Authoring: profile.CredentialAuthoringReference},
	}
	for _, raw := range []string{
		`{"base_url":"","credential":"env:FUTURECLOUD_API_KEY"}`,
		`{"base_url":"https://override.example/v1","credential":"env:FUTURECLOUD_API_KEY"}`,
	} {
		if err := validateStandardConnectionJSONKeys([]byte(raw), future); err == nil || !strings.Contains(err.Error(), `connection.futurecloud: field "base_url" is not recognized`) {
			t.Fatalf("futurecloud fixed base_url error = %v", err)
		}
	}
}

func operatorConnectionForProfile(t *testing.T, entry profile.Profile) Connection {
	t.Helper()
	provider := string(entry.ProviderID)
	switch entry.ConnectionShape {
	case routing.ConnectionShapeStandard:
		switch entry.Locator.Kind {
		case profile.LocatorFixed:
			return StandardConnection(provider, "", operatorCredentialForProfile(entry))
		case profile.LocatorBaseURL:
			return StandardConnection(provider, "https://futurecloud.example/v1", operatorCredentialForProfile(entry))
		case profile.LocatorAzureProject:
			return StandardConnection(provider, "https://futurecloud.services.ai.azure.com/api/projects/demo", operatorCredentialForProfile(entry))
		default:
			t.Fatalf("standard provider %q has unsupported locator kind %d", provider, entry.Locator.Kind)
		}
	case routing.ConnectionShapeZAI:
		return ZAIConnectionDocument("coding_plan", "env:TEST_TOKEN")
	case routing.ConnectionShapeBedrock:
		return BedrockConnectionDocument("us-east-1", "https://bedrock-mantle.us-east-1.api.aws/openai/v1", "env:TEST_TOKEN")
	case routing.ConnectionShapeCustom:
		return CustomConnectionDocument("https://futurecloud.example/v1", &CustomHeader{Name: "Authorization", Credential: "env:TEST_TOKEN"})
	default:
		t.Fatalf("provider %q has unsupported connection shape %d", provider, entry.ConnectionShape)
	}
	return Connection{}
}

func operatorCredentialForProfile(entry profile.Profile) string {
	if entry.Credential.Requirement == profile.CredentialUnsupported {
		return ""
	}
	return "env:TEST_TOKEN"
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
		"openai":     {"responses", StandardConnection("openai", "", "env:OPENAI_API_KEY"), routing.Provider("openai")},
		"anthropic":  {"messages", StandardConnection("anthropic", "", "env:ANTHROPIC_API_KEY"), routing.Provider("anthropic")},
		"deepseek":   {"", StandardConnection("deepseek", "", "env:DEEPSEEK_API_KEY"), routing.Provider("deepseek")},
		"kimi":       {"", StandardConnection("kimi", "", "env:MOONSHOT_API_KEY"), routing.Provider("kimi")},
		"together":   {"", StandardConnection("together", "", "env:TOGETHER_API_KEY"), routing.Provider("together")},
		"deepinfra":  {"", StandardConnection("deepinfra", "", "env:DEEPINFRA_TOKEN"), routing.Provider("deepinfra")},
		"scaleway":   {"responses_stream", StandardConnection("scaleway", "https://dedicated.example/v1", ""), routing.Provider("scaleway")},
		"sambanova":  {"messages_stream", StandardConnection("sambanova", "https://stack.example/v1", "env:SAMBANOVA_API_KEY"), routing.Provider("sambanova")},
		"stepfun":    {"chat_completions_stream", StandardConnection("stepfun", "https://api.stepfun.com/step_plan/v1", "env:STEP_API_KEY"), routing.Provider("stepfun")},
		"friendli":   {"messages_stream", StandardConnection("friendli", "https://friendli-gateway.example/v1", ""), routing.Provider("friendli")},
		"llm7":       {"", StandardConnection("llm7", "", ""), routing.Provider("llm7")},
		"ovhcloud":   {"", StandardConnection("ovhcloud", "", ""), routing.Provider("ovhcloud")},
		"modelscope": {"", StandardConnection("modelscope", "", "env:MODELSCOPE_TOKEN"), routing.Provider("modelscope")},
		"runpod":     {"responses", StandardConnection("runpod", "abc123", "env:RUNPOD_API_KEY"), routing.Provider("runpod")},
		"nebius":     {"responses", StandardConnection("nebius", "", "env:NEBIUS_API_KEY"), routing.Provider("nebius")},
		"gmi":        {"messages", StandardConnection("gmi", "", "env:GMI_API_KEY"), routing.Provider("gmi")},
		"groq":       {"responses_stream", StandardConnection("groq", "", "env:GROQ_API_KEY"), routing.Provider("groq")},
		"fireworks":  {"responses", StandardConnection("fireworks", "https://direct.example/v1", "env:FIREWORKS_API_KEY"), routing.Provider("fireworks")},
		"openrouter": {"chat_completions", StandardConnection("openrouter", "", "env:OPENROUTER_API_KEY"), routing.Provider("openrouter")},
		"zai":        {"", ZAIConnectionDocument("coding_plan", "env:ZAI_API_KEY"), routing.Provider("zai")},
		"chatgpt":    {"", StandardConnection("chatgpt", "", "secretfile:chatgpt/default"), routing.Provider("chatgpt")},
		"ollama":     {"chat_completions", StandardConnection("ollama", "", ""), routing.Provider("ollama")},
		"lmstudio":   {"responses", StandardConnection("lmstudio", "", "env:LM_API_TOKEN"), routing.Provider("lmstudio")},
		"vllm":       {"responses", StandardConnection("vllm", "", "env:VLLM_API_KEY"), routing.Provider("vllm")},
		"azure":      {"responses", StandardConnection("azure", "https://example.services.ai.azure.com/api/projects/prod", "env:AZURE_KEY"), routing.Provider("azure")},
		"bedrock":    {"responses", BedrockConnectionDocument("eu-west-2", "https://bedrock-mantle.eu-west-2.api.aws/v1", ""), routing.Provider("bedrock")},
		"custom":     {"chat_completions", CustomConnectionDocument("https://example.test/v1", &CustomHeader{Name: "Authorization", Credential: "env:CUSTOM_KEY"}), routing.Provider("custom")},
	} {
		t.Run(name, func(t *testing.T) {
			target, err := (TargetDraft{ID: "target", Model: "model", Protocol: test.protocol, Connection: test.connection}).routingTarget()
			if err != nil {
				t.Fatal(err)
			}
			if target.Provider() != test.provider {
				t.Fatalf("provider = %q, want %q", target.Provider(), test.provider)
			}
			if test.provider == routing.Provider("zai") && target.Protocol().String() != routing.ZAIProviderProtocol {
				t.Fatalf("derived Z.AI protocol = %q", target.Protocol().String())
			}
			if want, derived := profile.DerivedProtocolForSpec(string(test.provider)); derived && target.Protocol().String() != want {
				t.Fatalf("derived protocol = %q, want %q", target.Protocol().String(), want)
			}
		})
	}
}

func TestOperatorTargetDraftRejectsEmptyConnection(t *testing.T) {
	_, err := finalizeTargetDraft("ambiguous", "model", "responses", Connection{})
	if err == nil || !strings.Contains(err.Error(), "provider is unsupported") {
		t.Fatalf("finalize error = %v, want empty connection rejection", err)
	}
}

func TestProjectTargetPreservesEffectiveDerivedProtocol(t *testing.T) {
	for _, test := range []struct {
		name       string
		connection routing.Connection
		protocol   string
	}{
		{name: "Z.AI", connection: mustZAIConnection(t), protocol: routing.ZAIProviderProtocol},
		{name: "DeepSeek", connection: mustDeepSeekConnection(t), protocol: routing.DeepSeekProviderProtocol},
		{name: "Kimi", connection: mustKimiConnection(t), protocol: routing.KimiProviderProtocol},
	} {
		t.Run(test.name, func(t *testing.T) {
			id, err := routing.ParseTargetID("target")
			if err != nil {
				t.Fatal(err)
			}
			model, err := routing.ParseUpstreamModel("model")
			if err != nil {
				t.Fatal(err)
			}
			protocol, err := routing.ParseProtocol(test.protocol, test.connection.Provider(), func(routing.Provider, string) bool { return true })
			if err != nil {
				t.Fatal(err)
			}
			target, err := routing.NewTarget(id, model, protocol, test.connection)
			if err != nil {
				t.Fatal(err)
			}
			if got := projectTarget(target).Protocol; got != test.protocol {
				t.Fatalf("projected protocol = %q, want %q", got, test.protocol)
			}
		})
	}
}

func mustZAIConnection(t *testing.T) routing.Connection {
	t.Helper()
	provider, _ := routing.ParseProvider("zai", profile.SupportsSpec)
	connection, err := routing.NewZAIConnection(provider, routing.ZAIAccessCodingPlan, "env:ZAI_API_KEY")
	if err != nil {
		t.Fatal(err)
	}
	return connection
}

func mustDeepSeekConnection(t *testing.T) routing.Connection {
	t.Helper()
	provider, _ := routing.ParseProvider("deepseek", profile.SupportsSpec)
	connection, err := routing.NewStandardConnection(provider, "", "env:DEEPSEEK_API_KEY")
	if err != nil {
		t.Fatal(err)
	}
	return connection
}

func mustKimiConnection(t *testing.T) routing.Connection {
	t.Helper()
	provider, _ := routing.ParseProvider("kimi", profile.SupportsSpec)
	connection, err := routing.NewStandardConnection(provider, "", "env:MOONSHOT_API_KEY")
	if err != nil {
		t.Fatal(err)
	}
	return connection
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
	connection, err := target.Connection.RoutingConnection()
	standard, ok := connection.(routing.StandardConnection)
	if err != nil || !ok || target.Model != "gpt-5" || target.Protocol != "responses" || target.Provider != "openai" || standard.Credential().String() != "env:ROTATED" {
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
	persistedConnection, ok := actual.Connection().(routing.StandardConnection)
	if actual.Model().String() != "gpt-5" || actual.Protocol().String() != "responses" || actual.Provider() != routing.Provider("openai") || !ok || persistedConnection.Credential().String() != "env:ROTATED" {
		t.Fatalf("persisted target = %#v", actual)
	}
}

func TestServiceSetCredentialRevalidatesProfileConnectionContract(t *testing.T) {
	for _, test := range []struct {
		name   string
		target TargetDraft
		value  string
		want   string
	}{
		{
			name:   "required OpenAI credential cannot be cleared",
			target: openAITarget("openai"),
			value:  "",
			want:   "connection.openai.credential: is required",
		},
		{
			name: "unsupported Ollama credential cannot be added",
			target: TargetDraft{
				ID:         "ollama",
				Model:      "qwen",
				Protocol:   "chat_completions",
				Connection: StandardConnection("ollama", "", ""),
			},
			value: "env:OLLAMA_TOKEN",
			want:  "connection.ollama.credential: is not authorable",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, _, _ := testService(t)
			ctx := context.Background()
			if _, err := service.CreateWorkspace(ctx, CreateWorkspace{Slug: "dev", InitialRoute: "chat", Target: test.target}); err != nil {
				t.Fatalf("create workspace: %v", err)
			}

			_, err := service.SetCredential(ctx, SetCredential{Workspace: "dev", Route: "chat", TargetID: test.target.ID, Credential: test.value})
			var command CommandError
			if !errors.As(err, &command) || command.Code != InvalidArgument || !strings.Contains(command.Message, test.want) {
				t.Fatalf("SetCredential error = %#v, want INVALID_ARGUMENT containing %q", err, test.want)
			}
		})
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

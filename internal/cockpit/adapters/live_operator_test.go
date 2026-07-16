package adapters

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	operatorclient "github.com/swobuforge/swobu/internal/app/operator/client"
	clientprofile "github.com/swobuforge/swobu/internal/app/operator/clientprofile"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/platform/config"
)

type fakeOperatorClient struct {
	endpoints []operatorclient.EndpointData
	status    operatorclient.StatusProjection
	statusErr error
	getErr    error
	deleted   string
	upserted  operatorclient.EndpointData
	modelCatalogResult  operatorclient.ModelCatalogResult
	modelCatalogErr     error
}

func (f *fakeOperatorClient) ListEndpoints(context.Context) ([]operatorclient.EndpointData, error) {
	return f.endpoints, nil
}

func (f *fakeOperatorClient) GetEndpoint(_ context.Context, name string) (operatorclient.EndpointData, error) {
	if f.getErr != nil {
		return operatorclient.EndpointData{}, f.getErr
	}
	for _, endpoint := range f.endpoints {
		if endpoint.Name == name {
			return endpoint, nil
		}
	}
	return operatorclient.EndpointData{}, &operatorclient.ResponseError{
		StatusCode: http.StatusNotFound,
		Code:       "NOT_FOUND",
		Message:    "endpoint not found",
	}
}

func (f *fakeOperatorClient) UpsertEndpoint(_ context.Context, endpoint operatorclient.EndpointData) error {
	f.upserted = endpoint
	for i := range f.endpoints {
		if f.endpoints[i].Name == endpoint.Name {
			f.endpoints[i] = endpoint
			return nil
		}
	}
	f.endpoints = append(f.endpoints, endpoint)
	return nil
}

func (f *fakeOperatorClient) DeleteEndpoint(_ context.Context, name string) error {
	f.deleted = name
	return nil
}

func (f *fakeOperatorClient) Status(context.Context, string) (operatorclient.StatusProjection, error) {
	if f.statusErr != nil {
		return operatorclient.StatusProjection{}, f.statusErr
	}
	return f.status, nil
}

func (f *fakeOperatorClient) StartAuthSession(context.Context, string, string, string) (operatorclient.AuthSessionStartResult, error) {
	return operatorclient.AuthSessionStartResult{}, nil
}
func (f *fakeOperatorClient) GetAuthSessionStatus(context.Context, string) (operatorclient.AuthSessionStatusResult, error) {
	return operatorclient.AuthSessionStatusResult{}, nil
}
func (f *fakeOperatorClient) CancelAuthSession(context.Context, string) error { return nil }
func (f *fakeOperatorClient) RetryAuthSession(context.Context, string) (operatorclient.AuthSessionRetryResult, error) {
	return operatorclient.AuthSessionRetryResult{}, nil
}
func (f *fakeOperatorClient) ProbeModelCatalog(context.Context, string, string, string, string, string) (operatorclient.ModelCatalogResult, error) {
	return f.modelCatalogResult, f.modelCatalogErr
}

func TestLiveOperatorAdapter_LoadCockpitProjectsEndpoints(t *testing.T) {
	t.Parallel()

	client := &fakeOperatorClient{
		endpoints: []operatorclient.EndpointData{
			{
				Name:        "lab",
				SelectedRef: "cfg-lab",
				ProviderConfigs: []operatorclient.ProviderConfigData{
					{Ref: "cfg-lab", ProviderSpec: "openai", RouteModelID: "gpt", ModelID: "gpt-4.1", ProviderProtocol: "responses", BaseURL: "https://api.openai.com/v1", CredentialRef: "env:OPENAI_API_KEY"},
				},
			},
			{
				Name:        "dev",
				SelectedRef: "cfg-fast",
				ProviderConfigs: []operatorclient.ProviderConfigData{
					{Ref: "cfg-fast", ProviderSpec: "openai_compatible", RouteModelID: "gpt", ModelID: "gpt-4.1", ProviderProtocol: "responses", TargetAlias: "fast", BaseURL: "https://fast.example/v1", CredentialRef: "key-fast"},
					{Ref: "cfg-deep", ProviderSpec: "openai_compatible", RouteModelID: "gpt", ModelID: "gpt-4o", ProviderProtocol: "responses", BaseURL: "https://deep.example/v1", CredentialRef: "key-deep"},
					{Ref: "cfg-local", ProviderSpec: "openai_compatible", RouteModelID: "local", ModelID: "llama3.2", ProviderProtocol: "responses", BaseURL: "http://127.0.0.1:11434/v1"},
				},
			},
		},
		status: operatorclient.StatusProjection{
			RecentTraffic: []operatorclient.RecentTrafficRow{{
				RequestID:     "req-1",
				Endpoint:      "dev",
				ClientFamily:  "codex",
				Route:         "cfg-fast:gpt-4.1",
				Result:        "success",
				StatusCode:    200,
				ObservedAt:    "14:32:01",
				ModelResolved: "gpt-4.1",
			}},
		},
	}
	adapter := newLiveOperatorAdapterWithClient(client, "http://127.0.0.1:7926")

	model, err := adapter.LoadCockpit(context.Background())
	if err != nil {
		t.Fatalf("LoadCockpit returned error: %v", err)
	}
	if got, want := model.SelectedWorkspaceID, readmodel.WorkspaceID("dev"); got != want {
		t.Fatalf("selected workspace = %q, want %q", got, want)
	}
	if got := len(model.Tabs); got != 4 {
		t.Fatalf("tab count = %d, want 4", got)
	}
	if !model.Tabs[0].Selected || model.Tabs[0].Slug != "dev" {
		t.Fatalf("first tab = %#v, want selected dev", model.Tabs[0])
	}
	workspace := model.SelectedWorkspace
	if got, want := workspace.ClientBaseURL, "http://127.0.0.1:7926/c/dev"; got != want {
		t.Fatalf("client base URL = %q, want %q", got, want)
	}
	if got := len(workspace.RunCommands); got == 0 {
		t.Fatal("run commands were not projected from client profiles")
	}
	if got := len(workspace.Routes); got != 2 {
		t.Fatalf("route count = %d, want 2", got)
	}
	gpt := workspace.Routes[0]
	if gpt.ModelName != "gpt" || !gpt.Default {
		t.Fatalf("gpt route = %#v", gpt)
	}
	if got, want := gpt.Targets[0].ProviderProtocol, "responses"; got != want {
		t.Fatalf("first gpt target protocol = %q, want %q", got, want)
	}
	if got := len(gpt.Targets); got != 2 {
		t.Fatalf("gpt targets = %d, want 2", got)
	}
	if gpt.Targets[0].Name != "fast" || gpt.Targets[0].Rank != 1 {
		t.Fatalf("first gpt target = %#v", gpt.Targets[0])
	}
	if got, want := gpt.Targets[0].Model, "gpt-4.1"; got != want {
		t.Fatalf("first gpt target model = %q, want %q", got, want)
	}
	if got, want := gpt.Targets[1].Model, "gpt-4o"; got != want {
		t.Fatalf("second gpt target model = %q, want %q", got, want)
	}
	if latest, ok := workspace.Activity.LatestRow(); !ok || latest.ID != "req-1" {
		t.Fatalf("latest activity = %#v, %v", latest, ok)
	}
}

func TestRouteProjection_GroupsByRouteModelIDAndKeepsProviderModels(t *testing.T) {
	t.Parallel()

	routes := routesFromEndpoint(operatorclient.EndpointData{
		Name: "dev",
		ProviderConfigs: []operatorclient.ProviderConfigData{
			{
				Ref:          "a",
				ProviderSpec: "openai",
				RouteModelID: "gpt",
				ModelID:      "gpt-4.1",
				TargetRank:   1,
				TargetWeight: 1,
			},
			{
				Ref:          "b",
				ProviderSpec: "anthropic",
				RouteModelID: "gpt",
				ModelID:      "claude-sonnet",
				TargetRank:   2,
				TargetWeight: 1,
			},
		},
	})
	if len(routes) != 1 {
		t.Fatalf("routes = %d, want 1", len(routes))
	}
	route := routes[0]
	if got, want := route.ModelName, "gpt"; got != want {
		t.Fatalf("route model = %q, want %q", got, want)
	}
	if got, want := route.ID, readmodel.RouteID("gpt"); got != want {
		t.Fatalf("route id = %q, want %q", got, want)
	}
	if got, want := route.Targets[0].Provider, "openai"; got != want {
		t.Fatalf("first target provider = %q, want %q", got, want)
	}
	if got, want := route.Targets[0].Model, "gpt-4.1"; got != want {
		t.Fatalf("first target model = %q, want %q", got, want)
	}
	if got, want := route.Targets[1].Provider, "anthropic"; got != want {
		t.Fatalf("second target provider = %q, want %q", got, want)
	}
	if got, want := route.Targets[1].Model, "claude-sonnet"; got != want {
		t.Fatalf("second target model = %q, want %q", got, want)
	}
}

func TestLiveOperatorAdapter_ResolveProviderSetupProjectsOpenAI(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")

	adapter := newLiveOperatorAdapterWithClient(&fakeOperatorClient{}, "http://127.0.0.1:7926")
	setup, err := adapter.ResolveProviderSetup(context.Background(), ports.ResolveProviderSetupRequest{
		ProviderSpec: "openai",
	})
	if err != nil {
		t.Fatalf("ResolveProviderSetup returned error: %v", err)
	}
	if setup.DisplayName != "OpenAI" {
		t.Fatalf("display name = %q, want OpenAI", setup.DisplayName)
	}
	if setup.DefaultBaseURL != "https://api.openai.com/v1" {
		t.Fatalf("default base URL = %q, want OpenAI base URL", setup.DefaultBaseURL)
	}
	if setup.CredentialLabel != "env:OPENAI_API_KEY" {
		t.Fatalf("credential label = %q, want env:OPENAI_API_KEY", setup.CredentialLabel)
	}
	if setup.CredentialRef != "env:OPENAI_API_KEY" {
		t.Fatalf("credential ref = %q, want env:OPENAI_API_KEY", setup.CredentialRef)
	}
	if !setup.ReadyForCatalog {
		t.Fatal("openai with credential should be ready for catalog")
	}
	if setup.BlockReason != "" {
		t.Fatalf("block reason = %q, want empty", setup.BlockReason)
	}
}

func TestLiveOperatorAdapter_ResolveProviderSetupProjectsOllama(t *testing.T) {
	adapter := newLiveOperatorAdapterWithClient(&fakeOperatorClient{}, "http://127.0.0.1:7926")
	setup, err := adapter.ResolveProviderSetup(context.Background(), ports.ResolveProviderSetupRequest{
		ProviderSpec: "ollama",
	})
	if err != nil {
		t.Fatalf("ResolveProviderSetup returned error: %v", err)
	}
	if setup.DisplayName != "Ollama" {
		t.Fatalf("display name = %q, want Ollama", setup.DisplayName)
	}
	if setup.CredentialRequired {
		t.Fatal("ollama should not require a credential")
	}
	if !setup.ReadyForCatalog {
		t.Fatal("ollama should be ready for catalog without a credential")
	}
	if setup.BlockReason != "" {
		t.Fatalf("block reason = %q, want empty", setup.BlockReason)
	}
}

func TestLiveOperatorAdapter_ResolveProviderSetupRequiresBaseURLForOpenAICompatible(t *testing.T) {
	adapter := newLiveOperatorAdapterWithClient(&fakeOperatorClient{}, "http://127.0.0.1:7926")
	setup, err := adapter.ResolveProviderSetup(context.Background(), ports.ResolveProviderSetupRequest{
		ProviderSpec: "openai_compatible",
	})
	if err != nil {
		t.Fatalf("ResolveProviderSetup returned error: %v", err)
	}
	if setup.DisplayName != "OpenAI Compatible" {
		t.Fatalf("display name = %q, want OpenAI Compatible", setup.DisplayName)
	}
	if !setup.RequiresBaseURL {
		t.Fatal("openai-compatible should require an explicit base URL")
	}
	if setup.CredentialLabel != "enter base URL" {
		t.Fatalf("credential label = %q, want enter base URL", setup.CredentialLabel)
	}
	if setup.BlockReason != "enter base URL" {
		t.Fatalf("block reason = %q, want enter base URL", setup.BlockReason)
	}
	if setup.ReadyForCatalog {
		t.Fatal("openai-compatible without a base URL should not be ready for catalog")
	}
	if setup.DefaultAuthHeader != "Authorization" {
		t.Fatalf("default auth header = %q, want Authorization", setup.DefaultAuthHeader)
	}
}

func TestLiveOperatorAdapter_ResolveProviderSetupReportsChatGPTInteractiveAuth(t *testing.T) {
	adapter := newLiveOperatorAdapterWithClient(&fakeOperatorClient{}, "http://127.0.0.1:7926")
	setup, err := adapter.ResolveProviderSetup(context.Background(), ports.ResolveProviderSetupRequest{
		ProviderSpec: "chatgpt",
	})
	if err != nil {
		t.Fatalf("ResolveProviderSetup returned error: %v", err)
	}
	if setup.DisplayName != "ChatGPT" {
		t.Fatalf("display name = %q, want ChatGPT", setup.DisplayName)
	}
	if !setup.InteractiveAuth {
		t.Fatal("chatgpt should require interactive auth")
	}
	if setup.CredentialLabel != "browser login" {
		t.Fatalf("credential label = %q, want browser login", setup.CredentialLabel)
	}
	if setup.BlockReason != "auth first" {
		t.Fatalf("block reason = %q, want auth first", setup.BlockReason)
	}
	if setup.ReadyForCatalog {
		t.Fatal("chatgpt should not be ready for catalog before auth starts")
	}
	if len(setup.AuthModes) == 0 || !setup.AuthModes[0].Interactive {
		t.Fatalf("auth modes = %#v, want interactive auth modes", setup.AuthModes)
	}
}

func TestLiveOperatorAdapter_LoadCockpitWithDefaultDaemonHidesHeaderRight(t *testing.T) {
	t.Parallel()

	adapter := newLiveOperatorAdapterWithClient(&fakeOperatorClient{
		endpoints: []operatorclient.EndpointData{{
			Name:        "dev",
			SelectedRef: "cfg-fast",
			ProviderConfigs: []operatorclient.ProviderConfigData{{
				Ref:          "cfg-fast",
				ProviderSpec: "openai_compatible",
				ModelID:      "gpt-4.1",
				BaseURL:      "https://fast.example/v1",
			}},
		}},
	}, "")

	model, err := adapter.LoadCockpit(context.Background())
	if err != nil {
		t.Fatalf("LoadCockpit returned error: %v", err)
	}
	if got := model.HeaderRight; got != "" {
		t.Fatalf("header right = %q, want empty for default daemon", got)
	}
}

func TestLiveOperatorAdapter_LoadCockpitWithCustomDaemonShowsHeaderRight(t *testing.T) {
	t.Parallel()

	adapter := newLiveOperatorAdapterWithClient(&fakeOperatorClient{
		endpoints: []operatorclient.EndpointData{{
			Name:        "dev",
			SelectedRef: "cfg-fast",
			ProviderConfigs: []operatorclient.ProviderConfigData{{
				Ref:          "cfg-fast",
				ProviderSpec: "openai_compatible",
				ModelID:      "gpt-4.1",
				BaseURL:      "https://fast.example/v1",
			}},
		}},
	}, "http://pi:7926")

	model, err := adapter.LoadCockpit(context.Background())
	if err != nil {
		t.Fatalf("LoadCockpit returned error: %v", err)
	}
	if got, want := model.HeaderRight, "http://pi:7926"; got != want {
		t.Fatalf("header right = %q, want %q", got, want)
	}
}

func TestLiveOperatorAdapter_LoadCockpitWithEnvCustomDaemonShowsHeaderRight(t *testing.T) {
	t.Setenv("SWOBU_DAEMON_URL", "http://pi:7926")

	adapter := newLiveOperatorAdapterWithClient(&fakeOperatorClient{
		endpoints: []operatorclient.EndpointData{{
			Name:        "dev",
			SelectedRef: "cfg-fast",
			ProviderConfigs: []operatorclient.ProviderConfigData{{
				Ref:          "cfg-fast",
				ProviderSpec: "openai_compatible",
				ModelID:      "gpt-4.1",
				BaseURL:      "https://fast.example/v1",
			}},
		}},
	}, "")

	model, err := adapter.LoadCockpit(context.Background())
	if err != nil {
		t.Fatalf("LoadCockpit returned error: %v", err)
	}
	if got, want := model.HeaderRight, "http://pi:7926"; got != want {
		t.Fatalf("header right = %q, want %q", got, want)
	}
}

func TestLiveOperatorAdapter_EmptyEndpointListProjectsDraftWorkspace(t *testing.T) {
	t.Parallel()

	adapter := newLiveOperatorAdapterWithClient(&fakeOperatorClient{}, "http://127.0.0.1:7926")

	model, err := adapter.LoadCockpit(context.Background())
	if err != nil {
		t.Fatalf("LoadCockpit returned error: %v", err)
	}
	if model.SelectedWorkspace.State != readmodel.WorkspaceDraft {
		t.Fatalf("selected workspace state = %v, want draft", model.SelectedWorkspace.State)
	}
	if len(model.Tabs) != 2 || model.Tabs[0].ID != "+" || !model.Tabs[0].Selected {
		t.Fatalf("tabs = %#v", model.Tabs)
	}
}

func TestLiveOperatorAdapter_LoadCockpitDegradesOnActivityFailure(t *testing.T) {
	t.Parallel()

	statusErr := errors.New("status unavailable")
	adapter := newLiveOperatorAdapterWithClient(&fakeOperatorClient{
		endpoints: []operatorclient.EndpointData{{
			Name:        "dev",
			SelectedRef: "cfg-fast",
			ProviderConfigs: []operatorclient.ProviderConfigData{{
				Ref:          "cfg-fast",
				ProviderSpec: "openai_compatible",
				ModelID:      "gpt-4.1",
				BaseURL:      "https://fast.example/v1",
			}},
		}},
		statusErr: statusErr,
	}, "http://127.0.0.1:7926")

	model, err := adapter.LoadCockpit(context.Background())
	if err != nil {
		t.Fatalf("LoadCockpit returned error: %v", err)
	}
	if model.SelectedWorkspaceID != "dev" {
		t.Fatalf("selected workspace = %q, want dev", model.SelectedWorkspaceID)
	}
	if !model.SelectedWorkspace.Activity.IsEmpty() {
		t.Fatal("activity should be empty when status fails")
	}
}

func TestLiveOperatorAdapter_DiagnosticsPayloadIsRedacted(t *testing.T) {
	t.Parallel()

	client := &fakeOperatorClient{
		endpoints: []operatorclient.EndpointData{{
			Name:        "dev",
			SelectedRef: "cfg-fast",
			ProviderConfigs: []operatorclient.ProviderConfigData{{
				Ref:              "cfg-fast",
				ProviderSpec:     "openai_compatible",
				ModelID:          "gpt-4.1",
				TargetAlias:      "fast",
				BaseURL:          "https://secret-backend.example/v1",
				AuthHeader:       "Bearer sk-live-secret",
				CredentialRef:    "env:OPENAI_API_KEY",
				ProviderProtocol: "responses",
			}},
		}},
		status: operatorclient.StatusProjection{
			RecentTraffic: []operatorclient.RecentTrafficRow{{
				RequestID:           "req-1",
				Endpoint:            "dev",
				ClientFamily:        "codex",
				Route:               "gpt-4.1",
				Result:              "success",
				StatusCode:          200,
				ModelRequested:      "prompt body should not leak",
				ExchangeDiagnostics: []string{"Authorization: Bearer sk-live-secret"},
			}},
		},
	}
	adapter := newLiveOperatorAdapterWithClient(client, "http://127.0.0.1:7926")

	model, err := adapter.LoadCockpit(context.Background())
	if err != nil {
		t.Fatalf("LoadCockpit returned error: %v", err)
	}
	payload := model.Help.Diagnostics
	assertDiagnosticsDoesNotContain(t, payload, "sk-live-secret", "OPENAI_API_KEY", "secret-backend", "Authorization", "prompt body", "responses")
	text := payload.Text()
	for _, want := range []string{"dev", "gpt-4.1", "fast"} {
		if !strings.Contains(text, want) {
			t.Fatalf("diagnostics payload missing safe value %q:\n%s", want, text)
		}
	}

	result, err := adapter.CopyDiagnostics(context.Background())
	if err != nil {
		t.Fatalf("CopyDiagnostics returned error: %v", err)
	}
	if result.Status != ports.DiagnosticsCopyCopied {
		t.Fatalf("copy status = %v, want copied", result.Status)
	}
	for _, unsafe := range []string{"sk-live-secret", "OPENAI_API_KEY", "secret-backend", "Authorization"} {
		if strings.Contains(result.Text, unsafe) {
			t.Fatalf("diagnostics copy leaked %q:\n%s", unsafe, result.Text)
		}
	}
}

func assertDiagnosticsDoesNotContain(t *testing.T, payload readmodel.DiagnosticsPayload, values ...string) {
	t.Helper()
	text := payload.Text()
	for _, value := range values {
		if value == "" {
			continue
		}
		if strings.Contains(text, value) {
			t.Fatalf("diagnostics payload leaked unsafe value %q:\n%s", value, text)
		}
	}
}

func TestLiveOperatorAdapter_LoadCockpitHonorsCanceledContext(t *testing.T) {
	t.Parallel()

	client := &cancelAwareOperatorClient{}
	adapter := newLiveOperatorAdapterWithClient(client, "http://127.0.0.1:7926")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := adapter.LoadCockpit(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("LoadCockpit error = %v, want context.Canceled", err)
	}
	if !client.sawCanceled {
		t.Fatal("LoadCockpit did not pass the canceled context to the client")
	}
}

func TestLiveOperatorAdapter_ListActivityMapsStatusProjection(t *testing.T) {
	t.Parallel()

	in := 120
	out := 30
	dur := 145
	client := &fakeOperatorClient{
		status: operatorclient.StatusProjection{
			RecentTraffic: []operatorclient.RecentTrafficRow{{
				RequestID:     "req-1",
				ClientFamily:  "codex",
				Route:         "gpt",
				Result:        "success",
				StatusCode:    200,
				ObservedAt:    "14:32:01",
				ModelResolved: "gpt-4.1",
				Timing:        &operatorclient.RecentTrafficTimingRecord{DurMillis: &dur},
				TokenUsage:    &operatorclient.RecentTrafficTokenUseRecord{InputTokens: &in, OutputTokens: &out},
			}},
		},
	}
	adapter := newLiveOperatorAdapterWithClient(client, "http://127.0.0.1:7926")

	activity, err := adapter.ListActivity(context.Background(), ports.ListActivityRequest{WorkspaceID: "dev", Limit: 1})
	if err != nil {
		t.Fatalf("ListActivity returned error: %v", err)
	}
	row, ok := activity.LatestRow()
	if !ok {
		t.Fatal("latest row missing")
	}
	if row.ClientLabel != "codex" || row.HTTPStatus != 200 || row.TokensIn != 120 || row.TokensOut != 30 {
		t.Fatalf("activity row = %#v", row)
	}
	if got := row.RowValue(); got != "14:32:01 codex gpt 200 145ms" {
		t.Fatalf("row value = %q", got)
	}
}

func TestLiveOperatorAdapter_SaveWorkspaceCreatesDraftShellWithoutPersisting(t *testing.T) {
	t.Parallel()

	client := &fakeOperatorClient{}
	adapter := newLiveOperatorAdapterWithClient(client, "http://127.0.0.1:7926")

	workspace, err := adapter.SaveWorkspace(context.Background(), ports.SaveWorkspaceRequest{ID: "+", Slug: "new"})
	if err != nil {
		t.Fatalf("draft SaveWorkspace returned error: %v", err)
	}
	if workspace.ID != "new" || workspace.Slug != "new" || workspace.State != readmodel.WorkspaceExisting {
		t.Fatalf("draft SaveWorkspace workspace = %#v, want persisted shell projection", workspace)
	}
	if client.upserted.Name != "" || len(client.endpoints) != 0 {
		t.Fatalf("draft SaveWorkspace should not persist endpoint state, got upserted=%+v endpoints=%+v", client.upserted, client.endpoints)
	}
}

func TestLiveOperatorAdapter_SaveWorkspaceRejectsRename(t *testing.T) {
	t.Parallel()

	client := &fakeOperatorClient{
		endpoints: []operatorclient.EndpointData{{
			Name:        "dev",
			SelectedRef: "cfg-fast",
			ProviderConfigs: []operatorclient.ProviderConfigData{{
				Ref:          "cfg-fast",
				ProviderSpec: "openai_compatible",
				ModelID:      "gpt-4.1",
				BaseURL:      "https://fast.example/v1",
			}},
		}},
	}
	adapter := newLiveOperatorAdapterWithClient(client, "http://127.0.0.1:7926")

	if _, err := adapter.SaveWorkspace(context.Background(), ports.SaveWorkspaceRequest{ID: "dev", Slug: "prod"}); !errors.Is(err, ErrUnsupportedCommand) {
		t.Fatalf("SaveWorkspace rename error = %v, want ErrUnsupportedCommand", err)
	}
	if client.upserted.Name != "" {
		t.Fatalf("rename should not upsert, got %+v", client.upserted)
	}
	if client.deleted != "" {
		t.Fatalf("rename should not delete, got %q", client.deleted)
	}
}

func TestLiveOperatorAdapter_SaveRouteRenamesExistingModelAndCanSelectDefault(t *testing.T) {
	t.Parallel()

	client := &fakeOperatorClient{
		endpoints: []operatorclient.EndpointData{{
			Name:        "dev",
			SelectedRef: "cfg-local",
			ProviderConfigs: []operatorclient.ProviderConfigData{
				{Ref: "cfg-fast", ProviderSpec: "openai_compatible", RouteModelID: "gpt", ModelID: "gpt-4.1", BaseURL: "https://fast.example/v1"},
				{Ref: "cfg-deep", ProviderSpec: "openai_compatible", RouteModelID: "gpt", ModelID: "gpt-4o", BaseURL: "https://deep.example/v1"},
				{Ref: "cfg-local", ProviderSpec: "openai_compatible", RouteModelID: "local", ModelID: "llama3.2", BaseURL: "http://127.0.0.1:11434/v1"},
			},
		}},
	}
	adapter := newLiveOperatorAdapterWithClient(client, "http://127.0.0.1:7926")

	route, err := adapter.SaveRoute(context.Background(), ports.SaveRouteRequest{
		WorkspaceID: "dev",
		RouteID:     "gpt",
		ModelName:   "gpt-pro",
		Default:     true,
	})
	if err != nil {
		t.Fatalf("SaveRoute returned error: %v", err)
	}
	if got, want := route.ModelName, "gpt-pro"; got != want {
		t.Fatalf("route model = %q, want %q", got, want)
	}
	if got, want := client.upserted.ProviderConfigs[0].RouteModelID, "gpt-pro"; got != want {
		t.Fatalf("saved route model = %q, want %q", got, want)
	}
	if got, want := client.upserted.ProviderConfigs[0].ModelID, "gpt-4.1"; got != want {
		t.Fatalf("saved provider model = %q, want %q", got, want)
	}
	if got, want := client.upserted.ProviderConfigs[1].RouteModelID, "gpt-pro"; got != want {
		t.Fatalf("second saved route model = %q, want %q", got, want)
	}
	if got, want := client.upserted.ProviderConfigs[1].ModelID, "gpt-4o"; got != want {
		t.Fatalf("second saved provider model = %q, want %q", got, want)
	}
	if got, want := client.upserted.ProviderConfigs[2].RouteModelID, "local"; got != want {
		t.Fatalf("untouched route model = %q, want %q", got, want)
	}
	if got, want := client.upserted.SelectedRef, "cfg-fast"; got != want {
		t.Fatalf("selected ref = %q, want %q", got, want)
	}
}

func TestLiveOperatorAdapter_SaveRouteCannotCreateEmptyRoute(t *testing.T) {
	t.Parallel()

	client := &fakeOperatorClient{
		endpoints: []operatorclient.EndpointData{{
			Name:        "dev",
			SelectedRef: "cfg-local",
			ProviderConfigs: []operatorclient.ProviderConfigData{
				{Ref: "cfg-local", ProviderSpec: "openai_compatible", ModelID: "llama3.2", BaseURL: "http://127.0.0.1:11434/v1"},
			},
		}},
	}
	adapter := newLiveOperatorAdapterWithClient(client, "http://127.0.0.1:7926")

	if _, err := adapter.SaveRoute(context.Background(), ports.SaveRouteRequest{WorkspaceID: "dev", RouteID: "new", ModelName: "new"}); !errors.Is(err, ErrUnsupportedCommand) {
		t.Fatalf("SaveRoute error = %v, want ErrUnsupportedCommand", err)
	}
}

func TestLiveOperatorAdapter_DeleteRouteRemovesMatchingTargets(t *testing.T) {
	t.Parallel()

	client := &fakeOperatorClient{
		endpoints: []operatorclient.EndpointData{{
			Name:        "dev",
			SelectedRef: "cfg-fast",
			ProviderConfigs: []operatorclient.ProviderConfigData{
				{Ref: "cfg-fast", ProviderSpec: "openai_compatible", RouteModelID: "gpt", ModelID: "gpt-4.1", BaseURL: "https://fast.example/v1"},
				{Ref: "cfg-deep", ProviderSpec: "openai_compatible", RouteModelID: "gpt", ModelID: "gpt-4o", BaseURL: "https://deep.example/v1"},
				{Ref: "cfg-local", ProviderSpec: "openai_compatible", RouteModelID: "local", ModelID: "llama3.2", BaseURL: "http://127.0.0.1:11434/v1"},
			},
		}},
	}
	adapter := newLiveOperatorAdapterWithClient(client, "http://127.0.0.1:7926")

	if err := adapter.DeleteRoute(context.Background(), ports.DeleteRouteRequest{WorkspaceID: "dev", RouteID: "gpt"}); err != nil {
		t.Fatalf("DeleteRoute returned error: %v", err)
	}
	if got := len(client.upserted.ProviderConfigs); got != 1 {
		t.Fatalf("remaining configs = %d, want 1", got)
	}
	if got, want := client.upserted.ProviderConfigs[0].Ref, "cfg-local"; got != want {
		t.Fatalf("remaining ref = %q, want %q", got, want)
	}
	if got, want := client.upserted.SelectedRef, "cfg-local"; got != want {
		t.Fatalf("selected ref = %q, want %q", got, want)
	}
}

func TestLiveOperatorAdapter_SaveTargetEditsAndAddsProviderConfigs(t *testing.T) {
	t.Parallel()

	client := &fakeOperatorClient{
		endpoints: []operatorclient.EndpointData{{
			Name:        "dev",
			SelectedRef: "cfg-fast",
			ProviderConfigs: []operatorclient.ProviderConfigData{{
				Ref:          "cfg-fast",
				ProviderSpec: "openai_compatible",
				RouteModelID: "gpt",
				ModelID:      "gpt-4.1",
				BaseURL:      "https://fast.example/v1",
			}},
		}},
	}
	adapter := newLiveOperatorAdapterWithClient(client, "http://127.0.0.1:7926")

	edited, err := adapter.SaveTarget(context.Background(), ports.SaveTargetRequest{
		WorkspaceID:      "dev",
		RouteID:          "gpt",
		TargetID:         "cfg-fast",
		Name:             "fast",
		Provider:         "openai_compatible",
		Model:            "gpt-4o",
		ProviderProtocol: "responses",
		BaseURL:          "https://new-fast.example/v1",
		CredentialRef:    "env:FAST_KEY",
		Rank:             2,
		Weight:           4,
	})
	if err != nil {
		t.Fatalf("SaveTarget edit returned error: %v", err)
	}
	if edited.ID != "cfg-fast" || edited.BaseURL != "https://new-fast.example/v1" || edited.Name != "fast" || edited.Model != "gpt-4o" || edited.Rank != 2 || edited.Weight != 4 {
		t.Fatalf("edited target = %#v", edited)
	}
	if got, want := client.upserted.ProviderConfigs[0].RouteModelID, "gpt"; got != want {
		t.Fatalf("edited route model = %q, want %q", got, want)
	}
	if got, want := client.upserted.ProviderConfigs[0].ModelID, "gpt-4o"; got != want {
		t.Fatalf("edited provider model = %q, want %q", got, want)
	}

	added, err := adapter.SaveTarget(context.Background(), ports.SaveTargetRequest{
		WorkspaceID:      "dev",
		RouteID:          "gpt",
		Name:             "deep",
		Provider:         "openai_compatible",
		Model:            "gpt-4.1",
		ProviderProtocol: "responses",
		BaseURL:          "https://deep.example/v1",
		CredentialRef:    "env:DEEP_KEY",
		Rank:             1,
		Weight:           2,
	})
	if err != nil {
		t.Fatalf("SaveTarget add returned error: %v", err)
	}
	if added.ID == "" || added.Name != "deep" || added.Model != "gpt-4.1" || added.Rank != 1 || added.Weight != 2 {
		t.Fatalf("added target = %#v", added)
	}
	if got := len(client.upserted.ProviderConfigs); got != 2 {
		t.Fatalf("saved configs = %d, want 2", got)
	}
	for _, config := range client.upserted.ProviderConfigs {
		if config.TargetAlias == "deep" && (config.TargetRank != 1 || config.TargetWeight != 2) {
			t.Fatalf("deep rank/weight = %d/%d, want 1/2", config.TargetRank, config.TargetWeight)
		}
		if config.TargetAlias == "deep" && config.ProviderProtocol != "responses" {
			t.Fatalf("deep protocol = %q, want responses", config.ProviderProtocol)
		}
		if config.TargetAlias == "deep" && config.RouteModelID != "gpt" {
			t.Fatalf("deep route model = %q, want gpt", config.RouteModelID)
		}
	}
}

func TestLiveOperatorAdapter_SaveTargetCreatesEndpointForMissingWorkspace(t *testing.T) {
	t.Parallel()

	client := &fakeOperatorClient{}
	adapter := newLiveOperatorAdapterWithClient(client, "http://127.0.0.1:7926")

	added, err := adapter.SaveTarget(context.Background(), ports.SaveTargetRequest{
		WorkspaceID:   "dev",
		RouteID:       "gpt",
		Name:          "deep",
		Provider:      "openai_compatible",
		Model:         "gpt-4.1",
		BaseURL:       "https://deep.example/v1",
		CredentialRef: "env:DEEP_KEY",
		Rank:          1,
		Weight:        2,
	})
	if err != nil {
		t.Fatalf("SaveTarget create returned error: %v", err)
	}
	if added.ID == "" || added.Name != "deep" || added.Model != "gpt-4.1" || added.Rank != 1 || added.Weight != 2 {
		t.Fatalf("added target = %#v", added)
	}
	if got, want := client.upserted.Name, "dev"; got != want {
		t.Fatalf("upserted endpoint name = %q, want %q", got, want)
	}
	if got := len(client.upserted.ProviderConfigs); got != 1 {
		t.Fatalf("saved configs = %d, want 1", got)
	}
	if got, want := client.upserted.ProviderConfigs[0].RouteModelID, "gpt"; got != want {
		t.Fatalf("saved route model = %q, want %q", got, want)
	}
	if got, want := client.upserted.ProviderConfigs[0].ModelID, "gpt-4.1"; got != want {
		t.Fatalf("saved provider model = %q, want %q", got, want)
	}
}

func TestLiveOperatorAdapter_SaveTargetRejectsEmptyWorkspaceID(t *testing.T) {
	t.Parallel()

	client := &fakeOperatorClient{}
	adapter := newLiveOperatorAdapterWithClient(client, "http://127.0.0.1:7926")

	_, err := adapter.SaveTarget(context.Background(), ports.SaveTargetRequest{
		WorkspaceID:   " ",
		RouteID:       "gpt",
		Name:          "deep",
		Provider:      "openai_compatible",
		Model:         "gpt-4.1",
		BaseURL:       "https://deep.example/v1",
		CredentialRef: "env:DEEP_KEY",
		Rank:          1,
		Weight:        1,
	})
	if err == nil || !strings.Contains(err.Error(), "workspace is required") {
		t.Fatalf("SaveTarget error = %v, want workspace validation", err)
	}
	if client.upserted.Name != "" {
		t.Fatalf("upserted endpoint = %#v, want none", client.upserted)
	}
}

func TestLiveOperatorAdapter_SaveTargetOtherGetErrorFails(t *testing.T) {
	t.Parallel()

	client := &fakeOperatorClient{
		getErr: errors.New("backend exploded"),
	}
	adapter := newLiveOperatorAdapterWithClient(client, "http://127.0.0.1:7926")

	_, err := adapter.SaveTarget(context.Background(), ports.SaveTargetRequest{
		WorkspaceID:   "dev",
		RouteID:       "gpt",
		Name:          "deep",
		Provider:      "openai_compatible",
		Model:         "gpt-4.1",
		BaseURL:       "https://deep.example/v1",
		CredentialRef: "env:DEEP_KEY",
		Rank:          1,
		Weight:        1,
	})
	if err == nil || !strings.Contains(err.Error(), "backend exploded") {
		t.Fatalf("SaveTarget error = %v, want propagated get error", err)
	}
	if client.upserted.Name != "" {
		t.Fatalf("upserted endpoint = %#v, want none", client.upserted)
	}
}

func TestRouteProjection_ProviderConfigFromTargetRequestRejectsInvalidRankWeight(t *testing.T) {
	t.Parallel()

	_, err := providerConfigFromTargetRequest(ports.SaveTargetRequest{Rank: 0, Weight: 1}, "id")
	if err == nil {
		t.Fatal("providerConfigFromTargetRequest expected error for rank < 1, got nil")
	}
	_, err = providerConfigFromTargetRequest(ports.SaveTargetRequest{Rank: 1, Weight: 0}, "id")
	if err == nil {
		t.Fatal("providerConfigFromTargetRequest expected error for weight < 1, got nil")
	}
	_, err = providerConfigFromTargetRequest(ports.SaveTargetRequest{Rank: -1, Weight: 1}, "id")
	if err == nil {
		t.Fatal("providerConfigFromTargetRequest expected error for negative rank, got nil")
	}
}

func TestRouteProjection_ProviderConfigFromTargetRequestAcceptsValidRankWeight(t *testing.T) {
	t.Parallel()

	config, err := providerConfigFromTargetRequest(ports.SaveTargetRequest{Provider: "openai", BaseURL: "https://example.com/v1", ProviderProtocol: "responses", Rank: 2, Weight: 3, RouteID: "gpt", Model: "gpt-4", Name: "main"}, "id")
	if err != nil {
		t.Fatalf("providerConfigFromTargetRequest returned error: %v", err)
	}
	if config.TargetRank != 2 || config.TargetWeight != 3 {
		t.Fatalf("rank/weight = %d/%d, want 2/3", config.TargetRank, config.TargetWeight)
	}
	if got, want := config.RouteModelID, "gpt"; got != want {
		t.Fatalf("route model = %q, want %q", got, want)
	}
	if got, want := config.ModelID, "gpt-4"; got != want {
		t.Fatalf("provider model = %q, want %q", got, want)
	}
	if got, want := config.ProviderProtocol, "responses"; got != want {
		t.Fatalf("protocol = %q, want %q", got, want)
	}
}

func TestRouteProjection_ProviderConfigFromTargetRequestDefaultsEmptyProtocol(t *testing.T) {
	t.Parallel()

	config, err := providerConfigFromTargetRequest(ports.SaveTargetRequest{Provider: "openai", BaseURL: "https://example.com/v1", ProviderProtocol: "", Rank: 1, Weight: 1, RouteID: "gpt", Model: "gpt-4", Name: "main"}, "id")
	if err != nil {
		t.Fatalf("providerConfigFromTargetRequest returned error: %v", err)
	}
	if got, want := config.ProviderProtocol, "responses"; got != want {
		t.Fatalf("defaulted protocol = %q, want %q", got, want)
	}
	if got, want := config.RouteModelID, "gpt"; got != want {
		t.Fatalf("route model = %q, want %q", got, want)
	}
}

func TestRouteProjection_ProviderConfigFromTargetRequestRejectsInvalidProtocol(t *testing.T) {
	t.Parallel()

	_, err := providerConfigFromTargetRequest(ports.SaveTargetRequest{Provider: "openai", BaseURL: "https://example.com/v1", ProviderProtocol: "invalid", Rank: 1, Weight: 1, RouteID: "gpt", Model: "gpt-4", Name: "main"}, "id")
	if err == nil {
		t.Fatal("providerConfigFromTargetRequest expected error for invalid protocol, got nil")
	}
}

func TestRouteProjection_TargetMatchesRouteUsesTargetIDAndRouteModelID(t *testing.T) {
	t.Parallel()

	config := operatorclient.ProviderConfigData{
		Ref:          "cfg-fast",
		ProviderSpec: "openai_compatible",
		RouteModelID: "gpt",
		ModelID:      "gpt-4.1",
	}
	if !targetMatchesRoute(config, "cfg-fast", "gpt") {
		t.Fatal("targetMatchesRoute should match target ref and route model id")
	}
	if targetMatchesRoute(config, "cfg-fast", "local") {
		t.Fatal("targetMatchesRoute should reject a different route model id")
	}
	if targetMatchesRoute(config, "cfg-deep", "gpt") {
		t.Fatal("targetMatchesRoute should reject a different target ref")
	}
}

func TestRouteProjection_UsesPersistedTargetRankAndWeight(t *testing.T) {
	t.Parallel()

	routes := routesFromEndpoint(operatorclient.EndpointData{
		Name: "dev",
		ProviderConfigs: []operatorclient.ProviderConfigData{
			{Ref: "slow", ProviderSpec: "openai", RouteModelID: "gpt", ModelID: "gpt-4.1", TargetRank: 2, TargetWeight: 1},
			{Ref: "fast", ProviderSpec: "openai", RouteModelID: "gpt", ModelID: "gpt-4o", TargetRank: 1, TargetWeight: 3},
		},
	})
	if len(routes) != 1 {
		t.Fatalf("routes = %d, want 1", len(routes))
	}
	route := routes[0]
	if got, want := route.ModelName, "gpt"; got != want {
		t.Fatalf("route model = %q, want %q", got, want)
	}
	if got := route.RowValue(); got != "2 fallback steps" {
		t.Fatalf("row value = %q, want %q", got, "2 fallback steps")
	}
	if got := route.Targets[0].ID; got != readmodel.TargetID("fast") {
		t.Fatalf("first target = %q, want fast", got)
	}
	if route.Targets[0].Rank != 1 || route.Targets[0].Weight != 3 {
		t.Fatalf("first target rank/weight = %d/%d, want 1/3", route.Targets[0].Rank, route.Targets[0].Weight)
	}
}

func TestRouteProjection_FallsBackToModelIDWhenRouteModelMissing(t *testing.T) {
	t.Parallel()

	routes := routesFromEndpoint(operatorclient.EndpointData{
		Name: "dev",
		ProviderConfigs: []operatorclient.ProviderConfigData{
			{Ref: "slow", ProviderSpec: "openai", ModelID: "gpt-4.1", TargetRank: 2, TargetWeight: 1},
			{Ref: "fast", ProviderSpec: "openai", ModelID: "gpt-4.1", TargetRank: 1, TargetWeight: 3},
		},
	})
	if len(routes) != 1 {
		t.Fatalf("routes = %d, want 1", len(routes))
	}
	route := routes[0]
	if got, want := route.ModelName, "gpt-4.1"; got != want {
		t.Fatalf("route model = %q, want %q", got, want)
	}
}

func TestLiveOperatorAdapter_DeleteTargetRemovesConfigAndPreservesEndpointInvariant(t *testing.T) {
	t.Parallel()

	client := &fakeOperatorClient{
		endpoints: []operatorclient.EndpointData{{
			Name:        "dev",
			SelectedRef: "cfg-fast",
			ProviderConfigs: []operatorclient.ProviderConfigData{
				{Ref: "cfg-fast", ProviderSpec: "openai_compatible", RouteModelID: "gpt", ModelID: "gpt-4.1", BaseURL: "https://fast.example/v1"},
				{Ref: "cfg-deep", ProviderSpec: "openai_compatible", RouteModelID: "gpt", ModelID: "gpt-4o", BaseURL: "https://deep.example/v1"},
			},
		}},
	}
	adapter := newLiveOperatorAdapterWithClient(client, "http://127.0.0.1:7926")

	if err := adapter.DeleteTarget(context.Background(), ports.DeleteTargetRequest{WorkspaceID: "dev", RouteID: "gpt", TargetID: "cfg-fast"}); err != nil {
		t.Fatalf("DeleteTarget returned error: %v", err)
	}
	if got := len(client.upserted.ProviderConfigs); got != 1 {
		t.Fatalf("remaining configs = %d, want 1", got)
	}
	if got, want := client.upserted.SelectedRef, "cfg-deep"; got != want {
		t.Fatalf("selected ref = %q, want %q", got, want)
	}

	if err := adapter.DeleteTarget(context.Background(), ports.DeleteTargetRequest{WorkspaceID: "dev", RouteID: "gpt", TargetID: "cfg-deep"}); !errors.Is(err, ErrUnsupportedCommand) {
		t.Fatalf("deleting final target error = %v, want ErrUnsupportedCommand", err)
	}
}

func TestLiveOperatorAdapter_ExecuteRunCommandResolvesClientProfile(t *testing.T) {
	t.Parallel()

	client := &fakeOperatorClient{
		endpoints: []operatorclient.EndpointData{{
			Name:        "dev",
			SelectedRef: "cfg-fast",
			ProviderConfigs: []operatorclient.ProviderConfigData{{
				Ref:          "cfg-fast",
				ProviderSpec: "openai_compatible",
				ModelID:      "gpt-4.1",
				BaseURL:      "https://fast.example/v1",
			}},
		}},
	}
	adapter := newLiveOperatorAdapterWithClient(client, "http://127.0.0.1:7926")
	var got clientprofile.RunCommandSpec
	adapter.runCommand = func(_ context.Context, command clientprofile.RunCommandSpec) error {
		got = command
		return nil
	}

	_, err := adapter.ExecuteRunCommand(context.Background(), ports.ExecuteRunCommandRequest{
		WorkspaceID:  "dev",
		RunCommandID: "codex",
		RouteID:      "gpt-4.1",
	})
	if err != nil {
		t.Fatalf("ExecuteRunCommand returned error: %v", err)
	}
	if got.ClientID != "codex" || got.Binary != "codex" {
		t.Fatalf("resolved command = %+v", got)
	}
	if got.Env["OPENAI_API_KEY"] == "" {
		t.Fatalf("resolved env = %+v", got.Env)
	}
	if joined := strings.Join(got.Args, " "); !strings.Contains(joined, `model_provider="swobu"`) {
		t.Fatalf("resolved args = %q", joined)
	}
}

func TestLiveOperatorAdapter_ExecuteRunCommandRejectsUnknownCommand(t *testing.T) {
	t.Parallel()

	client := &fakeOperatorClient{
		endpoints: []operatorclient.EndpointData{{
			Name:        "dev",
			SelectedRef: "cfg-fast",
			ProviderConfigs: []operatorclient.ProviderConfigData{{
				Ref:          "cfg-fast",
				ProviderSpec: "openai_compatible",
				ModelID:      "gpt-4.1",
				BaseURL:      "https://fast.example/v1",
			}},
		}},
	}
	adapter := newLiveOperatorAdapterWithClient(client, "http://127.0.0.1:7926")
	adapter.runCommand = func(context.Context, clientprofile.RunCommandSpec) error {
		t.Fatal("executor should not run for unsupported command")
		return nil
	}

	if _, err := adapter.ExecuteRunCommand(context.Background(), ports.ExecuteRunCommandRequest{WorkspaceID: "dev", RunCommandID: "unknown"}); !errors.Is(err, ErrUnsupportedCommand) {
		t.Fatalf("ExecuteRunCommand error = %v, want ErrUnsupportedCommand", err)
	} else if !strings.Contains(err.Error(), `run command "unknown"`) {
		t.Fatalf("ExecuteRunCommand error = %q, want operation context", err.Error())
	}
}

func TestExecuteClientRunCommandUsesInjectedIO(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := executeClientRunCommand(context.Background(), clientprofile.RunCommandSpec{
		Binary: "cat",
	}, runCommandIOConfig{
		Stdin:  strings.NewReader("hello"),
		Stdout: &stdout,
		Stderr: &stderr,
	}); err != nil {
		t.Fatalf("executeClientRunCommand returned error: %v", err)
	}
	if got, want := stdout.String(), "hello"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr should be empty, got %q", stderr.String())
	}
}

func TestRunCommandEnvironmentOverridesDuplicateKeys(t *testing.T) {
	const key = "SWOBU_TEST_ENV_OVERRIDE"
	t.Setenv(key, "base")

	env := runCommandEnvironment(map[string]string{
		key:                "override",
		"SWOBU_SECOND_KEY": "value",
	})
	if countEnvValue(env, key, "override") != 1 {
		t.Fatalf("env = %#v, want one override entry", env)
	}
	if countEnvValue(env, key, "base") != 0 {
		t.Fatalf("env = %#v, want no base entry", env)
	}
	if countEnvValue(env, "SWOBU_SECOND_KEY", "value") != 1 {
		t.Fatalf("env = %#v, want secondary override", env)
	}
}

func TestAdapterFailurePreservesCauseAndAddsOperationContext(t *testing.T) {
	t.Parallel()

	err := adapterFailure("save workspace \"dev\"", ErrUnsupportedCommand)
	if !errors.Is(err, ErrUnsupportedCommand) {
		t.Fatalf("wrapped error = %v, want ErrUnsupportedCommand cause", err)
	}
	if !strings.Contains(err.Error(), `save workspace "dev"`) {
		t.Fatalf("wrapped error = %q, want operation context", err.Error())
	}
}

func TestPrepareRunCommandFileWritesAndPreservesExistingWhenRequested(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, ".client", "config.json")
	if err := prepareRunCommandFile(clientprofile.RunPrepareFileSpec{
		Path:    path,
		Content: "first",
		Mode:    0o600,
	}); err != nil {
		t.Fatalf("prepare write returned error: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat prepared file: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode = %v, want 0600", got)
	}
	if err := prepareRunCommandFile(clientprofile.RunPrepareFileSpec{
		Path:           path,
		Content:        "second",
		WriteIfMissing: true,
	}); err != nil {
		t.Fatalf("prepare write-if-missing returned error: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read prepared file: %v", err)
	}
	if string(raw) != "first" {
		t.Fatalf("prepared file content = %q, want first", string(raw))
	}
}

type cancelAwareOperatorClient struct {
	sawCanceled bool
}

func (c *cancelAwareOperatorClient) ListEndpoints(ctx context.Context) ([]operatorclient.EndpointData, error) {
	c.sawCanceled = ctx.Err() != nil
	return nil, ctx.Err()
}

func (c *cancelAwareOperatorClient) GetEndpoint(context.Context, string) (operatorclient.EndpointData, error) {
	return operatorclient.EndpointData{}, errors.New("unexpected call")
}

func (c *cancelAwareOperatorClient) UpsertEndpoint(context.Context, operatorclient.EndpointData) error {
	return errors.New("unexpected call")
}

func (c *cancelAwareOperatorClient) DeleteEndpoint(context.Context, string) error {
	return errors.New("unexpected call")
}

func (c *cancelAwareOperatorClient) Status(context.Context, string) (operatorclient.StatusProjection, error) {
	return operatorclient.StatusProjection{}, errors.New("unexpected call")
}
func (c *cancelAwareOperatorClient) StartAuthSession(context.Context, string, string, string) (operatorclient.AuthSessionStartResult, error) {
	return operatorclient.AuthSessionStartResult{}, errors.New("unexpected call")
}
func (c *cancelAwareOperatorClient) GetAuthSessionStatus(context.Context, string) (operatorclient.AuthSessionStatusResult, error) {
	return operatorclient.AuthSessionStatusResult{}, errors.New("unexpected call")
}
func (c *cancelAwareOperatorClient) CancelAuthSession(context.Context, string) error {
	return errors.New("unexpected call")
}
func (c *cancelAwareOperatorClient) RetryAuthSession(context.Context, string) (operatorclient.AuthSessionRetryResult, error) {
	return operatorclient.AuthSessionRetryResult{}, errors.New("unexpected call")
}
func (c *cancelAwareOperatorClient) ProbeModelCatalog(context.Context, string, string, string, string, string) (operatorclient.ModelCatalogResult, error) {
	return operatorclient.ModelCatalogResult{}, errors.New("unexpected call")
}

func newLiveOperatorAdapterWithClient(client operatorClient, daemonURL string) *LiveOperatorAdapter {
	adapter := &LiveOperatorAdapter{
		client:    client,
		daemonURL: strings.TrimRight(config.ResolveDaemonURL(daemonURL), "/"),
		commandIO: processRunCommandIO(),
	}
	adapter.runCommand = func(ctx context.Context, command clientprofile.RunCommandSpec) error {
		return executeClientRunCommand(ctx, command, adapter.commandIO)
	}
	return adapter
}

func countEnvValue(env []string, key, value string) int {
	target := key + "=" + value
	count := 0
	for _, entry := range env {
		if entry == target {
			count++
		}
	}
	return count
}

func TestLiveOperatorAdapter_SaveTargetRejectsWrongRoute(t *testing.T) {
	t.Parallel()

	client := &fakeOperatorClient{
		endpoints: []operatorclient.EndpointData{{
			Name:        "dev",
			SelectedRef: "cfg-fast",
			ProviderConfigs: []operatorclient.ProviderConfigData{
				{Ref: "cfg-fast", ProviderSpec: "openai_compatible", RouteModelID: "gpt", ModelID: "gpt-4.1", BaseURL: "https://fast.example/v1"},
				{Ref: "cfg-local", ProviderSpec: "openai_compatible", RouteModelID: "local", ModelID: "llama3.2", BaseURL: "http://127.0.0.1:11434/v1"},
			},
		}},
	}
	adapter := newLiveOperatorAdapterWithClient(client, "http://127.0.0.1:7926")

	// cfg-fast exists but not in route "local".
	_, err := adapter.SaveTarget(context.Background(), ports.SaveTargetRequest{
		WorkspaceID:   "dev",
		RouteID:       "local",
		TargetID:      "cfg-fast",
		Name:          "fast",
		Provider:      "openai_compatible",
		Rank:          1,
		Weight:        1,
		BaseURL:       "https://new.example/v1",
		CredentialRef: "env:KEY",
	})
	if err == nil || !strings.Contains(err.Error(), "target not found in route") {
		t.Fatalf("SaveTarget error = %v, want route mismatch", err)
	}
	if client.upserted.Name != "" {
		t.Fatal("SaveTarget should not upsert on route mismatch")
	}
}

func TestLiveOperatorAdapter_DeleteTargetRejectsWrongRoute(t *testing.T) {
	t.Parallel()

	client := &fakeOperatorClient{
		endpoints: []operatorclient.EndpointData{{
			Name:        "dev",
			SelectedRef: "cfg-fast",
			ProviderConfigs: []operatorclient.ProviderConfigData{
				{Ref: "cfg-fast", ProviderSpec: "openai_compatible", RouteModelID: "gpt", ModelID: "gpt-4.1", BaseURL: "https://fast.example/v1"},
				{Ref: "cfg-local", ProviderSpec: "openai_compatible", RouteModelID: "local", ModelID: "llama3.2", BaseURL: "http://127.0.0.1:11434/v1"},
			},
		}},
	}
	adapter := newLiveOperatorAdapterWithClient(client, "http://127.0.0.1:7926")

	// cfg-fast exists but not in route "local".
	err := adapter.DeleteTarget(context.Background(), ports.DeleteTargetRequest{
		WorkspaceID: "dev",
		RouteID:     "local",
		TargetID:    "cfg-fast",
	})
	if err == nil {
		t.Fatal("DeleteTarget should reject a target delete from the wrong route")
	}
	if client.upserted.Name != "" {
		t.Fatal("DeleteTarget should not upsert on route mismatch")
	}
}

func TestLiveOperatorAdapter_ProbeProviderModelsMapsDeploymentsAndProtocol(t *testing.T) {
	t.Parallel()

	client := &fakeOperatorClient{
		modelCatalogResult: operatorclient.ModelCatalogResult{
			Deployments: []operatorclient.ModelCatalogDeployment{
				{
					Name:                    "gpt-4.1",
					ModelName:               "gpt-4.1",
					ModelPublisher:          "openai",
					ModelVersion:            "2024-01",
					Family:                  "gpt",
					DefaultProviderProtocol: "responses",
				},
				{
					Name:                    "gpt-4o",
					ModelName:               "gpt-4o",
					ModelPublisher:          "openai",
					DefaultProviderProtocol: "responses",
				},
			},
			ResolvedProviderProtocol: "responses",
		},
	}
	adapter := newLiveOperatorAdapterWithClient(client, "http://127.0.0.1:7926")

	result, err := adapter.ProbeProviderModels(context.Background(), ports.ProbeProviderModelsRequest{
		ProviderSpec:     "openai",
		BaseURL:          "https://api.openai.com/v1",
		AuthHeader:       "Authorization",
		CredentialRef:    "env:OPENAI_API_KEY",
		ProviderProtocol: "responses",
	})
	if err != nil {
		t.Fatalf("ProbeProviderModels returned error: %v", err)
	}
	if len(result.Deployments) != 2 {
		t.Fatalf("deployments = %d, want 2", len(result.Deployments))
	}

	d0 := result.Deployments[0]
	if d0.ID != "gpt-4.1" || d0.Name != "gpt-4.1" || d0.ModelName != "gpt-4.1" || d0.ModelPublisher != "openai" || d0.ModelVersion != "2024-01" || d0.Family != "gpt" || d0.DefaultProviderProtocol != "responses" {
		t.Fatalf("first deployment = %#v", d0)
	}

	d1 := result.Deployments[1]
	if d1.ID != "gpt-4o" || d1.Name != "gpt-4o" || d1.ModelName != "gpt-4o" {
		t.Fatalf("second deployment = %#v", d1)
	}

	if result.ResolvedProviderProtocol != "responses" {
		t.Fatalf("resolved protocol = %q, want responses", result.ResolvedProviderProtocol)
	}
}

func TestLiveOperatorAdapter_ProbeProviderMapsPreservesError(t *testing.T) {
	t.Parallel()

	client := &fakeOperatorClient{
		modelCatalogResult: operatorclient.ModelCatalogResult{
			Error: "401 unauthorized",
		},
	}
	adapter := newLiveOperatorAdapterWithClient(client, "http://127.0.0.1:7926")

	result, err := adapter.ProbeProviderModels(context.Background(), ports.ProbeProviderModelsRequest{
		ProviderSpec:  "openai",
		BaseURL:       "https://api.openai.com/v1",
		CredentialRef: "env:OPENAI_API_KEY",
	})
	if err != nil {
		t.Fatalf("ProbeProviderModels returned error: %v", err)
	}
	if result.Error != "401 unauthorized" {
		t.Fatalf("error = %q, want 401 unauthorized", result.Error)
	}
	if len(result.Deployments) != 0 {
		t.Fatalf("deployments = %d, want 0", len(result.Deployments))
	}
}

func TestLiveOperatorAdapter_DeleteWorkspaceDelegatesEndpointDelete(t *testing.T) {
	t.Parallel()

	client := &fakeOperatorClient{}
	adapter := newLiveOperatorAdapterWithClient(client, "http://127.0.0.1:7926")

	if err := adapter.DeleteWorkspace(context.Background(), ports.DeleteWorkspaceRequest{ID: "dev"}); err != nil {
		t.Fatalf("DeleteWorkspace returned error: %v", err)
	}
	if client.deleted != "dev" {
		t.Fatalf("deleted = %q, want dev", client.deleted)
	}
}

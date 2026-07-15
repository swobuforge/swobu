package adapters

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	operatorclient "github.com/swobuforge/swobu/internal/app/operator/client"
	"github.com/swobuforge/swobu/internal/app/operator/clientprofile"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
)

type fakeOperatorClient struct {
	endpoints []operatorclient.EndpointData
	status    operatorclient.StatusProjection
	statusErr error
	deleted   string
	upserted  operatorclient.EndpointData
}

func (f *fakeOperatorClient) ListEndpoints(context.Context) ([]operatorclient.EndpointData, error) {
	return f.endpoints, nil
}

func (f *fakeOperatorClient) GetEndpoint(_ context.Context, name string) (operatorclient.EndpointData, error) {
	for _, endpoint := range f.endpoints {
		if endpoint.Name == name {
			return endpoint, nil
		}
	}
	return operatorclient.EndpointData{}, errors.New("not found")
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

func TestLiveOperatorAdapter_LoadCockpitProjectsEndpoints(t *testing.T) {
	t.Parallel()

	client := &fakeOperatorClient{
		endpoints: []operatorclient.EndpointData{
			{
				Name:        "lab",
				SelectedRef: "cfg-lab",
				ProviderConfigs: []operatorclient.ProviderConfigData{
					{Ref: "cfg-lab", ProviderSpec: "openai", ModelID: "gpt-4.1", BaseURL: "https://api.openai.com/v1", CredentialRef: "env:OPENAI_API_KEY"},
				},
			},
			{
				Name:        "dev",
				SelectedRef: "cfg-fast",
				ProviderConfigs: []operatorclient.ProviderConfigData{
					{Ref: "cfg-fast", ProviderSpec: "openai_compatible", ModelID: "gpt-4.1", TargetAlias: "fast", BaseURL: "https://fast.example/v1", CredentialRef: "key-fast"},
					{Ref: "cfg-deep", ProviderSpec: "openai_compatible", ModelID: "gpt-4.1", BaseURL: "https://deep.example/v1", CredentialRef: "key-deep"},
					{Ref: "cfg-local", ProviderSpec: "openai_compatible", ModelID: "llama3.2", BaseURL: "http://127.0.0.1:11434/v1"},
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
	if gpt.ModelName != "gpt-4.1" || !gpt.Default || gpt.PlanKind != readmodel.RoutePlanRanked {
		t.Fatalf("gpt route = %#v", gpt)
	}
	if got := len(gpt.Targets); got != 2 {
		t.Fatalf("gpt targets = %d, want 2", got)
	}
	if gpt.Targets[0].Name != "fast" || gpt.Targets[0].Rank != 1 {
		t.Fatalf("first gpt target = %#v", gpt.Targets[0])
	}
	if latest, ok := workspace.Activity.LatestRow(); !ok || latest.ID != "req-1" {
		t.Fatalf("latest activity = %#v, %v", latest, ok)
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
				Timing:        &operatorclient.RecentTrafficTiming{DurMillis: &dur},
				TokenUsage:    &operatorclient.RecentTrafficTokenUse{InputTokens: &in, OutputTokens: &out},
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
	if got := row.RowValue(); got != "14:32:01  codex  gpt  200  145ms" {
		t.Fatalf("row value = %q", got)
	}
}

func TestLiveOperatorAdapter_UnsupportedCommandsAreExplicit(t *testing.T) {
	t.Parallel()

	adapter := newLiveOperatorAdapterWithClient(&fakeOperatorClient{}, "http://127.0.0.1:7926")

	if _, err := adapter.SaveWorkspace(context.Background(), ports.SaveWorkspaceRequest{ID: "+", Slug: "new"}); !errors.Is(err, ErrUnsupportedCommand) {
		t.Fatalf("draft SaveWorkspace error = %v, want ErrUnsupportedCommand", err)
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
				{Ref: "cfg-fast", ProviderSpec: "openai_compatible", ModelID: "gpt-4.1", BaseURL: "https://fast.example/v1"},
				{Ref: "cfg-local", ProviderSpec: "openai_compatible", ModelID: "llama3.2", BaseURL: "http://127.0.0.1:11434/v1"},
			},
		}},
	}
	adapter := newLiveOperatorAdapterWithClient(client, "http://127.0.0.1:7926")

	route, err := adapter.SaveRoute(context.Background(), ports.SaveRouteRequest{
		WorkspaceID: "dev",
		RouteID:     "gpt-4.1",
		ModelName:   "gpt-4.2",
		Default:     true,
	})
	if err != nil {
		t.Fatalf("SaveRoute returned error: %v", err)
	}
	if got, want := route.ModelName, "gpt-4.2"; got != want {
		t.Fatalf("route model = %q, want %q", got, want)
	}
	if got, want := client.upserted.ProviderConfigs[0].ModelID, "gpt-4.2"; got != want {
		t.Fatalf("saved model = %q, want %q", got, want)
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
				{Ref: "cfg-fast", ProviderSpec: "openai_compatible", ModelID: "gpt-4.1", BaseURL: "https://fast.example/v1"},
				{Ref: "cfg-deep", ProviderSpec: "openai_compatible", ModelID: "gpt-4.1", BaseURL: "https://deep.example/v1"},
				{Ref: "cfg-local", ProviderSpec: "openai_compatible", ModelID: "llama3.2", BaseURL: "http://127.0.0.1:11434/v1"},
			},
		}},
	}
	adapter := newLiveOperatorAdapterWithClient(client, "http://127.0.0.1:7926")

	if err := adapter.DeleteRoute(context.Background(), ports.DeleteRouteRequest{WorkspaceID: "dev", RouteID: "gpt-4.1"}); err != nil {
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
				ModelID:      "gpt-4.1",
				BaseURL:      "https://fast.example/v1",
			}},
		}},
	}
	adapter := newLiveOperatorAdapterWithClient(client, "http://127.0.0.1:7926")

	edited, err := adapter.SaveTarget(context.Background(), ports.SaveTargetRequest{
		WorkspaceID:   "dev",
		RouteID:       "gpt-4.1",
		TargetID:      "cfg-fast",
		Name:          "fast",
		Provider:      "openai_compatible",
		Model:         "gpt-4.1",
		BaseURL:       "https://new-fast.example/v1",
		CredentialRef: "env:FAST_KEY",
	})
	if err != nil {
		t.Fatalf("SaveTarget edit returned error: %v", err)
	}
	if edited.ID != "cfg-fast" || edited.BaseURL != "https://new-fast.example/v1" || edited.Name != "fast" {
		t.Fatalf("edited target = %#v", edited)
	}

	added, err := adapter.SaveTarget(context.Background(), ports.SaveTargetRequest{
		WorkspaceID:   "dev",
		RouteID:       "gpt-4.1",
		Name:          "deep",
		Provider:      "openai_compatible",
		Model:         "gpt-4.1",
		BaseURL:       "https://deep.example/v1",
		CredentialRef: "env:DEEP_KEY",
	})
	if err != nil {
		t.Fatalf("SaveTarget add returned error: %v", err)
	}
	if added.ID == "" || added.Name != "deep" || added.Model != "gpt-4.1" {
		t.Fatalf("added target = %#v", added)
	}
	if got := len(client.upserted.ProviderConfigs); got != 2 {
		t.Fatalf("saved configs = %d, want 2", got)
	}
}

func TestLiveOperatorAdapter_DeleteTargetRemovesConfigAndPreservesEndpointInvariant(t *testing.T) {
	t.Parallel()

	client := &fakeOperatorClient{
		endpoints: []operatorclient.EndpointData{{
			Name:        "dev",
			SelectedRef: "cfg-fast",
			ProviderConfigs: []operatorclient.ProviderConfigData{
				{Ref: "cfg-fast", ProviderSpec: "openai_compatible", ModelID: "gpt-4.1", BaseURL: "https://fast.example/v1"},
				{Ref: "cfg-deep", ProviderSpec: "openai_compatible", ModelID: "gpt-4.1", BaseURL: "https://deep.example/v1"},
			},
		}},
	}
	adapter := newLiveOperatorAdapterWithClient(client, "http://127.0.0.1:7926")

	if err := adapter.DeleteTarget(context.Background(), ports.DeleteTargetRequest{WorkspaceID: "dev", RouteID: "gpt-4.1", TargetID: "cfg-fast"}); err != nil {
		t.Fatalf("DeleteTarget returned error: %v", err)
	}
	if got := len(client.upserted.ProviderConfigs); got != 1 {
		t.Fatalf("remaining configs = %d, want 1", got)
	}
	if got, want := client.upserted.SelectedRef, "cfg-deep"; got != want {
		t.Fatalf("selected ref = %q, want %q", got, want)
	}

	if err := adapter.DeleteTarget(context.Background(), ports.DeleteTargetRequest{WorkspaceID: "dev", RouteID: "gpt-4.1", TargetID: "cfg-deep"}); !errors.Is(err, ErrUnsupportedCommand) {
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
	}, runCommandIO{
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

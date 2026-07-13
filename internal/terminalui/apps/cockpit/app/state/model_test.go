package state

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/ports"
	"github.com/swobuforge/swobu/internal/profile"
	stateeffect "github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state/effect"
	stateModel "github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state/model"
)

func TestReduce_CreateAndRenameEndpointUpdatesCurrentSelectionAndCatalog(t *testing.T) {
	t.Parallel()

	model := Model{
		Endpoints:       []string{"acme"},
		CurrentEndpoint: "acme",
		Catalog: []CatalogEntry{{
			EndpointName:      "acme",
			ProviderConfigRef: "backend-a",
			ProviderSpec:      "openai_compatible",
			ModelIDs:          []string{"gpt-4.1-mini"},
		}},
		EndpointSnapshots: []EndpointSnapshot{{
			Name:                      "acme",
			SelectedProviderConfigRef: "backend-a",
			ProviderConfigs: []ProviderConfigSnapshot{{
				Ref:          "backend-a",
				ProviderSpec: "openai_compatible",
				ModelID:      "gpt-4.1-mini",
			}},
		}},
	}

	Reduce(&model, CreateEndpoint{Name: "staging"})
	if got := model.CurrentEndpoint; got != "staging" {
		t.Fatalf("current endpoint after create = %q, want staging", got)
	}

	Reduce(&model, SelectEndpoint{Name: "acme"})
	Reduce(&model, RenameCurrentEndpoint{Name: "prod"})
	if got := model.CurrentEndpoint; got != "prod" {
		t.Fatalf("current endpoint after rename = %q, want prod", got)
	}
	if got := model.Catalog[0].EndpointName; got != "prod" {
		t.Fatalf("catalog endpoint after rename = %q, want prod", got)
	}
	if got := model.EndpointSnapshots[0].Name; got != "prod" {
		t.Fatalf("endpoint snapshot name after rename = %q, want prod", got)
	}
}

func TestReduce_CreateDraftSelectionAndCreateSuccessClearsDraft(t *testing.T) {
	t.Parallel()

	model := Model{Endpoints: []string{"acme"}}

	Reduce(&model, SetCreateDraftName{Name: "jobs"})
	Reduce(&model, SetCreateDraftProviderSpec{ProviderSpec: "openai_compatible"})
	Reduce(&model, SetCreateDraftModelIDAction{ModelID: "gpt-4.1-mini"})
	Reduce(&model, SetCreateDraftCredentialRef{CredentialRef: "cred-a"})
	Reduce(&model, SetCreateDraftBaseURL{BaseURL: "https://example.test/v1"})
	Reduce(&model, WorkspaceCreateRequested{Name: "jobs"})
	Reduce(&model, WorkspaceSaveSucceeded{PreviousName: "", Name: "jobs"})

	if got := model.CreateDraftName; got != "" {
		t.Fatalf("create draft name after success = %q, want cleared", got)
	}
	if got := model.CreateDraftProviderConfig.ProviderSpec; got != "" {
		t.Fatalf("create draft provider after success = %#v, want cleared", model.CreateDraftProviderConfig)
	}
	if got := model.CurrentEndpoint; got != "jobs" {
		t.Fatalf("current endpoint after create success = %q, want jobs", got)
	}
}

func TestReduce_CreateDraftModelAndCredentialChanges_DoNotMutateProtocolFields(t *testing.T) {
	t.Parallel()

	model := Model{
		CreateDraftProviderConfig: ProviderConfigSnapshot{
			Ref:              DraftProviderRef,
			ProviderSpec:     "openai",
			BaseURL:          "https://api.openai.com/v1",
			CredentialRef:    "env:OPENAI_API_KEY",
			ProviderProtocol: "responses_stream",
		},
	}

	Reduce(&model, SetCreateDraftModelIDAction{ModelID: "gpt-4.1-mini"})
	Reduce(&model, SetCreateDraftCredentialRef{CredentialRef: "env:OPENAI_API_KEY"})

	if got := model.CreateDraftProviderConfig.ProviderProtocol; got != "responses_stream" {
		t.Fatalf("provider protocol mutated to %q; want responses_stream", got)
	}
}

func TestReduce_SetCreateDraftAuthHeader_DefaultsAndResetsModelSelection(t *testing.T) {
	t.Parallel()

	model := Model{
		CreateDraftProviderConfig: ProviderConfigSnapshot{
			ProviderSpec:     "openai_compatible",
			ModelID:          "gpt-4.1-mini",
			AuthHeader:       "X-Custom-Auth",
			ProviderProtocol: "responses_stream",
		},
		CreateDraftModelDeployments:  []ports.ProviderDeploymentRecord{{Name: "gpt-4.1-mini"}},
		CreateDraftModelProbePending: true,
		CreateDraftModelError:        "stale",
	}

	Reduce(&model, SetCreateDraftAuthHeaderAction{AuthHeader: ""})

	if got := model.CreateDraftProviderConfig.AuthHeader; got != "Authorization" {
		t.Fatalf("auth header=%q want Authorization", got)
	}
	if got := model.CreateDraftProviderConfig.ModelID; got != "" {
		t.Fatalf("model id=%q want cleared", got)
	}
	if model.CreateDraftModelProbePending {
		t.Fatal("create draft model probe pending=true want false")
	}
	if model.CreateDraftModelError != "" {
		t.Fatalf("create draft model error=%q want cleared", model.CreateDraftModelError)
	}
}

func TestReduce_CreateDraftProtocolChangeClearsWorkspaceSaveError(t *testing.T) {
	t.Parallel()

	model := Model{
		WorkspaceSaveError: "provider protocol \"responses\" is unsupported for provider \"bedrock\"",
		CreateDraftProviderConfig: ProviderConfigSnapshot{
			Ref:              DraftProviderRef,
			ProviderSpec:     "bedrock",
			ProviderProtocol: "responses",
		},
	}

	Reduce(&model, SetCreateDraftProviderProtocol{ProviderProtocol: "auto"})

	if got := model.WorkspaceSaveError; got != "" {
		t.Fatalf("workspace save error after protocol edit = %q, want cleared", got)
	}
	if got := model.CreateDraftProviderConfig.ProviderProtocol; got != "auto" {
		t.Fatalf("provider protocol after edit = %q, want auto", got)
	}
}

func TestReduce_SetCreateDraftTargetAlias_CanonicalizesValue(t *testing.T) {
	t.Parallel()

	model := Model{}
	Reduce(&model, SetCreateDraftTargetAlias{TargetAlias: "  FAST  "})

	if got := model.CreateDraftProviderConfig.TargetAlias; got != "fast" {
		t.Fatalf("target alias = %q, want fast", got)
	}
}

func TestReduce_WorkspaceRequestActionsEmitSaveEffects(t *testing.T) {
	t.Parallel()

	model := Model{
		CurrentEndpoint: "acme",
		CreateDraftProviderConfig: ProviderConfigSnapshot{
			Ref:          canonical.PrimaryTargetSelector,
			ProviderSpec: "ollama",
			BaseURL:      "http://127.0.0.1:11434/v1",
			ModelID:      "llama3.1",
		},
	}

	createEffects := Reduce(&model, WorkspaceCreateRequested{Name: "jobs"})
	if len(createEffects) != 1 {
		t.Fatalf("create effect count = %d, want 1", len(createEffects))
	}
	if _, ok := createEffects[0].(SaveNewWorkspaceEffect); !ok {
		t.Fatalf("create effect type = %T, want SaveNewWorkspaceEffect", createEffects[0])
	}

	renameEffects := Reduce(&model, WorkspaceRenameRequested{CurrentName: "acme", Name: "prod"})
	if len(renameEffects) != 1 {
		t.Fatalf("rename effect count = %d, want 1", len(renameEffects))
	}
	if _, ok := renameEffects[0].(SaveWorkspaceNameEffect); !ok {
		t.Fatalf("rename effect type = %T, want SaveWorkspaceNameEffect", renameEffects[0])
	}
}

func TestReduce_ReplaceEndpointsSeedsWorkspaceRailWithoutCatalog(t *testing.T) {
	t.Parallel()

	model := Model{}

	Reduce(&model, ReplaceEndpoints{Snapshots: []EndpointSnapshot{{
		Name:                      "beta",
		SelectedProviderConfigRef: "backend-b",
		ProviderConfigs: []ProviderConfigSnapshot{{
			Ref:          "backend-b",
			ProviderSpec: "openai_compatible",
		}},
	}}})

	if got := model.CurrentEndpoint; got != "beta" {
		t.Fatalf("current endpoint = %q, want beta", got)
	}
}

func TestReduce_ReplaceEndpointsPreservesExplicitCreateLaneSelection(t *testing.T) {
	t.Parallel()

	model := Model{
		Endpoints:       []string{"acme", "staging"},
		CurrentEndpoint: "",
	}

	Reduce(&model, ReplaceEndpoints{Snapshots: []EndpointSnapshot{
		{Name: "staging"},
		{Name: "acme"},
	}})

	if got := model.CurrentEndpoint; got != "" {
		t.Fatalf("current endpoint after refresh = %q, want explicit create lane", got)
	}
}

func TestReduce_RoutingSelectionAndStreamUpdates(t *testing.T) {
	t.Parallel()

	model := Model{
		StreamEnabled: true,
		EndpointSnapshots: []EndpointSnapshot{{
			Name:                      "acme",
			SelectedProviderConfigRef: "backend-a",
			ProviderConfigs: []ProviderConfigSnapshot{
				{Ref: "backend-a", ProviderSpec: "openai", ModelID: "gpt-4.1-mini"},
				{Ref: "backend-b", ProviderSpec: "anthropic"},
			},
		}},
		Catalog: []CatalogEntry{{
			EndpointName:      "acme",
			ProviderConfigRef: "backend-a",
		}},
	}

	Reduce(&model, RoutingSaveStartedAction{})
	Reduce(&model, RoutingSaveSucceeded{EndpointName: "acme", ProviderRef: "backend-b"})
	Reduce(&model, ToggleStream{})

	if got := model.EndpointSnapshots[0].SelectedProviderConfigRef; got != "backend-b" {
		t.Fatalf("selected provider ref = %q, want backend-b", got)
	}
	if got := model.Catalog[0].ProviderConfigRef; got != "backend-b" {
		t.Fatalf("catalog provider config ref = %q, want backend-b", got)
	}
	if model.StreamEnabled {
		t.Fatal("stream enabled = true, want false after toggle")
	}
}

func TestReduce_WorkspaceAndRoutingSaveTrackAnchoredErrors(t *testing.T) {
	t.Parallel()

	model := Model{
		HeaderStatus:    "ready",
		Endpoints:       []string{"acme"},
		CurrentEndpoint: "acme",
		EndpointSnapshots: []EndpointSnapshot{{
			Name:                      "acme",
			SelectedProviderConfigRef: "backend-a",
			ProviderConfigs: []ProviderConfigSnapshot{{
				Ref:          "backend-a",
				ProviderSpec: "openai_compatible",
				ModelID:      "gpt-4.1-mini",
			}},
		}},
		Catalog: []CatalogEntry{{EndpointName: "acme"}},
	}

	Reduce(&model, WorkspaceRenameRequested{CurrentName: "acme", Name: "prod"})
	Reduce(&model, WorkspaceSaveSucceeded{PreviousName: "acme", Name: "prod"})
	if got := model.CurrentEndpoint; got != "prod" {
		t.Fatalf("current endpoint after success = %q, want prod", got)
	}

	Reduce(&model, WorkspaceCreateRequested{Name: "beta"})
	Reduce(&model, WorkspaceSaveFailed{Message: "endpoint could not be saved"})
	if got := model.WorkspaceSaveError; got != "endpoint could not be saved" {
		t.Fatalf("workspace save error = %q, want endpoint could not be saved", got)
	}

	Reduce(&model, RoutingSaveStartedAction{})
	Reduce(&model, RoutingSaveFailed{Message: "selected target could not be saved", ErrorAnchor: "routing-row/run_on"})
	if got := model.SaveErrors["routing-row/run_on"]; got != "selected target could not be saved" {
		t.Fatalf("routing save error = %q, want selected target could not be saved", got)
	}
}

func TestReduce_ProviderAuthSessionFailedAnchorsAuthErrorOnly(t *testing.T) {
	t.Parallel()

	model := Model{
		HeaderStatus:    "waiting for login…",
		InteractionMode: InteractionModeBusySave,
		SaveErrors:      map[string]string{"routing-row/run_on": "previous routing failure"},
		AuthSessions: map[string]stateModel.AuthSessionViewState{
			stateModel.EndpointProviderAuthOwnerKey("testname", "openai/main").String(): {
				SessionID:    "sess-1",
				URL:          "https://auth.openai.com",
				SessionState: "pending",
			},
		},
	}

	Reduce(&model, stateeffect.ProviderAuthSessionFailedAction{
		EndpointName: "testname",
		ProviderConfig: ProviderConfigSnapshot{
			Ref:          "openai/main",
			ProviderSpec: "openai",
		},
		OwnerKey: stateModel.EndpointProviderAuthOwnerKey("testname", "openai/main").String(),
		Message:  "login timed out; retry",
	})

	if len(model.SaveErrors) != 0 {
		t.Fatalf("routing save errors = %#v, want cleared", model.SaveErrors)
	}
	auth := model.AuthSessions[stateModel.EndpointProviderAuthOwnerKey("testname", "openai/main").String()]
	if got := auth.SessionError; got != "login timed out; retry" {
		t.Fatalf("auth login session error = %q, want timeout message", got)
	}
	if got := auth.SessionID; got != "sess-1" {
		t.Fatalf("auth login session id = %q, want preserved", got)
	}
	if got := auth.URL; got != "https://auth.openai.com" {
		t.Fatalf("auth login url = %q, want preserved", got)
	}
	if got := model.InteractionMode; got != InteractionModeManageList {
		t.Fatalf("interaction mode = %q, want %q", got, InteractionModeManageList)
	}
}

func TestReduce_StoreKeychainCredentialTransitionsThroughBusyAndSaved(t *testing.T) {
	t.Parallel()

	model := Model{
		HeaderStatus:    "ready",
		InteractionMode: InteractionModeNAV,
	}

	effects := Reduce(&model, StoreKeychainCredentialRequested{
		ProviderSpec: "openrouter",
		KeyName:      "openrouter/default",
		Secret:       "token-123",
	})
	if got := model.HeaderStatus; got != "saving…" {
		t.Fatalf("header status during store = %q, want saving…", got)
	}
	if got := model.InteractionMode; got != InteractionModeBusySave {
		t.Fatalf("interaction mode during store = %q, want busy-save", got)
	}
	if len(effects) != 1 {
		t.Fatalf("effect count = %d, want 1", len(effects))
	}
	if _, ok := effects[0].(StoreKeychainCredentialEffect); !ok {
		t.Fatalf("effect type = %T, want StoreKeychainCredentialEffect", effects[0])
	}

	Reduce(&model, KeychainCredentialStored{ProviderSpec: "openrouter", KeyName: "openrouter/default"})
	if got := model.HeaderStatus; got != "saved" {
		t.Fatalf("header status after store = %q, want saved", got)
	}
	if got := model.InteractionMode; got != InteractionModeNAV {
		t.Fatalf("interaction mode after store = %q, want nav", got)
	}
}

func TestReduce_StoreKeychainCredentialReloadsActiveDraftCatalog(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		model     Model
		wantScope string
		wantRef   string
		wantProv  string
	}{
		{
			name: "create draft",
			model: Model{
				CreateDraftProviderConfig: ProviderConfigSnapshot{
					ProviderSpec:     "openai",
					CredentialRef:    "keychain",
					ProviderProtocol: "auto",
				},
			},
			wantScope: RoutingModelCatalogScopeCreateDraft,
			wantRef:   "keychain",
			wantProv:  "openai",
		},
		{
			name: "add model draft",
			model: Model{
				CurrentEndpoint:               "acme",
				AddModelDraftProviderSpec:     "openai",
				AddModelDraftBaseURL:          "https://api.openai.com/v1",
				AddModelDraftAuthHeader:       "Authorization",
				AddModelDraftCredentialRef:    "keychain",
				AddModelDraftProviderProtocol: "auto",
			},
			wantScope: RoutingModelCatalogScopeAddModelDraft,
			wantRef:   "keychain",
			wantProv:  "openai",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			effects := Reduce(&tt.model, KeychainCredentialStored{ProviderSpec: "openai", KeyName: "openai/default"})
			if len(effects) != 1 {
				t.Fatalf("effect count = %d, want 1", len(effects))
			}
			load, ok := effects[0].(LoadRoutingModelCatalogEffect)
			if !ok {
				t.Fatalf("effect type = %T, want LoadRoutingModelCatalogEffect", effects[0])
			}
			if load.Scope != tt.wantScope {
				t.Fatalf("scope=%q want %q", load.Scope, tt.wantScope)
			}
			if load.ProviderSpec != tt.wantProv {
				t.Fatalf("provider spec=%q want %q", load.ProviderSpec, tt.wantProv)
			}
			if load.CredentialRef != tt.wantRef {
				t.Fatalf("credential ref=%q want %q", load.CredentialRef, tt.wantRef)
			}
		})
	}
}

func TestReduce_EndpointCopyNoteAnchorsAndClearsOnWorkspaceSelectionChange(t *testing.T) {
	t.Parallel()

	model := Model{
		Endpoints:       []string{"acme", "staging"},
		CurrentEndpoint: "acme",
	}

	Reduce(&model, EndpointCopyNoted{Message: "copied"})
	Reduce(&model, SelectEndpoint{Name: "staging"})
	if got := model.WorkspaceCopyNote; got != "" {
		t.Fatalf("workspace copy note after selection = %q, want cleared", got)
	}
}

func TestProviderConfigForSpec_DefaultsToAutoProtocolForAllProviders(t *testing.T) {
	t.Parallel()

	got := ProviderConfigForSpec("openrouter", ProviderConfigSnapshot{})
	if got.BaseURL != "https://openrouter.ai/api/v1" {
		t.Fatalf("openrouter base url = %q", got.BaseURL)
	}
	if !ProviderRequiresCredential("openrouter", got.BaseURL) {
		t.Fatal("openrouter should require a credential")
	}
	if ProviderRequiresCredential("ollama", "http://127.0.0.1:11434/v1") {
		t.Fatal("ollama should not require a credential")
	}

	anthropic := ProviderConfigForSpec("anthropic", ProviderConfigSnapshot{})
	if got := anthropic.ProviderProtocol; got != "auto" {
		t.Fatalf("anthropic provider protocol=%q want auto", got)
	}

	switching := ProviderConfigForSpec("anthropic", ProviderConfigSnapshot{ProviderProtocol: "chat_completions"})
	if got := switching.ProviderProtocol; got != "auto" {
		t.Fatalf("invalid protocol should normalize to auto, got=%q", got)
	}

	chatgpt := ProviderConfigForSpec("chatgpt", ProviderConfigSnapshot{})
	if got := chatgpt.ProviderProtocol; got != "auto" {
		t.Fatalf("chatgpt provider protocol=%q want auto", got)
	}

	azure := ProviderConfigForSpec("azure", ProviderConfigSnapshot{})
	if got := azure.ProviderProtocol; got != "auto" {
		t.Fatalf("azure provider protocol=%q want auto", got)
	}
	if got := azure.BaseURL; got != "" {
		t.Fatalf("azure base URL=%q want empty", got)
	}
	azureHost := ProviderConfigForSpec("azure", ProviderConfigSnapshot{BaseURL: "swobu-useast-resource.services.ai.azure.com"})
	if got := azureHost.BaseURL; got != "https://swobu-useast-resource.services.ai.azure.com" {
		t.Fatalf("azure normalized base URL=%q want single canonical suffix", got)
	}
	openAICompatible := ProviderConfigForSpec("openai_compatible", ProviderConfigSnapshot{})
	if got := openAICompatible.AuthHeader; got != "Authorization" {
		t.Fatalf("openai-compatible auth header=%q want Authorization", got)
	}
	if got := stateModel.ProviderAuthHeaderOptions("openai_compatible"); len(got) != 3 || got[0] != "Authorization" || got[1] != "X-API-Key" || got[2] != "api-key" {
		t.Fatalf("openai-compatible auth header options=%v", got)
	}
	customAuthHeader := ProviderConfigForSpec("openai_compatible", ProviderConfigSnapshot{AuthHeader: "X-Custom-Auth"})
	if got := customAuthHeader.AuthHeader; got != "X-Custom-Auth" {
		t.Fatalf("openai-compatible custom auth header=%q want X-Custom-Auth", got)
	}
	if got := ProviderConfigForSpec("openrouter", ProviderConfigSnapshot{AuthHeader: "Authorization"}); got.AuthHeader != "" {
		t.Fatalf("openrouter auth header=%q want cleared", got.AuthHeader)
	}
	if got := profile.DefaultEnvKeyForSpec("azure"); got != "AZURE_OPENAI_API_KEY" {
		t.Fatalf("azure default env key=%q want AZURE_OPENAI_API_KEY", got)
	}
	if !ProviderRequiresCredential("azure", azure.BaseURL) {
		t.Fatal("azure should require a credential")
	}

	cleared := ProviderConfigForSpec("openrouter", ProviderConfigSnapshot{
		CredentialRef: "env:OLD_KEY",
	})
	if got := cleared.CredentialRef; got != "" {
		t.Fatalf("switching provider should clear credential ref, got %q", got)
	}
}

func TestProviderOptions_UsesCanonicalDisplayOrder(t *testing.T) {
	t.Parallel()

	options := ProviderOptions()
	order := make([]string, 0, len(options))
	for _, option := range options {
		order = append(order, option.Spec)
	}

	wantPrefix := []string{"ollama", "openai", "chatgpt", "anthropic", "openrouter", "bedrock", "azure", "openai_compatible"}
	if len(order) < len(wantPrefix) {
		t.Fatalf("provider options=%v want at least %v", order, wantPrefix)
	}
	for i, want := range wantPrefix {
		if order[i] != want {
			t.Fatalf("provider order=%v want prefix=%v", order, wantPrefix)
		}
	}
}

func TestReduce_DaemonRefreshTickSchedulesFullSyncAndReschedule(t *testing.T) {
	t.Parallel()

	effects := Reduce(&Model{}, DaemonRefreshTick{})
	if len(effects) != 4 {
		t.Fatalf("effect count = %d, want 4", len(effects))
	}
	if _, ok := effects[0].(RefreshDaemonStatusEffect); !ok {
		t.Fatalf("effect[0] = %T, want RefreshDaemonStatusEffect", effects[0])
	}
	if _, ok := effects[1].(RefreshEndpointsEffect); !ok {
		t.Fatalf("effect[1] = %T, want RefreshEndpointsEffect", effects[1])
	}
	if _, ok := effects[2].(RefreshStatusProjectionEffect); !ok {
		t.Fatalf("effect[2] = %T, want RefreshStatusProjectionEffect", effects[2])
	}
	if schedule, ok := effects[3].(ScheduleDaemonRefreshEffect); !ok {
		t.Fatalf("effect[3] = %T, want ScheduleDaemonRefreshEffect", effects[3])
	} else if schedule.Delay <= 0 {
		t.Fatalf("schedule delay = %s, want positive interval", schedule.Delay)
	}
}

func TestReduce_LoadRoutingModelCatalogTracksTupleAndAppliesMatchingResult(t *testing.T) {
	t.Parallel()

	model := Model{}
	effects := Reduce(&model, LoadRoutingModelCatalogRequestedAction{
		Scope:            RoutingModelCatalogScopeAddModelDraft,
		ProviderSpec:     "openrouter",
		ProviderProtocol: "auto",
		BaseURL:          "https://openrouter.ai/api/v1",
		CredentialRef:    "env:OPENROUTER_API_KEY",
	})
	if len(effects) != 1 {
		t.Fatalf("effect count=%d want 1", len(effects))
	}
	if _, ok := effects[0].(LoadRoutingModelCatalogEffect); !ok {
		t.Fatalf("effect type=%T want LoadRoutingModelCatalogEffect", effects[0])
	}
	if got := model.AddModelDraftProviderSpec; got != "openrouter" {
		t.Fatalf("provider spec=%q", got)
	}
	if !model.AddModelDraftModelProbePending {
		t.Fatal("add-model catalog probe should be pending after load request")
	}

	Reduce(&model, RoutingModelCatalogLoaded{
		Scope:            RoutingModelCatalogScopeAddModelDraft,
		ProviderSpec:     "openrouter",
		ProviderProtocol: "auto",
		BaseURL:          "https://openrouter.ai/api/v1",
		CredentialRef:    "env:OPENROUTER_API_KEY",
		Deployments:      []ports.ProviderDeploymentRecord{{Name: "openai/gpt-4.1"}},
	})
	if len(model.AddModelDraftModelDeployments) != 1 || model.AddModelDraftModelDeployments[0].Name != "openai/gpt-4.1" {
		t.Fatalf("model deployments=%v", model.AddModelDraftModelDeployments)
	}
	if model.AddModelDraftModelProbePending {
		t.Fatal("add-model catalog probe pending should clear after load result")
	}

	Reduce(&model, RoutingModelCatalogLoaded{
		Scope:            RoutingModelCatalogScopeAddModelDraft,
		ProviderSpec:     "anthropic",
		ProviderProtocol: "auto",
		BaseURL:          "https://api.anthropic.com",
		CredentialRef:    "env:ANTHROPIC_API_KEY",
		Deployments:      []ports.ProviderDeploymentRecord{{Name: "claude-sonnet-4"}},
	})
	if len(model.AddModelDraftModelDeployments) != 1 || model.AddModelDraftModelDeployments[0].Name != "openai/gpt-4.1" {
		t.Fatalf("mismatched tuple should be ignored; model deployments=%v", model.AddModelDraftModelDeployments)
	}
}

func TestReduce_LoadRoutingModelCatalogCreateDraft_AppliesMatchingResultAgainstRequestedTuple(t *testing.T) {
	active := t.TempDir()
	activeConfig := filepath.Join(active, "config")
	activeCreds := filepath.Join(active, "credentials")
	if err := os.WriteFile(activeConfig, []byte("[profile swobu-bedrock]\nregion = us-east-1\n"), 0o600); err != nil {
		t.Fatalf("write active config: %v", err)
	}
	if err := os.WriteFile(activeCreds, []byte("[swobu-bedrock]\naws_access_key_id = active\naws_secret_access_key = active\n"), 0o600); err != nil {
		t.Fatalf("write active credentials: %v", err)
	}
	t.Setenv("AWS_CONFIG_FILE", activeConfig)
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", activeCreds)

	model := Model{
		CreateDraftProviderConfig: ProviderConfigSnapshot{
			ProviderSpec:     "bedrock",
			ProviderProtocol: "auto",
			BaseURL:          "",
			CredentialRef:    "profile:swobu-bedrock",
		},
	}
	effects := Reduce(&model, LoadRoutingModelCatalogRequestedAction{
		Scope:            RoutingModelCatalogScopeCreateDraft,
		ProviderSpec:     "bedrock",
		ProviderProtocol: "auto",
		BaseURL:          "https://bedrock-mantle.us-east-1.api.aws/v1",
		CredentialRef:    "profile:swobu-bedrock",
	})
	if len(effects) != 1 {
		t.Fatalf("effect count=%d want 1", len(effects))
	}
	if _, ok := effects[0].(LoadRoutingModelCatalogEffect); !ok {
		t.Fatalf("effect type=%T want LoadRoutingModelCatalogEffect", effects[0])
	}
	if !model.CreateDraftModelProbePending {
		t.Fatal("create-draft catalog probe should be pending after load request")
	}

	Reduce(&model, RoutingModelCatalogLoaded{
		Scope:            RoutingModelCatalogScopeCreateDraft,
		ProviderSpec:     "bedrock",
		ProviderProtocol: "auto",
		BaseURL:          "https://bedrock-mantle.us-east-1.api.aws/v1",
		CredentialRef:    "profile:swobu-bedrock",
		Deployments:      []ports.ProviderDeploymentRecord{{Name: "anthropic.claude-sonnet-4-5-20250929-v1:0"}},
	})
	if len(model.CreateDraftModelDeployments) != 1 {
		t.Fatalf("model deployments=%v", model.CreateDraftModelDeployments)
	}
	if model.CreateDraftModelProbePending {
		t.Fatal("create-draft catalog probe pending should clear after load result")
	}
}

func TestReduce_ControlPlaneIncompatibleHardStopsAndRecoversOnCompatibleStatus(t *testing.T) {
	t.Parallel()

	model := Model{
		Endpoints:       []string{"acme"},
		CurrentEndpoint: "acme",
	}
	Reduce(&model, ControlPlaneIncompatibleDetected{
		ExpectedProtocol:  7,
		DaemonProtocol:    6,
		HasDaemonProtocol: true,
		Reason:            "control-plane protocol mismatch",
	})
	if model.ControlPlane == nil {
		t.Fatal("control plane mismatch not set")
	}
	if got := model.HeaderStatus; got != "incompatible" {
		t.Fatalf("header status = %q, want incompatible", got)
	}
	Reduce(&model, SelectEndpoint{Name: ""})
	if got := model.CurrentEndpoint; got != "acme" {
		t.Fatalf("current endpoint changed during incompatible mode: %q", got)
	}
	Reduce(&model, stateeffect.ReplaceDaemonStatus{State: "healthy", EndpointCount: 1})
	if model.ControlPlane != nil {
		t.Fatal("control plane mismatch should clear after healthy status")
	}
}

func TestReduce_DaemonRefreshTickInMismatchModeOnlyRefreshesStatus(t *testing.T) {
	t.Parallel()

	model := Model{
		ControlPlane: &ControlPlaneMismatch{
			ExpectedProtocol:  7,
			DaemonProtocol:    6,
			HasDaemonProtocol: true,
		},
	}
	effects := Reduce(&model, DaemonRefreshTick{})
	if len(effects) != 2 {
		t.Fatalf("effect count = %d, want 2", len(effects))
	}
	if _, ok := effects[0].(RefreshDaemonStatusEffect); !ok {
		t.Fatalf("effect[0] = %T, want RefreshDaemonStatusEffect", effects[0])
	}
	if _, ok := effects[1].(ScheduleDaemonRefreshEffect); !ok {
		t.Fatalf("effect[1] = %T, want ScheduleDaemonRefreshEffect", effects[1])
	}
}

func TestReduce_AddModelCatalogResult_AcceptsMismatchedProviderProtocol(t *testing.T) {
	t.Parallel()

	model := Model{}
	Reduce(&model, LoadRoutingModelCatalogRequestedAction{
		Scope:            RoutingModelCatalogScopeAddModelDraft,
		ProviderSpec:     "bedrock",
		ProviderProtocol: "responses",
		BaseURL:          "https://bedrock-mantle.us-east-1.api.aws/v1",
		CredentialRef:    "profile:default",
	})

	Reduce(&model, RoutingModelCatalogLoaded{
		Scope:            RoutingModelCatalogScopeAddModelDraft,
		ProviderSpec:     "bedrock",
		ProviderProtocol: "messages",
		BaseURL:          "https://bedrock-mantle.us-east-1.api.aws/v1",
		CredentialRef:    "profile:default",
		Deployments:      []ports.ProviderDeploymentRecord{{Name: "should-not-apply"}},
	})

	if len(model.AddModelDraftModelDeployments) != 1 || model.AddModelDraftModelDeployments[0].Name != "should-not-apply" {
		t.Fatalf("model deployments=%v", model.AddModelDraftModelDeployments)
	}
	if model.AddModelDraftModelProbePending {
		t.Fatal("pending flag should clear after catalog load is accepted")
	}
}

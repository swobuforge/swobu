package state

import (
	"testing"

	"github.com/swobuforge/swobu/internal/domain/compatibility"
)

func TestReduce_CreateAndRenameEndpointUpdatesCurrentSelectionAndCatalog(t *testing.T) {
	t.Parallel()

	model := Model{
		Endpoints:       []string{"acme"},
		CurrentEndpoint: "acme",
		Catalog: []CatalogEntry{{
			EndpointName:      "acme",
			ProviderConfigRef: "backend-a",
			ProviderSpec:      "custom",
			ModelIDs:          []string{"gpt-4.1-mini"},
		}},
		EndpointSnapshots: []EndpointSnapshot{{
			Name:                      "acme",
			SelectedProviderConfigRef: "backend-a",
			ProviderConfigs: []ProviderConfigSnapshot{{
				Ref:          "backend-a",
				ProviderSpec: "custom",
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
	Reduce(&model, SetCreateDraftProviderSpec{ProviderSpec: "custom"})
	Reduce(&model, SetCreateDraftModelID{ModelID: "gpt-4.1-mini"})
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

func TestReduce_WorkspaceRequestActionsEmitSaveEffects(t *testing.T) {
	t.Parallel()

	model := Model{
		CurrentEndpoint: "acme",
		CreateDraftProviderConfig: ProviderConfigSnapshot{
			Ref:          compatibility.PrimaryTargetSelector,
			ProviderSpec: "ollama",
			BaseURL:      "http://127.0.0.1:11434/v1",
			ModelID:      "llama3.1",
			ProtocolKind: "chat_completions",
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
			ProviderSpec: "custom",
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
				ProviderSpec: "custom",
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
	Reduce(&model, RoutingSaveFailed{Message: "selected target could not be saved"})
	if got := model.RoutingSaveError; got != "selected target could not be saved" {
		t.Fatalf("routing save error = %q, want selected target could not be saved", got)
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

func TestProviderConfigForSpecUsesLegacyProviderDefaults(t *testing.T) {
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
	if got := anthropic.ProtocolKind; got != "messages" {
		t.Fatalf("anthropic protocol kind = %q, want %q", got, "messages")
	}

	switching := ProviderConfigForSpec("anthropic", ProviderConfigSnapshot{ProtocolKind: "chat_completions"})
	if got := switching.ProtocolKind; got != "messages" {
		t.Fatalf("switching protocol kind = %q, want %q", got, "messages")
	}
}

func TestReduce_DaemonRefreshTickSchedulesFullSyncAndReschedule(t *testing.T) {
	t.Parallel()

	effects := Reduce(&Model{}, DaemonRefreshTick{})
	if len(effects) != 5 {
		t.Fatalf("effect count = %d, want 5", len(effects))
	}
	if _, ok := effects[0].(RefreshDaemonStatusEffect); !ok {
		t.Fatalf("effect[0] = %T, want RefreshDaemonStatusEffect", effects[0])
	}
	if _, ok := effects[1].(RefreshEndpointsEffect); !ok {
		t.Fatalf("effect[1] = %T, want RefreshEndpointsEffect", effects[1])
	}
	if _, ok := effects[2].(RefreshCatalogEffect); !ok {
		t.Fatalf("effect[2] = %T, want RefreshCatalogEffect", effects[2])
	}
	if _, ok := effects[3].(RefreshStatusProjectionEffect); !ok {
		t.Fatalf("effect[3] = %T, want RefreshStatusProjectionEffect", effects[3])
	}
	if schedule, ok := effects[4].(ScheduleDaemonRefreshEffect); !ok {
		t.Fatalf("effect[4] = %T, want ScheduleDaemonRefreshEffect", effects[4])
	} else if schedule.Delay <= 0 {
		t.Fatalf("schedule delay = %s, want positive interval", schedule.Delay)
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
	Reduce(&model, ReplaceDaemonStatus{State: "healthy", EndpointCount: 1})
	if model.ControlPlane != nil {
		t.Fatal("control plane mismatch should clear after compatible status")
	}
}

func TestReduce_DaemonRefreshTickInCompatibilityModeOnlyRefreshesStatus(t *testing.T) {
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

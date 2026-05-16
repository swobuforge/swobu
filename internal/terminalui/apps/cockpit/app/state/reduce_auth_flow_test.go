package state

import (
	"testing"

	stateeffect "github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state/effect"
	stateModel "github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state/model"
)

func TestReduce_ProviderConfigAddedSaved_DoesNotAutoStartAuth(t *testing.T) {
	model := Model{}
	effects := Reduce(&model, stateeffect.ProviderConfigAddedSaved{
		EndpointName: "acme",
		ProviderConfig: ProviderConfigSnapshot{
			Ref:           "chatgpt/main",
			ProviderSpec:  "chatgpt",
			CredentialRef: "chatgpt_login",
			ModelID:       "gpt-4o-mini",
		},
	})
	if model.HeaderStatus != "ready" {
		t.Fatalf("header=%q want ready", model.HeaderStatus)
	}
	if model.InteractionMode != InteractionModeNAV {
		t.Fatalf("mode=%q want %q", model.InteractionMode, InteractionModeNAV)
	}
	if len(effects) != 1 {
		t.Fatalf("effects len=%d want 1", len(effects))
	}
	if _, ok := effects[0].(stateeffect.RefreshEndpointsEffect); !ok {
		t.Fatalf("effects[0]=%T want RefreshEndpointsEffect", effects[0])
	}
}

func TestReduce_ProviderAuthSessionStarted_AutoOpensLoginURL(t *testing.T) {
	model := Model{}
	effects := Reduce(&model, stateeffect.ProviderAuthSessionStarted{
		EndpointName: "acme",
		ProviderConfig: ProviderConfigSnapshot{
			Ref:          "chatgpt/main",
			ProviderSpec: "chatgpt",
		},
		OwnerKey:     stateModel.EndpointProviderAuthOwnerKey("acme", "chatgpt/main").String(),
		SessionID:    "s-1",
		AuthorizeURL: "https://auth.example/login",
		State:        "pending",
	})
	session := model.AuthSessions[stateModel.EndpointProviderAuthOwnerKey("acme", "chatgpt/main").String()]
	if session.URL != "https://auth.example/login" {
		t.Fatalf("auth login url=%q", session.URL)
	}
	if model.InteractionMode != InteractionModeManageList {
		t.Fatalf("interaction mode=%q want=%q", model.InteractionMode, InteractionModeManageList)
	}
	if len(effects) != 1 {
		t.Fatalf("effects len=%d want 1", len(effects))
	}
	open, ok := effects[0].(stateeffect.OpenSupportLinkEffect)
	if !ok {
		t.Fatalf("effects[0]=%T want OpenSupportLinkEffect", effects[0])
	}
	if open.Label != "login" || open.URL != "https://auth.example/login" {
		t.Fatalf("open effect=%+v", open)
	}
}

func TestReduce_ProviderAuthSessionFailed_PreservesExistingLoginURLAndSessionID(t *testing.T) {
	model := Model{
		AuthSessions: map[string]stateModel.AuthSessionView{
			stateModel.EndpointProviderAuthOwnerKey("acme", "chatgpt/main").String(): {
				SessionID:    "s-1",
				URL:          "https://auth.example/login",
				SessionState: "pending",
			},
		},
	}
	effects := Reduce(&model, stateeffect.ProviderAuthSessionFailed{
		EndpointName: "acme",
		ProviderConfig: ProviderConfigSnapshot{
			Ref:          "chatgpt/main",
			ProviderSpec: "chatgpt",
		},
		OwnerKey: stateModel.EndpointProviderAuthOwnerKey("acme", "chatgpt/main").String(),
		Message:  "could not open default browser",
	})
	if len(effects) != 0 {
		t.Fatalf("effects len=%d want 0", len(effects))
	}
	session := model.AuthSessions[stateModel.EndpointProviderAuthOwnerKey("acme", "chatgpt/main").String()]
	if session.SessionID != "s-1" {
		t.Fatalf("auth session id=%q", session.SessionID)
	}
	if session.URL != "https://auth.example/login" {
		t.Fatalf("auth login url=%q", session.URL)
	}
	if session.SessionState != "failed" {
		t.Fatalf("auth session state=%q", session.SessionState)
	}
	if session.SessionError != "could not open default browser" {
		t.Fatalf("auth session error=%q", session.SessionError)
	}
}

func TestReduce_AuthSessionCopyNoted_ScopedToAuthRows(t *testing.T) {
	t.Parallel()
	model := Model{
		WorkspaceCopyNote: "workspace-copied",
	}
	effects := Reduce(&model, stateeffect.AuthSessionCopyNoted{
		OwnerKey: stateModel.EndpointProviderAuthOwnerKey("acme", "cfg-main").String(),
		Message:  "url copied",
	})
	if len(effects) != 0 {
		t.Fatalf("effects len=%d want 0", len(effects))
	}
	session := model.AuthSessions[stateModel.EndpointProviderAuthOwnerKey("acme", "cfg-main").String()]
	if session.CopyNote != "url copied" {
		t.Fatalf("auth login copy note=%q", session.CopyNote)
	}
	if model.WorkspaceCopyNote != "workspace-copied" {
		t.Fatalf("workspace copy note=%q", model.WorkspaceCopyNote)
	}
}

func TestReduce_ProviderAuthSessionCredentialResolved_CreateDraftTransient(t *testing.T) {
	t.Parallel()

	model := Model{
		CreateDraftProviderConfig: ProviderConfigSnapshot{
			ProviderSpec:  "chatgpt",
			CredentialRef: "chatgpt_login",
			BaseURL:       "https://api.openai.com/v1",
		},
	}
	effects := Reduce(&model, stateeffect.ProviderAuthSessionCredentialResolved{
		EndpointName: "",
		ProviderConfig: ProviderConfigSnapshot{
			Ref:           "create-draft",
			ProviderSpec:  "chatgpt",
			BaseURL:       "https://api.openai.com/v1",
			CredentialRef: "chatgpt_login",
		},
		OwnerKey:      stateModel.CreateDraftAuthOwnerKey("create-draft").String(),
		AuthScope:     stateModel.AuthScopeCreateDraft,
		CredentialRef: "keychain:chatgpt/default",
	})
	if len(effects) != 0 {
		t.Fatalf("effects len=%d want 0", len(effects))
	}
	if got := model.CreateDraftProviderConfig.CredentialRef; got != "keychain:chatgpt/default" {
		t.Fatalf("create draft credential ref=%q", got)
	}
}

func TestReduce_AddModelAuthFlow_DoesNotMutateGlobalProviderAuthRows(t *testing.T) {
	t.Parallel()

	existingOwner := stateModel.EndpointProviderAuthOwnerKey("acme", "cfg-existing").String()
	addOwner := stateModel.AddModelDraftAuthOwnerKey("acme", "cfg-add").String()
	model := Model{
		AuthSessions: map[string]stateModel.AuthSessionView{
			existingOwner: {SessionID: "sess-existing", URL: "https://existing.example/login", SessionState: "pending"},
		},
	}
	effects := Reduce(&model, stateeffect.ProviderAuthSessionStarted{
		EndpointName: "acme",
		ProviderConfig: ProviderConfigSnapshot{
			Ref:          "cfg-add",
			ProviderSpec: "chatgpt",
		},
		OwnerKey:     addOwner,
		AuthScope:    stateModel.AuthScopeEndpointProvider,
		SessionID:    "sess-add",
		AuthorizeURL: "https://add.example/login",
		State:        "pending",
	})
	if len(effects) != 1 {
		t.Fatalf("effects len=%d want 1", len(effects))
	}
	if got := model.AuthSessions[existingOwner].SessionID; got != "sess-existing" {
		t.Fatalf("global auth session id=%q want sess-existing", got)
	}
	if got := model.AuthSessions[addOwner].SessionID; got != "sess-add" {
		t.Fatalf("add-model auth session id=%q want sess-add", got)
	}
}

func TestReduce_NonAddModelAuthFlow_DoesNotOverwriteAddModelAuthState(t *testing.T) {
	t.Parallel()

	addOwner := stateModel.AddModelDraftAuthOwnerKey("acme", "cfg-add").String()
	existingOwner := stateModel.EndpointProviderAuthOwnerKey("acme", "cfg-existing").String()
	model := Model{
		AuthSessions: map[string]stateModel.AuthSessionView{
			addOwner: {SessionID: "sess-add", URL: "https://add.example/login", SessionState: "pending"},
		},
	}
	effects := Reduce(&model, stateeffect.ProviderAuthSessionStarted{
		EndpointName: "acme",
		ProviderConfig: ProviderConfigSnapshot{
			Ref:          "cfg-existing",
			ProviderSpec: "chatgpt",
		},
		OwnerKey:     existingOwner,
		AuthScope:    stateModel.AuthScopeEndpointProvider,
		SessionID:    "sess-existing",
		AuthorizeURL: "https://existing.example/login",
		State:        "pending",
	})
	if len(effects) != 1 {
		t.Fatalf("effects len=%d want 1", len(effects))
	}
	if got := model.AuthSessions[addOwner].SessionID; got != "sess-add" {
		t.Fatalf("add-model auth session id=%q want sess-add", got)
	}
	if got := model.AuthSessions[existingOwner].SessionID; got != "sess-existing" {
		t.Fatalf("global auth session id=%q want sess-existing", got)
	}
}

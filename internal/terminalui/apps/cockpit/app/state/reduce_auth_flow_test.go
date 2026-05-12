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
		AuthSubject:  "subject:acme#cfg-x",
		SessionID:    "s-1",
		AuthorizeURL: "https://auth.example/login",
		State:        "pending",
	})
	if model.AuthLoginURL != "https://auth.example/login" {
		t.Fatalf("auth login url=%q", model.AuthLoginURL)
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

func TestReduce_AuthLoginCopyNoted_ScopedToAuthRows(t *testing.T) {
	t.Parallel()
	model := Model{
		WorkspaceCopyNote: "workspace-copied",
	}
	effects := Reduce(&model, stateeffect.AuthLoginCopyNoted{Message: "url copied"})
	if len(effects) != 0 {
		t.Fatalf("effects len=%d want 0", len(effects))
	}
	if model.AuthLoginCopyNote != "url copied" {
		t.Fatalf("auth login copy note=%q", model.AuthLoginCopyNote)
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
		AuthSubject:   "subject:#create-draft",
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

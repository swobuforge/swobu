package routing

import (
	"testing"

	stateModel "github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state/model"
)

func TestProviderLoginSummary_SignedInReauthInProgress(t *testing.T) {
	t.Parallel()
	summary := providerLoginSummary("chatgpt", "keychain:chatgpt/default", stateModel.AuthSessionViewState{SessionState: "pending"})
	if summary != "signed in (re-auth in progress)" {
		t.Fatalf("summary=%q want signed in (re-auth in progress)", summary)
	}
}

func TestProviderLoginSummary_SignedInIgnoresTransientLoginError(t *testing.T) {
	t.Parallel()
	summary := providerLoginSummary("chatgpt", "keychain:chatgpt/default", stateModel.AuthSessionViewState{SessionState: "failed", SessionError: "could not open default browser"})
	if summary != "signed in" {
		t.Fatalf("summary=%q want signed in", summary)
	}
}

func TestProviderLoginSummary_UnresolvedCredentialKeepsLoginState(t *testing.T) {
	t.Parallel()
	summary := providerLoginSummary("chatgpt", "chatgpt_login", stateModel.AuthSessionViewState{SessionState: "pending"})
	if summary != "login pending" {
		t.Fatalf("summary=%q want login pending", summary)
	}
}

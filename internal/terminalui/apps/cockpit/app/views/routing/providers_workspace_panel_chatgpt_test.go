package routing

import (
	"testing"

	"github.com/swobuforge/swobu/internal/domain/providercatalog"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	stateModel "github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state/model"
)

func TestAddModelCreateReady_ChatGPTDoesNotRequireCredentialRef(t *testing.T) {
	t.Parallel()
	draft := state.ProviderConfigSnapshot{
		ProviderSpec: "chatgpt",
		ModelID:      "openai/gpt-3.5-turbo",
	}
	if !addModelCreateReady(draft) {
		t.Fatal("chatgpt add-model draft should be creatable without credential ref")
	}
}

func TestAddModelCreateReady_OpenRouterStillRequiresCredentialRef(t *testing.T) {
	t.Parallel()
	draft := state.ProviderConfigSnapshot{
		ProviderSpec: "openrouter",
		ModelID:      "openai/gpt-3.5-turbo",
	}
	if addModelCreateReady(draft) {
		t.Fatal("openrouter add-model draft should require credential ref")
	}
}

func TestEffectiveAddModelCredentialRef_UsesResolvedDraftCredential(t *testing.T) {
	t.Parallel()
	model := state.Model{
		AddModelDraftProviderSpec:  "chatgpt",
		AddModelDraftBaseURL:       "",
		AddModelDraftCredentialRef: "chatgpt:acct_a",
	}
	draft := state.ProviderConfigSnapshot{
		ProviderSpec:  "chatgpt",
		BaseURL:       "",
		CredentialRef: "chatgpt_device_auth",
	}
	if got := effectiveAddModelCredentialRef(model, draft); got != "chatgpt:acct_a" {
		t.Fatalf("credential ref=%q", got)
	}
}

func TestInteractiveAddModelCredentialRows_RequireSessionState(t *testing.T) {
	t.Parallel()

	draft := state.ProviderConfigSnapshot{
		Ref:          "cfg-a",
		ProviderSpec: "chatgpt",
	}

	if got := interactiveAddModelCredentialRows(state.Model{}, "chatgpt", "acme", draft, ""); len(got) != 0 {
		t.Fatalf("rows len=%d want 0 for missing strategy", len(got))
	}
	if got := interactiveAddModelCredentialRows(state.Model{}, "chatgpt", "acme", draft, "chatgpt_login"); len(got) == 0 {
		t.Fatal("expected browser auth rows before session start")
	}
	if got := interactiveAddModelCredentialRows(state.Model{}, "chatgpt", "acme", draft, "chatgpt_device_auth"); len(got) != 0 {
		t.Fatal("expected no rows before auth session state exists")
	}

	sessionModel := addModelAuthSessionModel("acme", "cfg-a", stateModel.AuthSessionViewState{
		SessionID:    "sess-1",
		SessionState: "pending",
		URL:          "https://example.com/verify",
	})
	if got := interactiveAddModelCredentialRows(sessionModel, "chatgpt", "acme", draft, "chatgpt_login"); len(got) == 0 {
		t.Fatal("expected status rows when auth session state exists")
	}
}

func TestAddModelCredentialSummary_InteractiveVariantShowsSignedInAfterResolution(t *testing.T) {
	t.Parallel()

	model := state.Model{
		AddModelDraftProviderSpec:  "chatgpt",
		AddModelDraftCredentialRef: "keychain:chatgpt/default",
	}
	draft := state.ProviderConfigSnapshot{
		ProviderSpec:  "chatgpt",
		CredentialRef: "chatgpt_login",
	}
	if got := addModelCredentialSummary(model, draft); got != "signed in" {
		t.Fatalf("summary=%q want signed in", got)
	}
}

func TestAddModelCredentialSummary_UsesDisplayLabelForAuthVariant(t *testing.T) {
	t.Parallel()

	model := state.Model{}
	draft := state.ProviderConfigSnapshot{
		ProviderSpec:  "chatgpt",
		CredentialRef: "chatgpt_device_auth",
	}
	if got := addModelCredentialSummary(model, draft); got != "device code" {
		t.Fatalf("summary=%q want device code", got)
	}
}

func TestAddModelCredentialSummary_MissingWhenUnset(t *testing.T) {
	t.Parallel()

	model := state.Model{}
	draft := state.ProviderConfigSnapshot{
		ProviderSpec:  "chatgpt",
		CredentialRef: "",
	}
	if got := addModelCredentialSummary(model, draft); got != "missing" {
		t.Fatalf("summary=%q want missing", got)
	}
}

func TestClassifyInteractiveAuthPhase_BrowserNotStarted(t *testing.T) {
	t.Parallel()
	model := state.Model{}
	draft := state.ProviderConfigSnapshot{Ref: "cfg-a", ProviderSpec: "chatgpt", CredentialRef: string(providercatalog.AuthVariantChatGPTLogin)}
	if got := classifyInteractiveAuthPhase(model, "acme", draft, providercatalog.AuthVariantChatGPTLogin); got != interactiveAuthPhaseStartRequired {
		t.Fatalf("state=%q want=%q", got, interactiveAuthPhaseStartRequired)
	}
}

func TestClassifyInteractiveAuthPhase_BrowserInProgress(t *testing.T) {
	t.Parallel()
	model := addModelAuthSessionModel("acme", "cfg-a", stateModel.AuthSessionViewState{
		SessionID:    "sess-1",
		SessionState: "pending",
	})
	draft := state.ProviderConfigSnapshot{Ref: "cfg-a", ProviderSpec: "chatgpt", CredentialRef: string(providercatalog.AuthVariantChatGPTLogin)}
	if got := classifyInteractiveAuthPhase(model, "acme", draft, providercatalog.AuthVariantChatGPTLogin); got != interactiveAuthPhaseInProgress {
		t.Fatalf("state=%q want=%q", got, interactiveAuthPhaseInProgress)
	}
}

func TestClassifyInteractiveAuthPhase_CreateDraftBrowserInProgress(t *testing.T) {
	t.Parallel()
	model := state.Model{
		AuthSessions: map[string]stateModel.AuthSessionViewState{
			stateModel.CreateDraftAuthOwnerKey("create-draft").String(): {
				SessionID:    "sess-1",
				SessionState: "pending",
				URL:          "https://chatgpt.com/activate",
			},
		},
	}
	draft := state.ProviderConfigSnapshot{Ref: "create-draft", ProviderSpec: "chatgpt", CredentialRef: string(providercatalog.AuthVariantChatGPTLogin)}
	if got := classifyInteractiveAuthPhase(model, "", draft, providercatalog.AuthVariantChatGPTLogin); got != interactiveAuthPhaseInProgress {
		t.Fatalf("state=%q want=%q", got, interactiveAuthPhaseInProgress)
	}
}

func TestClassifyInteractiveAuthPhase_IgnoresSessionFromOtherProviderRef(t *testing.T) {
	t.Parallel()
	model := addModelAuthSessionModel("acme", "cfg-other", stateModel.AuthSessionViewState{
		SessionID:    "sess-1",
		SessionState: "pending",
	})
	draft := state.ProviderConfigSnapshot{Ref: "cfg-a", ProviderSpec: "chatgpt", CredentialRef: string(providercatalog.AuthVariantChatGPTLogin)}
	if got := classifyInteractiveAuthPhase(model, "acme", draft, providercatalog.AuthVariantChatGPTLogin); got != interactiveAuthPhaseStartRequired {
		t.Fatalf("state=%q want=%q", got, interactiveAuthPhaseStartRequired)
	}
}

func TestClassifyInteractiveAuthPhase_DeviceCodeInProgress(t *testing.T) {
	t.Parallel()
	model := addModelAuthSessionModel("acme", "cfg-a", stateModel.AuthSessionViewState{
		SessionID:    "sess-1",
		SessionState: "pending",
		URL:          "https://chatgpt.com/activate",
		UserCode:     "VBMS-V2R4K",
	})
	draft := state.ProviderConfigSnapshot{Ref: "cfg-a", ProviderSpec: "chatgpt", CredentialRef: string(providercatalog.AuthVariantChatGPTDeviceAuth)}
	if got := classifyInteractiveAuthPhase(model, "acme", draft, providercatalog.AuthVariantChatGPTDeviceAuth); got != interactiveAuthPhaseInProgress {
		t.Fatalf("state=%q want=%q", got, interactiveAuthPhaseInProgress)
	}
}

func TestClassifyInteractiveAuthPhase_SignedIn(t *testing.T) {
	t.Parallel()
	model := state.Model{
		AddModelDraftProviderSpec:  "chatgpt",
		AddModelDraftCredentialRef: "keychain:chatgpt/default",
	}
	draft := state.ProviderConfigSnapshot{Ref: "cfg-a", ProviderSpec: "chatgpt", CredentialRef: string(providercatalog.AuthVariantChatGPTLogin)}
	if got := classifyInteractiveAuthPhase(model, "acme", draft, providercatalog.AuthVariantChatGPTLogin); got != interactiveAuthPhaseResolved {
		t.Fatalf("state=%q want=%q", got, interactiveAuthPhaseResolved)
	}
}

func TestClassifyInteractiveAuthPhase_Expired(t *testing.T) {
	t.Parallel()
	model := addModelAuthSessionModel("acme", "cfg-a", stateModel.AuthSessionViewState{
		SessionID:    "sess-1",
		SessionState: "expired",
	})
	draft := state.ProviderConfigSnapshot{Ref: "cfg-a", ProviderSpec: "chatgpt", CredentialRef: string(providercatalog.AuthVariantChatGPTDeviceAuth)}
	if got := classifyInteractiveAuthPhase(model, "acme", draft, providercatalog.AuthVariantChatGPTDeviceAuth); got != interactiveAuthPhaseExpired {
		t.Fatalf("state=%q want=%q", got, interactiveAuthPhaseExpired)
	}
}

func TestClassifyInteractiveAuthPhase_BrowserUnavailable(t *testing.T) {
	t.Parallel()
	model := addModelAuthSessionModel("acme", "cfg-a", stateModel.AuthSessionViewState{
		SessionID:    "sess-1",
		SessionState: "failed",
		SessionError: "could not open default browser",
	})
	draft := state.ProviderConfigSnapshot{Ref: "cfg-a", ProviderSpec: "chatgpt", CredentialRef: string(providercatalog.AuthVariantChatGPTLogin)}
	if got := classifyInteractiveAuthPhase(model, "acme", draft, providercatalog.AuthVariantChatGPTLogin); got != interactiveAuthPhaseStartUnavailable {
		t.Fatalf("state=%q want=%q", got, interactiveAuthPhaseStartUnavailable)
	}
}

func addModelAuthSessionModel(endpointName string, providerRef string, session stateModel.AuthSessionViewState) state.Model {
	ownerKey := stateModel.AddModelDraftAuthOwnerKey(endpointName, providerRef).String()
	return state.Model{AuthSessions: map[string]stateModel.AuthSessionViewState{
		ownerKey: session,
	}}
}

func TestShouldRenderInteractiveAuthCode_DeviceOnly(t *testing.T) {
	t.Parallel()
	if !shouldRenderInteractiveAuthCode(providercatalog.AuthVariantChatGPTDeviceAuth, "VBMS-V2R4K") {
		t.Fatal("device code variant should render code")
	}
	if shouldRenderInteractiveAuthCode(providercatalog.AuthVariantChatGPTLogin, "VBMS-V2R4K") {
		t.Fatal("browser login variant must not render device code")
	}
	if shouldRenderInteractiveAuthCode(providercatalog.AuthVariantChatGPTDeviceAuth, "") {
		t.Fatal("empty user code must not render")
	}
}

func TestInteractiveAuthLinkRows_LongURLAddsWrappedDisclosureRows(t *testing.T) {
	t.Parallel()
	longURL := "https://auth.openai.com/oauth/authorize?client_id=app_EMoamEE123456789&redirect_uri=http%3A%2F%2Flocalhost%2Fcb&response_type=code"
	rows := interactiveAuthLinkRows(longURL, "owner")
	if len(rows) < 2 {
		t.Fatalf("rows=%d want wrapped disclosure rows for long url", len(rows))
	}
}

func TestInteractiveAuthLinkRows_ShortURLStillUsesSingleLinkActionPlusWrappedLines(t *testing.T) {
	t.Parallel()
	rows := interactiveAuthLinkRows("https://chatgpt.com/activate", "owner")
	if len(rows) < 2 {
		t.Fatalf("rows=%d want action + disclosure rows", len(rows))
	}
}

func TestShouldShowAuthStartRetryHint(t *testing.T) {
	t.Parallel()

	if !shouldShowAuthStartRetryHint("device auth start returned status 403", "") {
		t.Fatal("expected retry hint for auth-start failure with empty session id")
	}
	if shouldShowAuthStartRetryHint("credential store failed", "") {
		t.Fatal("did not expect retry hint for credential store failure")
	}
	if shouldShowAuthStartRetryHint("credential store failed: keyring write failed", "") {
		t.Fatal("did not expect retry hint for detailed credential store failure")
	}
	if shouldShowAuthStartRetryHint("device auth start returned status 403", "sess-1") {
		t.Fatal("did not expect retry hint when session id is present")
	}
}

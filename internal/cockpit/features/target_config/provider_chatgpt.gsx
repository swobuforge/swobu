package target_config

import (
	"strings"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/ui"
)

type chatGPTProviderForm struct{ target *TargetConfig }

func ChatGPTProviderForm(w *TargetConfig) tui.Component { return &chatGPTProviderForm{target: w} }

func ChatGPTAuthControl(w *TargetConfig) *ui.SelectableRow {
	_, label := w.interactiveAuthMode()
	if label == "" { label = "browser login" }
	row := ui.NewSelectableRow(TargetAddMountKey(w, "auth-start"), "auth", label, "start ↵", w.ContinueSetup)
	row.AutoFocus = true
	return row
}

func ChatGPTAuthSummary(w *TargetConfig) *ui.SelectableRow {
	_, label := w.interactiveAuthMode()
	if label == "" { label = "browser login" }
	return ui.NewSelectableRow(TargetAddMountKey(w, "auth-pending"), "auth", label, "pending", nil)
}

func ChatGPTAuthOpenBrowser(w *TargetConfig) *ui.SelectableRow {
	url := ""
	if session := w.AuthSession.Get(); session.SessionID != "" { url = session.AuthorizeURL }
	return ui.NewSelectableRow(TargetAddMountKey(w, "auth-open"), "open", url, "open ↵", func() {
		if err := ui.OpenURL(url); err != nil { w.Error.Set(err.Error()) }
	})
}

func ChatGPTAuthStatus(w *TargetConfig) *ui.SelectableRow {
	row := ui.NewSelectableRow(TargetAddMountKey(w, "auth-status"), "status", "waiting for login", "refresh ↵", w.RefreshAuthSession)
	row.AutoFocus = true
	return row
}

func ChatGPTAuthCancel(w *TargetConfig) *ui.SelectableRow {
	return ui.NewSelectableRow(TargetAddMountKey(w, "auth-cancel"), "cancel", "", "cancel ↵", w.CancelAuthSession)
}

func ChatGPTAuthSignedIn(w *TargetConfig) *ui.SelectableRow {
	return ui.NewSelectableRow(TargetAddMountKey(w, "auth-signed-in"), "auth", "signed in", "ok", nil)
}

func ChatGPTAuthFailed(w *TargetConfig) *ui.SelectableRow {
	return ui.NewSelectableRow(TargetAddMountKey(w, "auth-failed"), "auth", "failed", "back ↵", func() { w.Back() })
}

func ChatGPTAuthRetry(w *TargetConfig) *ui.SelectableRow {
	return ui.NewSelectableRow(TargetAddMountKey(w, "auth-retry"), "retry", "", "retry ↵", w.startInteractiveAuth)
}

func ChatGPTAuthUserCode(w *TargetConfig) *ui.SelectableRow {
	code := strings.TrimSpace(w.AuthSession.Get().UserCode)
	return ui.CopyPasteRowComponent(TargetAddMountKey(w, "auth-code"), "code", code, "copy ↵", func() ui.CopyResult {
		return ui.CopyToClipboard(code)
	}, func(result ui.CopyResult) {
		if msg := result.ErrorForDisplay(); msg != "" { w.Error.Set(msg); return }
		w.Error.Set("")
	})
}

templ (f *chatGPTProviderForm) Render() {
	<div class="flex-col w-full" deps={f.target.Draft, f.target.BaseURL}>
		if f.target.Phase.Get() == PhaseAuthPending {
			<div deps={f.target.AuthSession, f.target.Error}>
				@ChatGPTAuthSummary(f.target)
				@ChatGPTAuthOpenBrowser(f.target)
				if strings.TrimSpace(f.target.AuthSession.Get().UserCode) != "" {
					@ChatGPTAuthUserCode(f.target)
				}
				@ChatGPTAuthStatus(f.target)
				@ChatGPTAuthCancel(f.target)
			</div>
			@ModelSelectRow(f.target)
		} else if f.target.Phase.Get() == PhaseAuthFailed {
			@ChatGPTAuthFailed(f.target)
			@ChatGPTAuthRetry(f.target)
		} else {
			if f.target.RequiresInteractiveAuth() && strings.TrimSpace(f.target.Draft.Get().CredentialRef) == "" {
				@ChatGPTAuthControl(f.target)
			} else if f.target.AuthSession.Get().State == "succeeded" {
				@ChatGPTAuthSignedIn(f.target)
			} else if genericCredentialRowVisible(f.target) {
				<div key={credentialRegionKey(f.target)} class="w-full">@CredentialControlRegion(f.target)</div>
			}
		}
	</div>
}

package target_config

import (
	"strings"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/ui"
)

type chatGPTProviderForm struct{ target *TargetConfig }

func ChatGPTProviderForm(w *TargetConfig) tui.Component { return &chatGPTProviderForm{target: w} }

func ChatGPTAuthControl(w *TargetConfig) *ui.SelectableRow {
	row := ui.NewSelectableRow(TargetAddMountKey(w, "auth-start"), "authentication", "signed out", "sign in ↵", w.ContinueSetup)
	row.AutoFocus = true
	return row
}

func ChatGPTAuthSummary(w *TargetConfig) *ui.SelectableRow {
	_, label := w.interactiveAuthMode()
	if label == "" { label = "browser login" }
	return ui.NewSelectableRow(TargetAddMountKey(w, "auth-pending"), "authentication", label, "pending", nil)
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
	row := ui.NewSelectableRow(TargetAddMountKey(w, "auth-signed-in"), "authentication", "signed in", "reconnect ↵", w.startInteractiveAuth)
	row.AutoFocus = true
	return row
}

func ChatGPTAuthFailed(w *TargetConfig) *ui.SelectableRow {
	row := ui.NewSelectableRow(TargetAddMountKey(w, "auth-failed"), "authentication", "failed", "retry ↵", w.startInteractiveAuth)
	row.AutoFocus = true
	return row
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
		if targetAuthPending(f.target) {
			<div deps={f.target.AuthSession, f.target.Error}>
				@ChatGPTAuthSummary(f.target)
				@ChatGPTAuthOpenBrowser(f.target)
				if strings.TrimSpace(f.target.AuthSession.Get().UserCode) != "" {
					@ChatGPTAuthUserCode(f.target)
				}
				@ChatGPTAuthStatus(f.target)
				@ChatGPTAuthCancel(f.target)
			</div>
		} else if targetAuthFailed(f.target) {
			@ChatGPTAuthFailed(f.target)
		} else {
			if targetUsesInteractiveAuth(f.target) && strings.TrimSpace(f.target.Draft.Get().CredentialRef) == "" {
				@ChatGPTAuthControl(f.target)
			} else if strings.TrimSpace(f.target.Draft.Get().CredentialRef) != "" {
				@ChatGPTAuthSignedIn(f.target)
			}
		}
	</div>
}

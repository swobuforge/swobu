package target_config

import (
	"strings"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/ui"
)

type chatGPTProviderForm struct{ target *TargetConfig }

type chatGPTAuthModeMenu struct {
	target         *TargetConfig
	backout        func()
	replaceSession bool
}

func ChatGPTProviderForm(w *TargetConfig) tui.Component { return &chatGPTProviderForm{target: w} }

func ChatGPTAuthControl(w *TargetConfig) *ui.Select {
	_, label := w.interactiveAuthMode()
	return ui.NewSelect(ui.SelectProps{
		ID: TargetAddMountKey(w, "auth-mode"),
		Label: "authentication",
		Value: label,
		Action: "choose ↵",
		AutoFocus: true,
		Body: func(backout func()) tui.Component {
			return &chatGPTAuthModeMenu{target: w, backout: backout}
		},
	})
}

func ChatGPTPendingAuthControl(w *TargetConfig) *ui.Select {
	return ui.NewSelect(ui.SelectProps{
		ID: TargetAddMountKey(w, "auth-mode-pending"),
		Label: "authentication",
		Value: chatGPTAuthModeLabel(w),
		Action: "change ↵",
		Body: func(backout func()) tui.Component {
			return &chatGPTAuthModeMenu{target: w, backout: backout, replaceSession: true}
		},
	})
}

func (m *chatGPTAuthModeMenu) KeyMap() tui.KeyMap {
	return ui.BackScope(func() bool { return true }, m.backout)
}

func (m *chatGPTAuthModeMenu) choose(mode chatGPTAuthMode) {
	m.backout()
	if m.replaceSession {
		m.target.replaceInteractiveAuth(mode)
		return
	}
	m.target.ChatGPTAuthMode.Set(mode)
	m.target.startInteractiveAuth()
}

func ChatGPTAuthModeOption(m *chatGPTAuthModeMenu, mode chatGPTAuthMode) *ui.SelectableRow {
	action := "sign in ↵"
	if m.replaceSession { action = "switch ↵" }
	row := ui.NewSelectableRow(
		TargetAddMountKey(m.target, "auth-mode-"+mode.requestValue()),
		"",
		mode.label(),
		action,
		func() { m.choose(mode) },
	)
	row.AutoFocus = mode == m.target.ChatGPTAuthMode.Get()
	return row
}

func chatGPTAuthModeLabel(w *TargetConfig) string {
	_, label := w.interactiveAuthMode()
	if label == "" { label = "browser login" }
	return label
}

func chatGPTAuthURL(w *TargetConfig) string {
	url := ""
	if session := w.AuthSession.Get(); session.SessionID != "" { url = session.AuthorizeURL }
	return url
}

func ChatGPTAuthOpenBrowser(w *TargetConfig) *ui.SelectableRow {
	url := chatGPTAuthURL(w)
	return ui.NewSelectableRow(TargetAddMountKey(w, "auth-open"), "login URL", "", "open ↵", func() {
		_ = ui.OpenURL(url)
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
	<div class="flex-col w-full" deps={f.target.Draft, f.target.BaseURL, f.target.ChatGPTAuthMode}>
		if targetAuthPending(f.target) {
			<div deps={f.target.AuthSession, f.target.Error}>
				@ChatGPTPendingAuthControl(f.target)
				@ChatGPTAuthOpenBrowser(f.target)
				<div class="pl-3 w-full">
					@ChatGPTAuthURLText(f.target)
				</div>
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

templ (m *chatGPTAuthModeMenu) Render() {
	<div class="flex-col w-full">
		@ChatGPTAuthModeOption(m, chatGPTAuthBrowser)
		@ChatGPTAuthModeOption(m, chatGPTAuthDevice)
	</div>
}

templ ChatGPTAuthURLText(w *TargetConfig) {
	<div class="pl-2 w-full">
		@FlowText(chatGPTAuthURL(w))
	</div>
}

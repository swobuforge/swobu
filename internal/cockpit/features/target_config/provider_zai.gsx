package target_config

import (
	"strings"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/ui"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/routing"
)

type zaiProviderForm struct{ target *TargetConfig }

func ZAIProviderForm(w *TargetConfig) tui.Component { return &zaiProviderForm{target: w} }

func (w *TargetConfig) IsZAIFlow() bool {
	return profile.ProviderID(w.Draft.Get().ProviderSpec) == profile.ProviderSpecZAI
}

func zaiAccessDisplay(raw string) string {
	access, err := routing.ParseZAIAccess(raw)
	if err != nil {
		return "required"
	}
	return access.Label()
}

func ZAIAccessPicker(w *TargetConfig, backout func()) *ui.SearchPicker {
	options := routing.ZAIAccesses()
	items := make([]ui.SearchOption, 0, len(options))
	for _, option := range options {
		items = append(items, ui.SearchOption{ID: string(option), Label: option.Label()})
	}
	picker := ui.NewSearchPicker(TargetAddMountKey(w, "zai-access-picker"), "access", items, func(selection ui.Selection) {
		w.SelectZAIAccess(selection.Value)
		if backout != nil {
			backout()
		}
	}, func() {
		if backout != nil {
			backout()
		}
	})
	picker.AutoFocus = true
	return picker
}

func ZAIAccessSelect(w *TargetConfig) *ui.Select {
	raw := strings.TrimSpace(w.Draft.Get().ZAIAccess)
	action := "change ↵"
	if raw == "" {
		action = "choose ↵"
	}
	return ui.NewSelect(ui.SelectProps{
		ID:        TargetAddMountKey(w, "zai-access"),
		Label:     "access",
		Value:     zaiAccessDisplay(raw),
		Action:    action,
		AutoFocus: raw == "",
		CanEnter:  func() bool { return true },
		Body:      func(backout func()) tui.Component { return ZAIAccessPicker(w, backout) },
	})
}

templ (f *zaiProviderForm) Render() {
	<div class="flex-col w-full" deps={f.target.Draft}>
		@ZAIAccessSelect(f.target)
		if strings.TrimSpace(f.target.Draft.Get().ZAIAccess) != "" {
			<div key={credentialRegionKey(f.target)} class="w-full">@CredentialControlRegion(f.target, setupRequiresCredential(f.target))</div>
		}
	</div>
}

package target_config

import (
	tui "github.com/grindlemire/go-tui"
)

type azureProviderForm struct {
	target            *TargetConfig
	endpointCommitted bool
	endpointDraft     *tui.State[string]
}

func AzureProviderForm(w *TargetConfig) tui.Component {
	return &azureProviderForm{target: w, endpointDraft: tui.NewState(w.BaseURL.Get())}
}

func (f *azureProviderForm) BindApp(app *tui.App) { f.endpointDraft.BindApp(app) }

func (f *azureProviderForm) UpdateProps(fresh tui.Component) {
	if next, ok := fresh.(*azureProviderForm); ok {
		f.target = next.target
	}
}

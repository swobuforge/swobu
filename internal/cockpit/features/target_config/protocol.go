package target_config

import (
	"strings"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/ui"
	"github.com/swobuforge/swobu/internal/domain/endpointintent"
)

func (w *TargetConfig) SelectProtocol(protocol string) {
	protocol = strings.TrimSpace(protocol)
	for _, option := range w.CurrentProtocolOptions() {
		if option.ID != protocol { continue }
		w.Draft.Update(func(d endpointintent.TargetDraft) endpointintent.TargetDraft { d.ProviderProtocol = protocol; return d })
		w.Error.Set("")
		w.Phase.Set(PhaseReadyToCreate)
		w.CommitEdit(w.actionContext())
		return
	}
	if protocol != "" { w.Error.Set("unsupported protocol " + protocol) }
}

func (w *TargetConfig) ProtocolPickerOptions() []ui.SearchOption {
	options := w.CurrentProtocolOptions()
	opts := make([]ui.SearchOption, len(options))
	for i, option := range options { opts[i] = ui.SearchOption{ID: option.ID, Label: option.Label, Value: option.ID, Keywords: option.Keywords} }
	return opts
}

func ProtocolPicker(w *TargetConfig, backout func()) *ui.SearchPicker {
	options := w.CurrentProtocolOptions()
	opts := make([]ui.SearchOption, len(options))
	for i, option := range options { opts[i] = ui.SearchOption{ID: option.ID, Label: option.Label, Keywords: option.Keywords} }
	picker := ui.NewSearchPicker(TargetAddMountKey(w, "protocol-picker"), "protocol", opts, func(sel ui.Selection) {
		w.SelectProtocol(sel.Value)
		if backout != nil { backout() }
	}, func() { if backout != nil { backout() } })
	picker.AutoFocus = true
	return picker
}

func protocolBlockedAction(w *TargetConfig) string {
	if w.setupState().RequiresEndpoint || w.setupState().AuthModeRequired {
		if w.IsAzureFlow() { return "deployment" }
		return "model first"
	}
	if strings.TrimSpace(w.SelectedModel.Get().ModelName) == "" {
		if w.IsAzureFlow() { return "deployment" }
		return "model first"
	}
	return "blocked"
}

func ProtocolSelect(w *TargetConfig) *ui.Select {
	protocol := strings.TrimSpace(w.Draft.Get().ProviderProtocol)
	options := w.CurrentProtocolOptions()
	hydrating := w.mode == targetConfigModeEdit && w.IsAzureFlow() && len(w.Catalog.Get().Deployments) == 0
	value, action, enterable := protocol, "change ↵", true
	switch {
	case hydrating:
	case w.setupState().RequiresEndpoint || w.setupState().AuthModeRequired:
		value, action, enterable = "blocked", protocolBlockedAction(w), false
	case strings.TrimSpace(w.SelectedModel.Get().ModelName) == "":
		value, action, enterable = "blocked", protocolBlockedAction(w), false
	case len(options) == 0:
		value, action, enterable = "blocked", protocolBlockedAction(w), false
	case protocol == "":
		value, action = "required", "choose ↵"
	case len(options) == 1:
		value, action, enterable = protocol, "default", false
	}
	props := ui.SelectProps{ID: TargetAddMountKey(w, "protocol-display"), Label: "protocol", Value: value, Action: action, AutoFocus: hydrating, CanEnter: func() bool { return enterable }}
	if enterable {
		if hydrating { props.OnEnter = func() { w.ReadyAndProbe(w.Draft.Get().CredentialRef, w.BaseURL.Get()) } }
		props.Body = func(backout func()) tui.Component { return ProtocolPicker(w, backout) }
	}
	return ui.NewSelect(props)
}

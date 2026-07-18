package target_config

import (
	"strings"

	"github.com/swobuforge/swobu/internal/cockpit/ui"
)

func TargetConfigHeader(w *TargetConfig) *ui.SelectableRow {
	return ui.NewSelectableRow(
		TargetAddMountKey(w, "target-config-parent"), "", targetConfigTitle(w), targetConfigParentAction(w), w.Close,
	)
}

func CreateRetryControl(w *TargetConfig) *ui.SelectableRow {
	return ui.NewSelectableRow(
		TargetAddMountKey(w, "create-retry"), w.saveVerb(), "failed", "retry ↵", func() {
			w.Error.Set("")
			w.Phase.Set(PhaseReadyToCreate)
		},
	)
}

func DeleteControl(w *TargetConfig) *ui.SelectableRow {
	value, action := "target", "delete ↵"
	activate := func() { w.DeleteArmed.Set(true) }
	if w.DeleteArmed.Get() {
		value, action = "delete target?", "confirm ↵"
		activate = func() {
			if w.OnDeleteConfirmed == nil {
				w.Error.Set("target delete is not wired yet")
				return
			}
			if err := w.OnDeleteConfirmed(); err != nil {
				w.Error.Set(err.Error())
				return
			}
			w.Error.Set("")
			w.DeleteArmed.Set(false)
		}
	}
	return ui.NewSelectableRow(
		TargetAddMountKey(w, "delete"), "delete", value, action, activate,
	)
}

func CreateControl(w *TargetConfig) *ui.SelectableRow {
	action := w.saveVerb() + " ↵"
	activate := func() { w.Create(w.actionContext()) }
	switch {
	case !w.SetupAllowsModelChoice():
		action = firstAction(ModelBlockedAction(w))
		if w.IsAzureFlow() && action == "credential first" {
			action = "credential"
		}
		activate = func() { w.Error.Set(action) }
	case strings.TrimSpace(w.SelectedModel.Get().ModelName) == "":
		action, activate = "model first", func() { w.Error.Set("model first") }
	case strings.TrimSpace(w.Draft.Get().ProviderProtocol) == "":
		action, activate = "protocol first", func() { w.Error.Set("protocol first") }
	}
	row := ui.NewSelectableRow(TargetAddMountKey(w, "create"), w.saveVerb(), "", action, activate)
	row.AutoFocus = action == w.saveVerb()+" ↵"
	return row
}

func firstAction(action string) string {
	action = strings.TrimSpace(action)
	if strings.HasSuffix(action, "first") {
		return action
	}
	switch action {
	case "credential":
		return "credential first"
	case "auth":
		return "auth first"
	case "":
		return "model first"
	default:
		return action
	}
}

func (w *TargetConfig) saveVerb() string {
	if w.mode == targetConfigModeEdit {
		return "save"
	}
	return "create"
}

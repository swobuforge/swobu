package target_config

import (
	"strings"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/ui"
	"github.com/swobuforge/swobu/internal/profile"
)

func (w *TargetConfig) ModelPickerOptions() []ui.SearchOption {
	deployments := w.Catalog.Get().Deployments
	opts := make([]ui.SearchOption, len(deployments))
	for i, deployment := range deployments {
		opts[i] = ui.SearchOption{ID: deployment.ID, Label: deployment.Name, Value: deployment.ID}
	}
	return opts
}

func TargetModelLabel(w *TargetConfig) string {
	return "model"
}

func (w *TargetConfig) tryProbeModelCatalog() {
	if len(w.Catalog.Get().Deployments) == 0 && w.Phase.Get() != PhaseCatalogFailed {
		setup := w.refreshSetup()
		if setup.ReadyForCatalog {
			w.ReadyAndProbe(setup.CredentialRef, w.BaseURL.Get())
		}
	}
}

func (w *TargetConfig) SetupAllowsModelChoice() bool {
	setup := w.setupState()
	if setup.InteractiveAuth && strings.TrimSpace(w.Draft.Get().CredentialRef) == "" {
		return false
	}
	return setup.ReadyForCatalog || w.CatalogLoading.Get() || len(w.Catalog.Get().Deployments) > 0 || w.Phase.Get() == PhaseCatalogFailed
}

func ModelBlockedAction(w *TargetConfig) string {
	setup := w.setupState()
	if setup.RequiresEndpoint {
		switch setup.EndpointKind {
		case profile.EndpointAzureResourceLocator:
			return "project first"
		case profile.EndpointRequiredHTTPBaseURL:
			switch profile.ProviderID(setup.ProviderSpec) {
			case profile.ProviderSpecBedrock:
				return "region first"
			case profile.ProviderSpecOpenAICompatible:
				return "backend first"
			default:
				return "endpoint first"
			}
		case profile.EndpointDefaultHTTPBaseURL:
			return "endpoint first"
		default:
			if label := strings.TrimSpace(setup.EndpointLabel); label != "" {
				return label + " first"
			}
			return "endpoint first"
		}
	}
	if setup.InteractiveAuth || setup.AuthModeRequired {
		return "auth first"
	}
	if strings.TrimSpace(setup.BlockReason) == "profile first" {
		return "profile first"
	}
	if setup.CredentialRequired && strings.TrimSpace(setup.CredentialRef) == "" {
		return "credential"
	}
	return "setup first"
}

func (w *TargetConfig) SelectModelByID(id string) {
	for _, deployment := range w.Catalog.Get().Deployments {
		if deployment.ID == id {
			w.SelectModel(deployment)
			return
		}
	}
	w.SelectModel(readmodel.ModelDeploymentReadModel{ID: id, Name: id, ModelName: id})
}

func ModelPicker(w *TargetConfig, backout func()) *ui.SearchPicker {
	catalog := w.Catalog.Get()
	opts := make([]ui.SearchOption, 0, len(catalog.Deployments))
	for _, deployment := range catalog.Deployments {
		opts = append(opts, ui.SearchOption{ID: deployment.ID, Label: deployment.Name})
	}
	picker := ui.NewSearchPicker(TargetAddMountKey(w, "model-picker"), "model", opts, func(sel ui.Selection) {
		w.SelectModelByID(sel.Value)
		if backout != nil {
			backout()
		}
	}, func() {
		if backout != nil {
			backout()
		}
	})
	picker.AutoFocus, picker.Mode = true, ui.SearchPickerOpen
	return picker
}

func ModelCatalogRetry(w *TargetConfig) *ui.SelectableRow {
	value := "catalog failed"
	switch strings.ToLower(strings.TrimSpace(w.Error.Get())) {
	case "project not found":
		value = "project not found"
	case "unauthorized":
		value = "unauthorized"
	case "none found":
		value = "none found"
	}
	row := ui.NewSelectableRow(TargetAddMountKey(w, "catalog-retry"), TargetModelLabel(w), value, "retry ↵", w.RetryCatalog)
	row.AutoFocus = true
	return row
}

func ModelCatalogLoading(w *TargetConfig) *ui.SelectableRow {
	row := ui.NewSelectableRow(TargetAddMountKey(w, "loading-catalog"), "model", "loading catalog…", "wait", nil)
	row.AutoFocus = true
	return row
}

func ModelSelectRow(w *TargetConfig) *ui.Select {
	model := strings.TrimSpace(w.SelectedModel.Get().ModelName)
	setup := w.setupState()
	value, action := model, "change ↵"
	if setup.RequiresEndpoint || setup.AuthModeRequired || !w.SetupAllowsModelChoice() {
		value, action = "blocked", ModelBlockedAction(w)
	} else if value == "" {
		value, action = "required", "choose ↵"
	}
	return ui.NewSelect(ui.SelectProps{
		ID: TargetAddMountKey(w, "model-display"), Label: TargetModelLabel(w), Value: value, Action: action,
		AutoFocus: model == "" && w.SetupAllowsModelChoice(),
		CanEnter:  func() bool { return w.SetupAllowsModelChoice() },
		OnEnter:   func() { w.tryProbeModelCatalog() },
		Body:      func(backout func()) tui.Component { return ModelPicker(w, backout) },
	})
}

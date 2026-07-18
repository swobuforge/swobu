package target_config

import tui "github.com/grindlemire/go-tui"

// targetTail is the provider-form tail component. Its visible hierarchy lives
// in this GSX source.
type targetTail struct{ root *TargetConfig }

func TargetConfigTail(w *TargetConfig) tui.Component { return &targetTail{root: w} }

templ (t *targetTail) Render() {
	<div class="flex-col w-full">
		if t.root.CatalogLoading.Get() || t.root.Phase.Get() == PhaseLoadingCatalog {
			@ModelCatalogLoading(t.root)
		} else if t.root.Phase.Get() == PhaseCatalogFailed {
			@ModelCatalogRetry(t.root)
		} else {
			@ModelSelectRow(t.root)
		}

		@ProtocolSelect(t.root)

		if t.root.mode != targetConfigModeEdit {
			if t.root.CanChangePlacement() {
				@PlacementSelect(t.root)
			}
		}

		if t.root.mode == targetConfigModeEdit {
			@DeleteControl(t.root)
		} else if t.root.Phase.Get() == PhaseCreateFailed {
			@CreateRetryControl(t.root)
		} else {
			@CreateControl(t.root)
		}
	</div>
}

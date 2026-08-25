package workspace_connect

import (
	ui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/clientconnect"
	cockpitui "github.com/swobuforge/swobu/internal/cockpit/ui"
)

type FlowTextView = cockpitui.FlowTextView

func FlowText(text string) *FlowTextView {
	return cockpitui.FlowText(text)
}

func EndpointRow(d *Disclosure) *cockpitui.SelectableRow {
	return cockpitui.NewSelectableRow("workspace-connect:endpoint:"+d.Target.WorkspaceSlug(), "endpoint", d.Target.WorkspaceURL(), d.endpointAction(), d.toggleEndpoint)
}

func OtherClientsRow(d *Disclosure) *cockpitui.SelectableRow {
	return d.rowEscape(cockpitui.NewSelectableRow("workspace-connect:other-clients", "Other clients", "", "setup ↵", d.openManualSetup))
}

func OtherClientsHeaderRow(d *Disclosure) *cockpitui.SelectableRow {
	return d.rowEscape(cockpitui.NewSelectableRow("workspace-connect:other-clients", "Other clients", "", "close ↵", d.closeChildScope))
}

func ClientHeaderRow(d *Disclosure, id clientconnect.ClientID, name string) *cockpitui.SelectableRow {
	return d.rowEscape(cockpitui.NewSelectableRow("workspace-connect:client:"+string(id), name, "", "close ↵", d.closeChildScope))
}

func CheckingClientRow(d *Disclosure, obs clientObservation) *cockpitui.SelectableRow {
	return d.rowEscape(cockpitui.NewSelectableRow("workspace-connect:client:"+string(obs.Client.ID), obs.Client.Name, "", "checking…", func() { d.chooseClient(obs.Client.ID) }))
}

func ConfiguredClientRow(d *Disclosure, obs clientObservation) *cockpitui.SelectableRow {
	return d.rowEscape(cockpitui.NewSelectableRow("workspace-connect:client:"+string(obs.Client.ID), obs.Client.Name, "", "configured ↵", func() { d.chooseClient(obs.Client.ID) }))
}

func NeedsChangeClientRow(d *Disclosure, obs clientObservation) *cockpitui.SelectableRow {
	if obs.Applying {
		return d.rowEscape(cockpitui.NewSelectableRow("workspace-connect:client:"+string(obs.Client.ID), obs.Client.Name, "", "configuring…", func() { d.chooseClient(obs.Client.ID) }))
	}
	return d.rowEscape(cockpitui.NewSelectableRow("workspace-connect:client:"+string(obs.Client.ID), obs.Client.Name, "", "configure ↵", func() { d.chooseClient(obs.Client.ID) }))
}

func FailedClientRow(d *Disclosure, obs clientObservation) *cockpitui.SelectableRow {
	return d.rowEscape(cockpitui.NewSelectableRow("workspace-connect:client:"+string(obs.Client.ID), obs.Client.Name, "", "retry ↵", func() { d.chooseClient(obs.Client.ID) }))
}

func PlanActionRow(d *Disclosure, obs clientObservation) *cockpitui.SelectableRow {
	if obs.Applying {
		return d.rowEscape(cockpitui.NewSelectableRow("workspace-connect:apply:"+string(obs.Client.ID), "config", shortLocus(obs.Plan.ConfigPath), "configuring…", nil))
	}
	action := "apply ↵"
	if obs.Plan.RequiresReplace() {
		action = "replace ↵"
	}
	return d.rowEscape(cockpitui.NewSelectableRow("workspace-connect:apply:"+string(obs.Client.ID), "config", shortLocus(obs.Plan.ConfigPath), action, func() { d.applyPlan(obs.Client.ID) }))
}

func ManualCopyRow(d *Disclosure, key, label, displayValue, copyValue string) *cockpitui.SelectableRow {
	return d.rowEscape(cockpitui.NewSelectableRow("workspace-connect:manual:"+key, label, displayValue, d.copyAction(key), func() { d.copyItem(key, copyValue) }))
}

templ FindingClientsRow() {
	<div class="flex-row w-full">
		<span class="w-2"></span><span class="grow truncate nowrap" minWidth={0}>finding installed clients…</span><span class="w-14">wait</span>
	</div>
}

templ CheckingConfigRow() {
	<div class="flex-row w-full">
		<span class="w-2"></span><span class="grow truncate nowrap" minWidth={0}>checking configuration…</span><span class="w-14">wait</span>
	</div>
}

templ InertRow(label string, value string) {
	<div class="flex-row w-full">
		<span class="w-2"></span><span class="w-18">{label}</span><span class="grow truncate nowrap" minWidth={0}>{value}</span>
	</div>
}

templ PlanChangeRow(field string, value string) {
	<div class="flex-col w-full">
		<div class="flex-row w-full">
			<span class="w-2"></span><span class="w-18">{field}</span>
		</div>
		<div class="pl-20 w-full">
			@FlowText(value)
		</div>
	</div>
}

templ DetailRow(value string) {
	<div class="pl-20 w-full">
		@FlowText(value)
	</div>
}

templ (d *Disclosure) Render() {
	<div class="flex-col w-full" deps={d.EndpointOpen, d.DiscoveryPending, d.Observations, d.Child, d.Feedback}>
		@EndpointRow(d)
		if !d.EndpointOpen.Get() {
			<div class="flex-row w-full">
				<span class="w-2"></span><span class="w-18"></span><span class="grow">OpenAI · Anthropic</span>
			</div>
		}
		if d.EndpointOpen.Get() {
			<div class="pl-3 flex-col w-full">
				if d.DiscoveryPending.Get() {
					@FindingClientsRow()
				}
				for _, obs := range d.Observations.Get() {
					if d.Child.Get().isClient(obs.Client.ID) {
						@ClientHeaderRow(d, obs.Client.ID, obs.Client.Name)
						<div class="pl-3 flex-col w-full">
							if obs.Kind == observationChecking {
								@CheckingConfigRow()
							} else if obs.Kind == observationMatch {
								@InertRow("status", "current")
							} else if obs.Kind == observationFailed {
								@DetailRow(obs.Err)
							} else if obs.Kind == observationNeedsChange {
								for _, change := range obs.Plan.Changes {
									@PlanChangeRow(change.Field, displayChange(d.Target, change))
								}
								@PlanActionRow(d, obs)
								if obs.Err != "" {
									@DetailRow(obs.Err)
								}
							}
						</div>
					} else {
						if obs.Kind == observationChecking {
							@CheckingClientRow(d, obs)
						} else if obs.Kind == observationMatch {
							@ConfiguredClientRow(d, obs)
						} else if obs.Kind == observationNeedsChange {
							@NeedsChangeClientRow(d, obs)
						} else if obs.Kind == observationFailed {
							@FailedClientRow(d, obs)
						}
					}
				}
				if d.Child.Get().isManual() {
					@OtherClientsHeaderRow(d)
					<div class="pl-3 flex-col w-full">
						@InertRow("API", "OpenAI · Anthropic")
						@ManualCopyRow(d, "base-url", "Base URL", d.Target.WorkspaceURL(), d.Target.WorkspaceURL())
						if d.Feedback.Get().key == "base-url" && d.Feedback.Get().result.Status == cockpitui.CopySavedFile && d.Feedback.Get().result.Path != "" {
							@DetailRow(d.Feedback.Get().result.Path)
						}
						@ManualCopyRow(d, "model", "Model", "default", "default")
						if d.Feedback.Get().key == "model" && d.Feedback.Get().result.Status == cockpitui.CopySavedFile && d.Feedback.Get().result.Path != "" {
							@DetailRow(d.Feedback.Get().result.Path)
						}
						@ManualCopyRow(d, "models-url", "Models URL", d.Target.WorkspaceURL()+"/models", d.Target.WorkspaceURL()+"/models")
						if d.Feedback.Get().key == "models-url" && d.Feedback.Get().result.Status == cockpitui.CopySavedFile && d.Feedback.Get().result.Path != "" {
							@DetailRow(d.Feedback.Get().result.Path)
						}
						@ManualCopyRow(d, "api-key", "API key", "swobu · placeholder", "swobu")
						if d.Feedback.Get().key == "api-key" && d.Feedback.Get().result.Status == cockpitui.CopySavedFile && d.Feedback.Get().result.Path != "" {
							@DetailRow(d.Feedback.Get().result.Path)
						}
						if d.Feedback.Get().result.Status == cockpitui.CopyFailed {
							@DetailRow("copy failed · run swobu doctor --copy")
						}
					</div>
				} else {
					@OtherClientsRow(d)
				}
			</div>
		}
	</div>
}

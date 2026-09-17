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

func PlanActionRow(d *Disclosure, obs clientObservation) *cockpitui.SelectableRow {
	if obs.Applying {
		return d.rowEscape(cockpitui.NewSelectableRow("workspace-connect:apply:"+string(obs.Client.ID), "config", shortLoci(obs.Plan.ConfigPaths), "configuring…", nil))
	}
	action := "apply ↵"
	if obs.Plan.RequiresReplace() {
		action = "replace ↵"
	}
	return d.rowEscape(cockpitui.NewSelectableRow("workspace-connect:apply:"+string(obs.Client.ID), "config", shortLoci(obs.Plan.ConfigPaths), action, func() { d.applyPlan(obs.Client.ID) }))
}

func ManualCopyRow(d *Disclosure, key, label, displayValue, copyValue string, allowFileFallback bool) *cockpitui.SelectableRow {
	return d.rowEscape(cockpitui.NewSelectableRow("workspace-connect:manual:"+key, label, displayValue, d.copyAction(key), func() { d.copyItem(key, copyValue, allowFileFallback) }))
}

func ClientPicker(d *Disclosure) *cockpitui.SearchPicker {
	observations := d.Observations.Get()
	options := make([]cockpitui.SearchOption, 0, len(observations))
	for _, obs := range observations {
		action := "checking…"
		switch obs.Kind {
		case observationMatch:
			action = "configured ↵"
		case observationNeedsChange:
			action = "configure ↵"
			if obs.Applying { action = "configuring…" }
		case observationFailed:
			action = "retry ↵"
		}
		options = append(options, cockpitui.SearchOption{ID: string(obs.Client.ID), Label: obs.Client.Name, Keywords: []string{string(obs.Client.ID)}, Action: action})
	}
	picker := cockpitui.NewSearchPicker("workspace-connect:clients", "client", options, func(sel cockpitui.Selection) { d.chooseClient(clientconnect.ClientID(sel.Value)) }, func() { d.Back() })
	return picker
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

templ DangerDetailRow(value string) {
	<div class="pl-20 w-full">
		@FlowText(value)
	</div>
}

templ (d *Disclosure) Render() {
	<div class="flex-col w-full" deps={d.EndpointOpen, d.DiscoveryPending, d.Observations, d.Child, d.Feedback, d.ApplyFeedback}>
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
				if d.Child.Get().kind == childNone && !d.DiscoveryPending.Get() {
					@ClientPicker(d)
					if d.ApplyFeedback.Get() != "" { @DetailRow(d.ApplyFeedback.Get()) }
				}
				for _, obs := range d.Observations.Get() {
					if d.Child.Get().isClient(obs.Client.ID) {
						@ClientHeaderRow(d, obs.Client.ID, obs.Client.Name)
						<div class="pl-3 flex-col w-full">
							if obs.Kind == observationChecking {
								@CheckingConfigRow()
							} else if obs.Kind == observationMatch {
								@InertRow("status", "current")
								if obs.Err != "" { @DetailRow(obs.Err) }
							} else if obs.Kind == observationFailed {
								@DangerDetailRow(obs.Err)
							} else if obs.Kind == observationNeedsChange {
								for _, change := range obs.Plan.Changes { @PlanChangeRow(change.Field, displayChange(d.Target, change)) }
								@PlanActionRow(d, obs)
								if obs.Err != "" { @DetailRow(obs.Err) }
							}
						</div>
					}
				}
				if d.Child.Get().isManual() {
					@OtherClientsHeaderRow(d)
					<div class="pl-3 flex-col w-full">
						@InertRow("API", "OpenAI · Anthropic")
						@ManualCopyRow(d, "base-url", "Base URL", d.Target.WorkspaceURL(), d.Target.WorkspaceURL(), true)
						if d.Feedback.Get().key == "base-url" && d.Feedback.Get().savedPath != "" {
							@DetailRow("saved "+d.Feedback.Get().savedPath)
						}
						@ManualCopyRow(d, "model", "Model", "default", "default", true)
						if d.Feedback.Get().key == "model" && d.Feedback.Get().savedPath != "" {
							@DetailRow("saved "+d.Feedback.Get().savedPath)
						}
						@ManualCopyRow(d, "models-url", "Models URL", d.Target.WorkspaceURL()+"/models", d.Target.WorkspaceURL()+"/models", true)
						if d.Feedback.Get().key == "models-url" && d.Feedback.Get().savedPath != "" {
							@DetailRow("saved "+d.Feedback.Get().savedPath)
						}
						@ManualCopyRow(d, "api-key", "API key", "swobu · placeholder", "swobu", false)
						if d.Feedback.Get().saveErr != nil {
							@DangerDetailRow("copy failed · "+d.Feedback.Get().saveErr.Error())
						} else if d.Feedback.Get().clipboard.Status == cockpitui.CopyFailed && d.Feedback.Get().savedPath == "" {
							@DangerDetailRow("copy failed · run swobu doctor --copy")
						}
						if d.Feedback.Get().clipboard.Status == cockpitui.CopyUnavailable && d.Feedback.Get().savedPath == "" {
							@DetailRow("clipboard unavailable")
						}
					</div>
				} else if d.Child.Get().kind == childNone {
					@OtherClientsRow(d)
				}
			</div>
		}
	</div>
}

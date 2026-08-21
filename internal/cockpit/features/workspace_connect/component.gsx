package workspace_connect

import (
	ui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/clientconnect"
	cockpitui "github.com/swobuforge/swobu/internal/cockpit/ui"
)

func EndpointRow(d *Disclosure) *cockpitui.SelectableRow {
	return cockpitui.NewSelectableRow("workspace-connect:endpoint:"+d.Target.WorkspaceSlug(), "endpoint", d.Target.WorkspaceURL(), d.endpointAction(), d.toggleEndpoint)
}

func ClientRow(d *Disclosure, client clientconnect.Client) *cockpitui.SelectableRow {
	return d.rowEscape(cockpitui.NewSelectableRow("workspace-connect:client:"+string(client.ID), client.Name, "", d.clientAction(client), func() { d.chooseClient(client) }))
}

func OtherClientsRow(d *Disclosure) *cockpitui.SelectableRow {
	return d.rowEscape(cockpitui.NewSelectableRow("workspace-connect:other-clients", "Other clients", "", "setup \u21b5", d.openManualSetup))
}

func PlanHeaderRow(d *Disclosure, plan clientconnect.Plan) *cockpitui.SelectableRow {
	return d.rowEscape(cockpitui.NewSelectableRow("workspace-connect:client:"+string(plan.ClientID), plan.ClientName, "", "close \u21b5", d.closeChildScope))
}

func OtherClientsHeaderRow(d *Disclosure) *cockpitui.SelectableRow {
	return d.rowEscape(cockpitui.NewSelectableRow("workspace-connect:other-clients", "Other clients", "", "close \u21b5", d.closeChildScope))
}

func PlanActionRow(d *Disclosure, plan clientconnect.Plan) *cockpitui.SelectableRow {
	action := "apply \u21b5"
	if plan.RequiresReplace() {
		action = "replace \u21b5"
	}
	return d.rowEscape(cockpitui.NewSelectableRow("workspace-connect:apply:"+string(plan.ClientID), "config", shortLocus(plan.ConfigPath), action, d.applyPlan))
}

func ManualCopyRow(d *Disclosure, key, label, displayValue, copyValue string) *cockpitui.SelectableRow {
	return d.rowEscape(cockpitui.NewSelectableRow("workspace-connect:manual:"+key, label, displayValue, d.copyAction(key), func() { d.copyItem(key, copyValue) }))
}

templ InertRow(label string, value string) {
	<div class="flex-row w-full">
		<span class="w-2"></span><span class="w-18">{label}</span><span>{value}</span>
	</div>
}

templ DetailRow(value string) {
	<div class="pl-20 w-full"><span>{value}</span></div>
}

templ ConfiguredClientRow(client clientconnect.Client) {
	<div class="flex-row w-full">
		<span class="w-2"></span><span class="w-18">{client.Name}</span><span class="grow"></span><span class="w-14">configured</span>
	</div>
}

templ (d *Disclosure) Render() {
	<div class="flex-col w-full" deps={d.EndpointOpen, d.Clients, d.Child, d.Feedback, d.Error}>
		@EndpointRow(d)
		if !d.EndpointOpen.Get() {
			<div class="pl-20 w-full"><span>OpenAI · Anthropic</span></div>
		}
		if d.EndpointOpen.Get() {
			<div class="pl-3 flex-col w-full">
				for _, client := range d.Clients.Get() {
					if d.Child.Get().hasPlan(client.ID) {
						@PlanHeaderRow(d, d.Child.Get().plan)
						<div class="pl-3 flex-col w-full">
							for _, change := range d.Child.Get().plan.Changes {
								@InertRow(change.Field, displayChange(d.Target, change))
							}
							@PlanActionRow(d, d.Child.Get().plan)
							if d.Error.Get() != "" {
								@DetailRow(d.Error.Get())
							}
						</div>
					} else if client.Configured {
						@ConfiguredClientRow(client)
					} else {
						@ClientRow(d, client)
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
						if d.Error.Get() != "" {
							@DetailRow(d.Error.Get())
						}
					</div>
				} else {
					@OtherClientsRow(d)
				}
				if d.Error.Get() != "" && d.Child.Get().kind == childNone {
					@DetailRow(d.Error.Get())
				}
			</div>
		}
	</div>
}

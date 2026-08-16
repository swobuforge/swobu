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
	return d.rowEscape(cockpitui.NewSelectableRow("workspace-connect:client:"+string(client.ID), client.Name, "", d.clientAction(client), func(){ d.chooseClient(client) }))
}

func OtherClientsRow(d *Disclosure) *cockpitui.SelectableRow {
	return d.rowEscape(cockpitui.NewSelectableRow("workspace-connect:other-clients", "Other clients", "", "copy ↵", d.copyWorkspaceURL))
}

func PlanActionRow(d *Disclosure, plan clientconnect.Plan) *cockpitui.SelectableRow {
	action := "apply ↵"
	if plan.RequiresReplace() { action = "replace ↵" }
	return d.rowEscape(cockpitui.NewSelectableRow("workspace-connect:apply:"+string(plan.ClientID), "writes", shortLocus(plan.ConfigPath), action, d.applyPlan))
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
	<div class="flex-col w-full" deps={d.EndpointOpen, d.Clients, d.Plan, d.Error}>
		@EndpointRow(d)
		<div class="pl-20 w-full"><span>OpenAI · Anthropic</span></div>
		if d.EndpointOpen.Get() {
			<div class="pl-3 flex-col w-full">
				for _, client := range d.Clients.Get() {
					if client.Configured {
						@ConfiguredClientRow(client)
					} else {
						@ClientRow(d, client)
					}
					if !client.Configured && d.Plan.Get().ClientID == client.ID {
						<div class="pl-3 flex-col w-full">
							for _, change := range d.Plan.Get().Changes {
								@InertRow(change.Field, displayChange(d.Plan.Get().Target, change))
							}
							@PlanActionRow(d, d.Plan.Get())
						</div>
					}
				}
				@OtherClientsRow(d)
				if d.Error.Get() != "" {
					@DetailRow(d.Error.Get())
				}
			</div>
		}
	</div>
}

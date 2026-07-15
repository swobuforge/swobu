package routes

import (
	"fmt"

	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
)

templ Section(model readmodel.WorkspaceReadModel) {
	<div class="flex-col w-full">
		@SectionHeader("routes", model.View.RoutesExpanded)
		if model.View.RoutesExpanded {
			if len(model.Routes) == 0 {
				@ContentRow("(no routes)", "", "", false)
			} else {
				for _, route := range model.Routes {
					@ContentRow(route.ModelName, route.RowValue(), "open ↵", model.View.FocusedRouteID == route.ID)
					if model.View.ExpandedRouteID == route.ID {
						for i, target := range route.Targets {
							@TargetDetail(target)
							if i < len(route.Targets)-1 {
								<br />
							}
						}
						<br />
					}
				}
				@ContentRow("add route", "", "add ↵", false)
			}
		}
	</div>
}

templ SectionHeader(label string, expanded bool) {
	<div class="flex-row">
		<span class="w-2"></span>
		if expanded {
			<span>{label + " ▾"}</span>
		} else {
			<span>{label + " ▸"}</span>
		}
	</div>
}

templ ContentRow(label string, value string, action string, focused bool) {
	<div class="flex-row w-full">
		if focused {
			<span class="w-5">{">"}</span>
		} else {
			<span class="w-5"></span>
		}
		<span class="w-18">{label}</span>
		<span class="w-36">{value}</span>
		<span>{action}</span>
	</div>
}

templ TargetDetail(target readmodel.TargetReadModel) {
	<div class="flex-col w-full">
		@DetailRow("name", target.Name)
		@DetailRow("provider", target.Provider)
		@DetailRow("model", target.Model)
		@DetailRow("base URL", target.BaseURL)
		@DetailRow("credential", target.CredentialRef)
		@DetailRow("rank", fmt.Sprint(target.Rank))
		@DetailRow("weight", fmt.Sprint(target.Weight))
	</div>
}

templ DetailRow(label string, value string) {
	<div class="flex-row w-full">
		<span class="w-8"></span>
		<span class="w-15">{label}</span>
		<span>{value}</span>
	</div>
}

package workspace

import "github.com/swobuforge/swobu/internal/cockpit/readmodel"

templ Section(model readmodel.WorkspaceReadModel) {
	<div class="flex-col w-full">
		@SectionHeader("workspace", model.View.WorkspaceExpanded)
		if model.View.WorkspaceExpanded {
			if model.IsDraft() {
				@ContentRow("slug", "", "create ↵")
				@ContentRow("client base URL", "(derived from slug)", "")
			} else {
				@ContentRow("client base URL", model.ClientBaseURL, "copy ↵")
				if !model.View.WorkspaceSummaryOnly {
					if len(model.RunCommands) > 0 {
						@ContentRow("run once", model.RunCommands[0].Label, "open ↵")
					}
					@ContentRow("edit workspace", "", "open ↵")
					if model.View.DeleteWorkspaceConfirm {
						@ContentRow("delete workspace " + confirmationSlug(model) + "?", "", "y/n")
					}
				}
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

templ ContentRow(label string, value string, action string) {
	<div class="flex-row w-full">
		<span class="w-5"></span>
		<span class="w-18">{label}</span>
		<span class="w-36">{value}</span>
		<span>{action}</span>
	</div>
}

func confirmationSlug(model readmodel.WorkspaceReadModel) string {
	if model.View.WorkspaceConfirmationID != "" {
		return string(model.View.WorkspaceConfirmationID)
	}
	return model.Slug
}

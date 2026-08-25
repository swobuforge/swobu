package workspace_edit

import (
	"github.com/swobuforge/swobu/internal/cockpit/ui"
)

type FlowTextView = ui.FlowTextView

func FlowText(text string) *FlowTextView {
	return ui.FlowText(text)
}

templ (w *Workflow) Render() {
	<div class="flex-col w-full">
		@RowComponent(w)
		if w.Workspace.IsDraft() {
			@EndpointPreviewRow(w.WorkspaceURLPreview())
		}
	</div>
}

templ EndpointPreviewRow(value string) {
	<div class="flex-col w-full">
		<div class="flex-row w-full">
			<span class="w-2"></span>
			<span class="w-18">endpoint</span>
			<span class="grow"></span>
			<span class="w-14">preview</span>
		</div>
		<div class="pl-20 w-full">
			@FlowText(value)
		</div>
	</div>
}

package workspace_edit

templ (w *Workflow) Render() {
	<div class="flex-col w-full">
		@RowComponent(w)
		if w.Workspace.IsDraft() {
			@EndpointPreviewRow(w.WorkspaceURLPreview())
		}
	</div>
}

templ EndpointPreviewRow(value string) {
	<div class="flex-row w-full">
		<span class="w-2"></span>
		<span class="w-18">endpoint</span>
		<span class="w-32">{value}</span>
		<span>preview</span>
	</div>
}

package workspace_edit

templ (w *Workflow) Render() {
	<div class="flex-col w-full">
		@RowComponent(w)
	</div>
}

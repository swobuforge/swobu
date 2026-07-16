package route_edit

templ (w *Workflow) Render() {
	<div class="flex-col w-full" deps={w.Phase, w.ModelName, w.Error}>
		@ModelRowComponent(w)
		@DefaultRowComponent(w)
		@DeleteRowComponent(w)
		if w.Error.Get() != "" {
			<div class="flex-row w-full">
				<span>{w.Error.Get()}</span>
			</div>
		}
	</div>
}

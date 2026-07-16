package run_once

templ (w *Workflow) Render() {
	<div class="flex-col w-full" deps={w.Selected, w.Picker, w.Phase, w.Message}>
		<div class="flex-row w-full">
			<span>{w.Title()}</span>
		</div>
		@ModelRowComponent(w)
		if w.IsPickerOpen() {
			for i, route := range w.Routes {
				@ModelOptionRowComponent(w, i, route)
			}
		}
		<div class="flex-row w-full focusable" onActivate={w.ActivateRun}>
			<span class="w-15">command</span>
			<span class="w-35">{w.CommandValue()}</span>
			<span>{w.RunActionLabel()}</span>
		</div>
		if w.Message.Get() != "" {
			<div class="flex-row w-full">
				<span>{w.Message.Get()}</span>
			</div>
		}
	</div>
}

package run_once

templ (w *Workflow) Render() {
	<div class="flex-col w-full" deps={w.Selected, w.Phase, w.Message}>
		<div class="flex-row w-full">
			<span class="w-5"></span>
			<span>{w.Title()}</span>
		</div>
		<div class="flex-row w-full focusable" onActivate={w.ChangeModel}>
			<span class="w-8"></span>
			<span class="w-15">model</span>
			<span class="w-36">{w.ModelValue()}</span>
			<span>change ↵</span>
		</div>
		<div class="flex-row w-full focusable" onActivate={w.ActivateRun}>
			<span class="w-8"></span>
			<span class="w-15">command</span>
			<span class="w-36">{w.CommandValue()}</span>
			<span>{w.RunActionLabel()}</span>
		</div>
		if w.StatusMessage() != "" {
			<div class="flex-row w-full">
				<span class="w-8"></span>
				<span>{w.StatusMessage()}</span>
			</div>
		}
	</div>
}

package route_edit

import tui "github.com/grindlemire/go-tui"

templ (w *Workflow) Render() {
	<div class="flex-col w-full" deps={w.Phase, w.ModelName, w.Error}>
		<div class="flex-row w-full focusable" onActivate={w.ActivateName}>
			<span class="w-8"></span>
			<span class="w-15">model</span>
			if w.IsEditing() {
				if app != nil {
					<input value={w.ModelName} width={30} border={tui.BorderRounded} />
				} else {
					<span class="w-30">{w.ModelName.Get()}</span>
				}
			} else {
				<span class="w-30">{w.Route.ModelName}</span>
			}
			<span>{w.ActionLabel()}</span>
		</div>
		<div class="flex-row w-full focusable" onActivate={w.ActivateDefault}>
			<span class="w-8"></span>
			<span class="w-15">default</span>
			<span class="w-30">{w.DefaultValueLabel()}</span>
			<span>{w.DefaultActionLabel()}</span>
		</div>
		<div class="flex-row w-full focusable" onActivate={w.ActivateDelete}>
			<span class="w-8"></span>
			<span class="w-15">delete</span>
			<span class="w-30">{w.DeleteValueLabel()}</span>
			<span>{w.DeleteActionLabel()}</span>
		</div>
		if w.Error.Get() != "" {
			<div class="flex-row w-full">
				<span class="w-8"></span>
				<span>{w.Error.Get()}</span>
			</div>
		}
	</div>
}

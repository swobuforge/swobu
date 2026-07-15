package workspace_edit

import tui "github.com/grindlemire/go-tui"

templ (w *Workflow) Render() {
	<div class="flex-col w-full" deps={w.Phase, w.Mode, w.Slug, w.Error}>
		if w.IsEditing() {
			<div class="flex-row w-full">
				<span class="w-5"></span>
				<span class="w-18">slug</span>
				if app != nil {
					<input value={w.Slug} autoFocus width={30} border={tui.BorderRounded} />
				} else {
					<span class="w-30">{w.ValueLabel()}</span>
				}
				<span>{w.ActionLabel()}</span>
			</div>
		} else {
			<div class="flex-row w-full focusable" onActivate={w.Activate}>
				<span class="w-5"></span>
				<span class="w-18">slug</span>
				<span class="w-30">{w.ValueLabel()}</span>
				<span>{w.ActionLabel()}</span>
			</div>
		}
		if w.ErrorMessage() != "" {
			<div class="flex-row w-full">
				<span class="w-9"></span>
				<span>{w.ErrorMessage()}</span>
			</div>
		}
	</div>
}

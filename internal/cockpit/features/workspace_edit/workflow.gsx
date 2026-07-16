package workspace_edit

templ (w *Workflow) Render() {
	<div class="flex-col w-full" deps={w.Phase, w.Mode, w.Slug, w.Error, w.FocusedState()}>
		if w.IsEditing() {
			<div class="flex-row w-full">
				<span class="w-5">{w.Arrow()}</span>
				<span class="w-18">slug</span>
				<input value={w.Slug} autoFocus onSubmit={w.SubmitSlug} width={35} />
				<span class="w-1"></span>
				<span>{w.ActionLabel()}</span>
			</div>
		} else {
			<div class="flex-row w-full focusable" onActivate={w.Activate} onFocus={w.OnFocus} onBlur={w.OnBlur}>
				<span class="w-5">{w.Arrow()}</span>
				<span class="w-18">slug</span>
				<span class="w-35">{w.ValueLabel()}</span>
				<span class="w-1"></span>
				<span>{w.ActionLabel()}</span>
			</div>
		}
		if w.visibleError() != "" {
			<div class="flex-row w-full">
				<span class="w-9"></span>
				<span>{w.visibleError()}</span>
			</div>
		}
	</div>
}

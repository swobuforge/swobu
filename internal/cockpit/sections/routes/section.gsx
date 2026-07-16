package routes

// ---------------------------------------------------------------------------
// Section render
// ---------------------------------------------------------------------------

templ (s *SectionView) Render() {
	<div class="flex-col w-full">
		<div key={sectionHeaderKey(s)} class="w-full">
			@SectionHeaderComponent(s)
		</div>
		if s.Expanded.Get() {
			// --- Routes list -----------------------------------------------
			if len(s.State.Routes) == 0 {
				@SectionInertRow("(no routes)", "", "")
			} else {
				for _, route := range s.State.Routes {
					// --- Route row --------------------------------------------
					<div key={routeMountKey(route)} class="w-full">
						@RouteRowComponent(s, route)
					</div>
					if s.isExpanded(route) {
						// --- Route detail rows (name, default, delete) --------
						detail := RouteDetailRowComponent(s, route)
						<div key={s.routeDetailRowKey(route)} class="w-full">
							@detail
						</div>
						// --- Contract row -------------------------------------
						@ContractRow("client sends", contractRowValue(route))
						// --- Target rows by step ------------------------------
						for stepIdx, stepTargets := range groupedTargets(route) {
							@StepHeaderRow(stepHeaderText(stepIdx+1, len(stepTargets) > 1))
							for _, target := range stepTargets {
								<div key={targetMountKey(route, target)} class="w-full">
									@TargetRowComponent(s, route, target)
								</div>
								if s.State.OpenTarget.Get() == target.ID {
									row := TargetStringRowComponent(s, route, target)
									<div key={s.targetStringRowKey(route, target)} class="w-full">
										@row
									</div>
								}
								// --- Delete confirmation for a target --------
								if s.State.DeleteConfirmTarget.Get() == target.ID {
									<div key={"del:" + string(target.ID)} class="w-full">
										@TargetDeleteConfirmRow(s, route, target)
									</div>
								}
							}
						}
						// --- Add target trigger / workflow -------------------
						if s.State.AddTargetRoute.Get() == route.ID {
							wf := s.targetAddWorkflow(route)
							<div key={targetAddMountKey(route)} class="w-full">
								@TargetAddWorkflowComponent(wf)
							</div>
						} else {
							<div key={addTargetMountKey(route)} class="w-full">
								@AddTargetRowComponent(s, route)
							</div>
						}
					}
				}
			}
			// --- Draft route -----------------------------------------------
			if s.RouteDraft.IsOpen() {
				@SectionDraftRouteRow(s)
			}
			// --- Add route action ------------------------------------------
			<div key={addRouteMountKey()} class="w-full">
				@AddRouteRowComponent(s)
			</div>
		}
	</div>
}

templ ContractRow(label string, value string) {
	<div class="flex-row w-full">
		<span class="w-8"></span>
		<span class="w-18">{label}</span>
		<span>{value}</span>
	</div>
}

templ StepHeaderRow(label string) {
	<div class="flex-row w-full">
		<span class="w-8"></span>
		<span>{label}</span>
	</div>
}

templ (r *SectionDraftRouteRowView) Render() {
	<div class="flex-row w-full">
		<span class="w-5">{r.Arrow()}</span>
		<div class="w-18">
			<input value={r.ModelName} autoFocus onSubmit={r.Submit} width={18} />
		</div>
		<span class="w-35">no targets</span>
		<span>create ↵</span>
	</div>
}

templ SectionInertRow(label string, value string, action string) {
	<div class="flex-row w-full">
		<span class="w-5"></span>
		<span class="w-18">{label}</span>
		<span class="w-35">{value}</span>
		<span>{action}</span>
	</div>
}

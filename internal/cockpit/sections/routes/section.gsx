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
			<div class="pl-3 w-full">
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
							<div class="pl-3 w-full">
								// --- Name (editable) ------------------------------------
								<div key={s.routeNameRowKey(route)} class="w-full">
									@RouteNameRowComponent(s, route)
								</div>
								// --- Default toggle -------------------------------------
								if route.TargetCount() > 0 {
									<div key={s.routeDefaultRowKey(route)} class="w-full">
										@RouteDefaultRowComponent(s, route)
									</div>
								}
								<div key={s.shareRowKey(route)} class="w-full">
									@ShareRowComponent(s, route)
								</div>
								if route.Share != nil {
									<div key={s.shareRevokeRowKey(route)} class="w-full">
										@ShareRevokeRowComponent(s, route)
									</div>
								}
								// --- Target rows by step --------------------------------
								for tierIdx, tierTargets := range groupedTargets(route) {
									@StepHeaderRow(tierHeaderText(tierIdx, len(tierTargets) > 1))
									<div class="pl-3 w-full">
										for _, target := range tierTargets {
												if s.State.OpenTarget.Get() == target.ID {
													config := s.targetEditConfig(route, target)
													<div key={s.targetConfigKey(route, target.ID)} class="w-full">
														@TargetConfigComponent(config)
													</div>
												} else {
													<div key={targetMountKey(route, target)} class="w-full">
														@TargetRowComponent(s, route, target)
													</div>
												}
												// --- Delete confirmation for a target --------
												if s.State.DeleteConfirmTarget.Get() == target.ID {
													<div key={"del:" + string(target.ID)} class="w-full">
														@TargetDeleteConfirmRow(s, route, target)
													</div>
												}
										}
									</div>
								}
								// --- Add target trigger / config -------------------
								if s.State.AddTargetRoute.Get() == route.ID {
									config := s.targetAddConfig(route)
									<div key={targetAddMountKey(route)} class="w-full mt-1">
										@TargetConfigComponent(config)
									</div>
								} else {
									<div key={addTargetMountKey(route)} class="w-full mt-1">
										@AddTargetRowComponent(s, route)
									</div>
									// --- Route-level delete --------------------------
									// Hidden while the add-target workflow is active so the
									// operator does not delete the route mid-edit.
									<div key={s.routeDeleteRowKey(route)} class="w-full">
										@RouteDeleteRowComponent(s, route)
									</div>
								}
							</div>
						}
					}
				}
				<div class="w-full mt-1"/>
				// --- Draft route -----------------------------------------------
				if s.DraftRoute != nil && s.DraftRoute.IsExpanded() {
					<div key={"draft-route"} class="w-full">
						@DraftParentRowComponent(s)
					</div>
					<div class="pl-3 w-full">
						<div key={"draft-name"} class="w-full">
							@DraftNameRowComponent(s)
						</div>
					</div>
				}
				// --- Add route action ------------------------------------------
				if s.DraftRoute == nil {
					<div key={addRouteMountKey()} class="w-full">
						@AddRouteRowComponent(s)
					</div>
				}
			</div>
		}
	</div>
}

templ StepHeaderRow(label string) {
	<div class="flex-row w-full mt-1">
		<span class="w-2"></span>
		<span class="grow truncate nowrap" minWidth={0}>{label}</span>
	</div>
}

templ SectionInertRow(label string, value string, action string) {
	<div class="flex-row w-full">
		<span class="w-2"></span>
		<span class="w-18">{label}</span>
		<span class="grow truncate nowrap" minWidth={0}>{value}</span>
		if action != "" { <span class="w-14">{action}</span> }
	</div>
}

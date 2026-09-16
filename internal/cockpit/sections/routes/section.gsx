package routes

import "github.com/swobuforge/swobu/internal/cockpit/ui"

// ---------------------------------------------------------------------------
// Section render
// ---------------------------------------------------------------------------

templ (s *SectionView) Render() {
	<div class="flex-col w-full">
		<div key={"routes-header"} class="w-full">
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
								if feedback := s.State.ShareFeedback.Get(); feedback.RouteID == route.ID && feedback.Message != "" {
									<div class="pl-3 w-full" textStyle={ui.ToneStyle(feedback.Tone)}>@ui.FlowText(feedback.Message)</div>
								}
								if route.Share != nil {
									<div key={s.shareRevokeRowKey(route)} class="w-full">
										@ShareRevokeRowComponent(s, route)
									</div>
								}
								// --- Inline tier target rows -----------------------------
								for tierIdx, tierTargets := range groupedTargets(route) {
									for targetIdx, target := range tierTargets {
										<div key={targetMountKey(route, target)} class={tierTargetRowClass(targetIdx)}>
											@TargetControlComponent(s, route, target, tierIdx, targetIdx)
										</div>
									}
								}
								// --- Add target trigger / config -------------------
								<div key={addTargetMountKey(route)} class="w-full mt-1">
									@AddTargetControlComponent(s, route)
								</div>
								if s.State.AddTargetRoute.Get() != route.ID {
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

templ TierContextRow(label string, tone ui.Tone) {
	<div class="flex-row w-full mt-1">
		<span class="w-2"></span>
		<span class="w-18" textStyle={ui.ToneStyle(tone)}>{label}</span>
		<span class="grow" minWidth={0}></span>
	</div>
}

func tierTargetRowClass(targetIndex int) string {
	if targetIndex == 0 {
		return "w-full mt-1"
	}
	return "w-full"
}

templ SectionInertRow(label string, value string, action string) {
	<div class="flex-row w-full">
		<span class="w-2"></span>
		<span class="w-18">{label}</span>
		<span class="grow truncate nowrap" minWidth={0}>{value}</span>
		if action != "" { <span class="w-14">{action}</span> }
	</div>
}

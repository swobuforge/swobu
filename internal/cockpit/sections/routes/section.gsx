package routes

import (
	tui "github.com/grindlemire/go-tui"
	route_edit "github.com/swobuforge/swobu/internal/cockpit/features/route_edit"
	target_edit "github.com/swobuforge/swobu/internal/cockpit/features/target_edit"
)

// ---------------------------------------------------------------------------
// Section render
// ---------------------------------------------------------------------------

templ (s *SectionView) Render() {
	<div class="flex-col w-full">
		@SectionHeader("routes", s.Expanded.Get())
		if s.Expanded.Get() {
			// --- Routes list ---------------------------------------------------
			if len(s.State.Routes) == 0 {
				@SectionInertRow("(no routes)", "", "")
			} else {
				for _, route := range s.State.Routes {
					// --- Route row ------------------------------------------------
					if app != nil {
						<div key={routeMountKey(route)} class="w-full">
							@RouteRowComponent(s, route)
						</div>
					} else {
						@SectionInertRow(route.ModelName, route.RowValue(), routeActionLabel(s.isExpanded(route)))
					}
					if s.isExpanded(route) {
						// --- Target rows ------------------------------------------
						for _, target := range route.Targets {
							if app != nil {
								<div key={targetMountKey(route, target)} class="w-full">
									@TargetRowComponent(s, route, target)
								</div>
							} else {
								@SectionInertRow("target "+targetRankLabel(target), targetValue(target), "open ↵")
							}
							if s.State.OpenTarget.Get() == target.ID {
								workflow := s.targetEditor(route, target)
								if app != nil && workflow != nil {
									<div key={s.targetWorkflowKey(route, target.ID)} class="w-full">
										@workflow
									</div>
								} else if workflow != nil {
									@SectionTargetEditPreview(workflow)
								}
							}
						}
						// --- Route actions ----------------------------------------
						if app != nil {
							<div key={addTargetMountKey(route)} class="w-full">
								@AddTargetRowComponent(s, route)
							</div>
						} else {
							@SectionInertRow("add target", "", "add ↵")
						}
						// --- Target creator ---------------------------------------
						if s.State.AddTargetRoute.Get() == route.ID {
							workflow := s.targetCreator(route)
							if app != nil && workflow != nil {
								<div key={s.targetWorkflowKey(route, "")} class="w-full">
									@workflow
								</div>
							} else if workflow != nil {
									@SectionTargetEditPreview(workflow)
							}
						}
						// --- Route editor -----------------------------------------
						workflow := s.routeEditor(route)
						if app != nil && workflow != nil {
							<div key={s.routeWorkflowKey(route)} class="w-full">
								@workflow
							</div>
						} else if workflow != nil {
							@SectionRouteEditPreview(workflow)
						}
					}
				}
			}
			// --- Draft route ---------------------------------------------------
			if s.RouteDraft.IsOpen() {
				if app != nil {
					@SectionDraftRouteRow(s)
				} else {
					@SectionDraftRoutePreview(s)
				}
			}
			// --- Add route action ----------------------------------------------
			if app != nil {
				<div key={addRouteMountKey()} class="w-full">
					@AddRouteRowComponent(s)
				</div>
			} else {
				@SectionInertRow("add route", "", "add ↵")
			}
		}
	</div>
}

templ (r *SectionDraftRouteRowView) Render() {
	<div class="flex-row w-full">
		<span class="w-5"></span>
		if app != nil {
			<div class="w-18">
				<input value={r.ModelName} autoFocus onSubmit={r.Submit} border={tui.BorderRounded} />
			</div>
		} else {
			<span class="w-18">{r.ModelName.Get()}</span>
		}
		<span class="w-36">incomplete · no targets</span>
		<span>create ↵</span>
	</div>
}

// ---------------------------------------------------------------------------
// Layout helpers
// ---------------------------------------------------------------------------

templ SectionHeader(label string, expanded bool) {
	<div class="flex-row">
		<span class="w-2"></span>
		if expanded {
			<span>{label + " ▾"}</span>
		} else {
			<span>{label + " ▸"}</span>
		}
	</div>
}

templ SectionInertRow(label string, value string, action string) {
	<div class="flex-row w-full">
		<span class="w-5"></span>
		<span class="w-18">{label}</span>
		<span class="w-36">{value}</span>
		<span>{action}</span>
	</div>
}

// ---------------------------------------------------------------------------
// Draft route preview
// ---------------------------------------------------------------------------

templ SectionDraftRoutePreview(s *SectionView) {
	<div class="flex-row w-full" onActivate={func() { s.createDraftRoute() }}>
		<span class="w-5"></span>
		<span class="w-18">{s.RouteDraft.ModelName.Get()}</span>
		<span class="w-36">incomplete · no targets</span>
		<span>create ↵</span>
	</div>
}

// ---------------------------------------------------------------------------
// Route edit preview
// ---------------------------------------------------------------------------

templ SectionRouteEditPreview(workflow *route_edit.Workflow) {
	<div class="flex-col w-full">
		<div class="flex-row w-full">
			<span class="w-8"></span>
			<span class="w-15">model</span>
			<span class="w-30">{workflow.ModelName.Get()}</span>
			<span>{workflow.ActionLabel()}</span>
		</div>
		<div class="flex-row w-full">
			<span class="w-8"></span>
			<span class="w-15">default</span>
			<span class="w-30">{workflow.DefaultValueLabel()}</span>
			<span>{workflow.DefaultActionLabel()}</span>
		</div>
		<div class="flex-row w-full">
			<span class="w-8"></span>
			<span class="w-15">delete</span>
			<span class="w-30">{workflow.DeleteValueLabel()}</span>
			<span>{workflow.DeleteActionLabel()}</span>
		</div>
		if workflow.Error.Get() != "" {
			<div class="flex-row w-full">
				<span class="w-8"></span>
				<span>{workflow.Error.Get()}</span>
			</div>
		}
	</div>
}

// ---------------------------------------------------------------------------
// Target edit preview
// ---------------------------------------------------------------------------

templ SectionTargetEditPreview(workflow *target_edit.Workflow) {
	<div class="flex-col w-full">
		@SectionTargetEditPreviewField("name", workflow.Name.Get())
		@SectionTargetEditPreviewField("provider", workflow.Provider.Get())
		@SectionTargetEditPreviewField(workflow.ModelLabel(), workflow.Model.Get())
		if workflow.ShowBaseURL() {
			@SectionTargetEditPreviewField(workflow.BaseURLLabel(), workflow.BaseURL.Get())
		}
		if workflow.ShowAuthDisclosure() {
			@SectionTargetEditPreviewField("auth", workflow.AuthDisclosureValue())
		}
		if workflow.ShowDeviceCode() {
			@SectionTargetEditPreviewField("code", workflow.DeviceCodeValue())
		}
		if workflow.ShowCredential() {
			@SectionTargetEditPreviewField(workflow.CredentialLabel(), workflow.CredentialValue())
		}
		@SectionTargetEditPreviewField("rank", workflow.Rank.Get())
		@SectionTargetEditPreviewField("weight", workflow.Weight.Get())
		<div class="flex-row w-full">
			<span class="w-8"></span>
			<span class="w-15">target</span>
			<span class="w-36">{workflow.Name.Get()}</span>
			<span>{workflow.SaveActionLabel()}</span>
		</div>
		<div class="flex-row w-full">
			<span class="w-8"></span>
			<span class="w-15">delete</span>
			<span class="w-36">{workflow.DeleteValueLabel()}</span>
			<span>{workflow.DeleteActionLabel()}</span>
		</div>
		if workflow.ErrorMessage() != "" {
			<div class="flex-row w-full">
				<span class="w-8"></span>
				<span>{workflow.ErrorMessage()}</span>
			</div>
		}
	</div>
}

templ SectionTargetEditPreviewField(label string, value string) {
	<div class="flex-row w-full">
		<span class="w-8"></span>
		<span class="w-15">{label}</span>
		<span class="w-30">{value}</span>
	</div>
}

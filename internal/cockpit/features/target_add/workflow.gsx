package target_add

import (
	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/ui"
)

// ---------------------------------------------------------------------------
// Workflow render — mount nested components directly so they register focus.
// ---------------------------------------------------------------------------

templ (w *Workflow) Render() {
	<div class="flex-col w-full" deps={w.Phase, w.Provider, w.CredentialRef, w.ProviderSetup, w.AuthSession, w.CatalogLoading, w.SelectedModel, w.ManualModelID, w.Error}>
		if w.IsOpen() {
			<div class="flex-row w-full">
				<span class="w-8"></span>
				<span>{workflowTitle(w)}</span>
			</div>
			<div class="flex-row w-full">
				<span class="w-5"></span>
				<div class="flex-col w-full">
					if w.Phase.Get() == PhaseChoosingProvider {
						@ProviderPickerComponent(w)
					}
					if w.Phase.Get() == PhaseProviderSetup {
						@ProviderDisplayRowComponent(w)
						if w.requiresInteractiveAuth() {
							@AuthStartRowComponent(w)
							@ModelBlockedRowComponent(w)
						} else if w.ProviderSetup.Get().ReadyForCatalog {
							if w.AuthSession.Get().State == "succeeded" {
								@AuthSignedInRowComponent(w)
							} else if w.CredentialRef.Get() != "" {
								@CredentialDisplayRowComponent(w)
							}
							@LoadingCatalogRowComponent(w)
						} else {
							@ProviderSetupBlockedRowComponent(w)
							@ModelBlockedRowComponent(w)
						}
					}
					if w.Phase.Get() == PhaseLoadingCatalog {
						@ProviderDisplayRowComponent(w)
						if w.AuthSession.Get().State == "succeeded" {
							@AuthSignedInRowComponent(w)
						} else if w.CredentialRef.Get() != "" {
							@CredentialDisplayRowComponent(w)
						}
						@LoadingCatalogRowComponent(w)
					}
					if w.Phase.Get() == PhaseAuthPending {
						@ProviderDisplayRowComponent(w)
						@AuthPendingSummaryRowComponent(w)
						@AuthPendingOpenRowComponent(w)
						@AuthPendingStatusRowComponent(w)
						@AuthPendingCancelRowComponent(w)
						@ModelBlockedRowComponent(w)
						if w.Error.Get() != "" {
							<div class="flex-row w-full">
								<span class="w-8"></span>
								<span class="w-18"></span>
								<span>{w.Error.Get()}</span>
							</div>
						}
					}
					if w.Phase.Get() == PhaseChoosingModel {
						@ModelPickerComponent(w)
					}
					if w.Phase.Get() == PhaseCatalogFailed {
						@ProviderDisplayRowComponent(w)
						if w.AuthSession.Get().State == "succeeded" {
							@AuthSignedInRowComponent(w)
						} else if w.CredentialRef.Get() != "" {
							@CredentialDisplayRowComponent(w)
						}
						@CatalogFailedRetryRowComponent(w)
						if w.Error.Get() != "" {
							<div class="flex-row w-full">
								<span class="w-8"></span>
								<span class="w-18"></span>
								<span>{w.Error.Get()}</span>
							</div>
						}
						@ManualModelEntryRowComponent(w)
					}
					if w.Phase.Get() == PhaseChoosingPlacement {
						<div class="flex-row w-full">
							<span class="w-8"></span>
							<span class="w-18">placement</span>
							<span>choose placement</span>
						</div>
						for i, opt := range w.getPlacementOptions() {
							@PlacementPickerOptionRowComponent(w, i, opt)
						}
					}
					if w.Phase.Get() == PhaseReadyToCreate {
						@ProviderDisplayRowComponent(w)
						if w.AuthSession.Get().State == "succeeded" {
							@AuthSignedInRowComponent(w)
						} else if w.CredentialRef.Get() != "" {
							@CredentialDisplayRowComponent(w)
						}
						@ModelDisplayRowComponent(w)
						@PlacementDisplayRowComponent(w)
						@CreateRowComponent(w)
					}
					if w.Phase.Get() == PhaseAuthFailed {
						@ProviderDisplayRowComponent(w)
						@AuthFailedRowComponent(w)
						if w.Error.Get() != "" {
							<div class="flex-row w-full">
								<span class="w-8"></span>
								<span class="w-18"></span>
								<span>{w.Error.Get()}</span>
							</div>
						}
					}
					if w.Phase.Get() == PhaseCreateFailed {
						@CreateRetryRowComponent(w)
						if w.Error.Get() != "" {
							<div class="flex-row w-full">
								<span class="w-8"></span>
								<span class="w-18"></span>
								<span>{w.Error.Get()}</span>
							</div>
						}
					}
				</div>
			</div>
		}
	</div>
}

package target_add

// TargetAdd phase constants matching the RFC UX state machine.
type Phase int

const (
	PhaseClosed Phase = iota
	PhaseChoosingProvider
	PhaseProviderSetup
	PhaseAuthPending
	PhaseLoadingCatalog
	PhaseChoosingModel
	PhaseReadyToCreate
	PhaseChoosingPlacement
	PhaseCreated

	// Failure branches
	PhaseProviderSetupBlocked
	PhaseCatalogFailed
	PhaseAuthFailed
	PhaseCreateFailed
)

// IsTerminal reports whether the workflow has reached a final phase.
func (p Phase) IsTerminal() bool {
	switch p {
	case PhaseCreated, PhaseProviderSetupBlocked, PhaseCatalogFailed, PhaseAuthFailed, PhaseCreateFailed:
		return true
	}
	return false
}

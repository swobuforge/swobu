package managementaudit

import (
	"errors"
	"sort"
	"time"

	"github.com/swobuforge/swobu/internal/accessstate"
	"github.com/swobuforge/swobu/internal/accounting"
	"github.com/swobuforge/swobu/internal/app/operator/connect"
	"github.com/swobuforge/swobu/internal/configstore"
	"github.com/swobuforge/swobu/internal/connections"
	"github.com/swobuforge/swobu/internal/routing"
)

type Service struct {
	connections *connections.Store
	access      *accessstate.Store
	accounting  *accounting.Store
	connect     *connect.Attempts
	routing     *configstore.Store
}

// Snapshot carries private retained facts and explicit capture-family coverage,
// not current authority or a claim of complete management history.
type Snapshot struct {
	Workspace       string   `json:"workspace"`
	Coverage        string   `json:"coverage"`
	CaptureFamilies []string `json:"capture_families"`
	Events          []Event  `json:"events"`
}

// Event projects exactly one owner's committed facts. Its typed facts are
// mutually exclusive; their review context remains private operator data.
type Event struct {
	Connection           *connections.Audit                     `json:"connection,omitempty"`
	WorkspaceDisplayName *configstore.WorkspaceDisplayNameAudit `json:"workspace_display_name,omitempty"`
	WorkspaceCreation    *configstore.WorkspaceCreationAudit    `json:"workspace_creation,omitempty"`
	WorkspaceDeletion    *configstore.WorkspaceDeletionAudit    `json:"workspace_deletion,omitempty"`
	ClientConfiguration  *connect.Audit                         `json:"client_configuration,omitempty"`
	PrincipalLabel       *accessstate.PrincipalLabelAudit       `json:"principal_label,omitempty"`
	PrincipalCreation    *accessstate.PrincipalCreationAudit    `json:"principal_creation,omitempty"`
	Action               string                                 `json:"action"`
	Actor                string                                 `json:"actor"`
	Workspace            string                                 `json:"workspace"`
	CommittedAt          time.Time                              `json:"committed_at"`
	Permission           *accessstate.PermissionAudit           `json:"permission,omitempty"`
	Limit                *accounting.LimitChange                `json:"limit,omitempty"`
	PrincipalRevocation  *accessstate.PrincipalRevocationAudit  `json:"principal_revocation,omitempty"`
	CredentialRevocation *accessstate.CredentialRevocationAudit `json:"credential_revocation,omitempty"`
	CredentialIssuance   *accessstate.CredentialIssuanceAudit   `json:"credential_issuance,omitempty"`
}

func New(access *accessstate.Store, accountingStore *accounting.Store, connectAttempts *connect.Attempts, routingStore *configstore.Store, connectionStore *connections.Store) (Service, error) {
	if access == nil || accountingStore == nil || connectAttempts == nil || routingStore == nil || connectionStore == nil {
		return Service{}, errors.New("management audit requires Access, Accounting, Connect, Routing and Connection owners")
	}
	return Service{connections: connectionStore, access: access, accounting: accountingStore, connect: connectAttempts, routing: routingStore}, nil
}

// Read returns detached owner snapshots for an exact Workspace or Node scope
// when workspace is empty. It does not require current resource existence.
func (service Service) Read(workspace string) (Snapshot, error) {
	if workspace != "" {
		slug, err := routing.ParseWorkspaceSlug(workspace)
		if err != nil || slug.String() != workspace {
			return Snapshot{}, errors.New("management audit requires exact Workspace scope")
		}
	}
	result := Snapshot{Workspace: workspace, Coverage: "partial", CaptureFamilies: []string{"checked_permission", "approved_limit", "checked_principal_revocation", "checked_credential_revocation", "checked_credential_issuance", "checked_principal_creation", "checked_principal_label", "verified_client_configuration"}, Events: []Event{}}
	connectAudits, err := service.connect.Audits()
	if err != nil {
		return Snapshot{}, err
	}
	result.CaptureFamilies = append(result.CaptureFamilies, "checked_workspace_creation", "checked_workspace_deletion", "checked_workspace_display_name", "checked_connection")
	displayNames, err := service.routing.WorkspaceDisplayNameAudits()
	if err != nil {
		return Snapshot{}, err
	}
	for _, audit := range displayNames {
		if workspace == "" || audit.Workspace == workspace {
			result.Events = append(result.Events, Event{Action: audit.Action, Actor: audit.Caller, Workspace: audit.Workspace, CommittedAt: audit.CommittedAt, WorkspaceDisplayName: &audit})
		}
	}
	creations, err := service.routing.WorkspaceCreationAudits()
	if err != nil {
		return Snapshot{}, err
	}
	deletions, err := service.routing.WorkspaceDeletionAudits()
	if err != nil {
		return Snapshot{}, err
	}
	for _, audit := range creations {
		if workspace == "" || audit.Workspace == workspace {
			result.Events = append(result.Events, Event{Action: audit.Action, Actor: audit.Caller, Workspace: audit.Workspace, CommittedAt: audit.CommittedAt, WorkspaceCreation: &audit})
		}
	}
	for _, audit := range deletions {
		if workspace == "" || audit.Workspace == workspace {
			result.Events = append(result.Events, Event{Action: audit.Action, Actor: audit.Caller, Workspace: audit.Workspace, CommittedAt: audit.CommittedAt, WorkspaceDeletion: &audit})
		}
	}
	for _, audit := range connectAudits {
		if workspace == "" || audit.Workspace == workspace {
			result.Events = append(result.Events, Event{Action: audit.Action, Actor: audit.Caller, Workspace: audit.Workspace, CommittedAt: audit.CommittedAt, ClientConfiguration: &audit})
		}
	}
	if workspace == "" {
		for _, audit := range service.connections.Audits() {
			result.Events = append(result.Events, Event{Action: audit.Action, Actor: audit.Caller, CommittedAt: audit.CommittedAt, Connection: &audit})
		}
		for _, audit := range service.access.PrincipalLabelAudits() {
			result.Events = append(result.Events, Event{Action: "principal.label", Actor: audit.Caller, CommittedAt: audit.CommittedAt, PrincipalLabel: &audit})
		}
		for _, audit := range service.access.PrincipalCreationAudits() {
			result.Events = append(result.Events, Event{Action: "principal.create", Actor: audit.Caller, CommittedAt: audit.CommittedAt, PrincipalCreation: &audit})
		}
	}
	for _, audit := range service.access.CredentialIssuanceAudits() {
		if workspace == "" || audit.Workspace == workspace {
			result.Events = append(result.Events, Event{Action: "credential.issue", Actor: audit.Caller, Workspace: audit.Workspace, CommittedAt: audit.CommittedAt, CredentialIssuance: &audit})
		}
	}
	for _, audit := range service.access.CredentialRevocationAudits() {
		if workspace == "" || audit.Workspace == workspace {
			result.Events = append(result.Events, Event{Action: "credential.revoke", Actor: audit.Caller, Workspace: audit.Workspace, CommittedAt: audit.CommittedAt, CredentialRevocation: &audit})
		}
	}
	for _, audit := range service.access.PrincipalRevocationAudits() {
		if workspace == "" {
			result.Events = append(result.Events, Event{Action: "principal.revoke", Actor: audit.Caller, CommittedAt: audit.CommittedAt, PrincipalRevocation: &audit})
			continue
		}
		for _, affected := range audit.Workspaces {
			if affected == workspace {
				result.Events = append(result.Events, Event{Action: "principal.revoke", Actor: audit.Caller, Workspace: workspace, CommittedAt: audit.CommittedAt, PrincipalRevocation: &audit})
				break
			}
		}
	}
	for _, receipt := range service.access.PermissionAudits() {
		if workspace == "" || receipt.Audit.Workspace == workspace {
			result.Events = append(result.Events, Event{Action: receipt.Audit.Action, Actor: receipt.Caller, Workspace: receipt.Audit.Workspace, CommittedAt: receipt.Audit.CommittedAt, Permission: receipt.Audit})
		}
	}
	var changes []accounting.LimitChange
	if workspace == "" {
		changes = service.accounting.AllLimitChanges()
	} else {
		changes = service.accounting.LimitChanges(workspace)
	}
	for _, change := range changes {
		result.Events = append(result.Events, Event{Action: "limit.apply", Actor: change.Actor, Workspace: change.Workspace, CommittedAt: change.AppliedAt, Limit: &change})
	}
	sort.SliceStable(result.Events, func(left, right int) bool {
		return result.Events[left].CommittedAt.Before(result.Events[right].CommittedAt)
	})
	return result, nil
}

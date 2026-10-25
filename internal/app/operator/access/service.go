package access

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/swobuforge/swobu/internal/accessstate"
	"github.com/swobuforge/swobu/internal/app/operator/routebindings"
	"github.com/swobuforge/swobu/internal/configstore"
	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/routing"
)

type Service struct {
	state    *accessstate.Store
	routing  *configstore.Store
	bindings *routebindings.Coordinator
}

func NewService(state *accessstate.Store, routingStore *configstore.Store, bindings *routebindings.Coordinator) (Service, error) {
	if state == nil || routingStore == nil || bindings == nil {
		return Service{}, errors.New("access state, routing store, and binding coordinator are required")
	}
	return Service{state: state, routing: routingStore, bindings: bindings}, nil
}

type Credential struct {
	ID        string    `json:"id"`
	Workspace string    `json:"workspace"`
	CreatedAt time.Time `json:"created_at"`
}
type Access struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Kind        string       `json:"kind"`
	Status      string       `json:"status"`
	CreatedAt   time.Time    `json:"created_at,omitempty"`
	Credentials []Credential `json:"credentials"`
}

// Principal is the ontology projection of one managed Access identity. The
// projection preserves the historical ID and credential lifecycle; it does
// not claim that a label proves a human identity.
type Principal struct {
	Revision    string       `json:"revision"`
	ID          string       `json:"id"`
	Label       string       `json:"label"`
	Kind        string       `json:"kind"`
	Status      string       `json:"status"`
	CreatedAt   time.Time    `json:"created_at,omitempty"`
	Permissions []Permission `json:"permissions"`
}

// Permission is the natural Principal-to-Workspace use relationship. A
// credential is intentionally not embedded: permission and credential have
// independent revocation lifecycles.
type Permission struct {
	Workspace     string                         `json:"workspace"`
	Action        string                         `json:"action"`
	ExpiresAt     *time.Time                     `json:"expires_at,omitempty"`
	Services      []routing.ServiceKind          `json:"services,omitempty"`
	RequestLimits []accessstate.RequestRateLimit `json:"request_limits,omitempty"`
}

func (s Service) GrantPermission(_ context.Context, accessRaw, workspaceRaw string) error {
	return s.GrantPermissionAtRevision(accessRaw, workspaceRaw, "")
}

func (s Service) GrantPermissionAtRevision(accessRaw, workspaceRaw, expectedRevision string) error {
	return s.grantPermissionAtRevision(accessRaw, workspaceRaw, expectedRevision, "")
}

func (s Service) GrantPermissionIdempotent(accessRaw, workspaceRaw, expectedRevision, key string, expiry ...time.Time) error {
	return s.grantPermissionAtRevision(accessRaw, workspaceRaw, expectedRevision, key, expiry...)
}

func (s Service) grantPermissionAtRevision(accessRaw, workspaceRaw, expectedRevision, key string, expiry ...time.Time) error {
	unlock := s.bindings.Lock()
	defer unlock()
	id, err := accessidentity.RestoreManaged(accessRaw)
	if err != nil {
		return err
	}
	workspace, err := routing.ParseWorkspaceSlug(workspaceRaw)
	if err != nil {
		return err
	}
	if _, ok := s.routing.Config().Workspace(workspace); !ok {
		return fmt.Errorf("%w: workspace %q", routing.ErrNotFound, workspaceRaw)
	}
	return s.state.ApplyPermission(id, workspace, expectedRevision, key, true, expiry...)
}

func (s Service) RevokePermission(_ context.Context, accessRaw, workspaceRaw string) error {
	return s.RevokePermissionAtRevision(accessRaw, workspaceRaw, "")
}

func (s Service) RevokePermissionAtRevision(accessRaw, workspaceRaw, expectedRevision string) error {
	return s.revokePermissionAtRevision(accessRaw, workspaceRaw, expectedRevision, "")
}

func (s Service) RevokePermissionIdempotent(accessRaw, workspaceRaw, expectedRevision, key string) error {
	return s.revokePermissionAtRevision(accessRaw, workspaceRaw, expectedRevision, key)
}

func (s Service) revokePermissionAtRevision(accessRaw, workspaceRaw, expectedRevision, key string) error {
	id, err := accessidentity.RestoreManaged(accessRaw)
	if err != nil {
		return err
	}
	workspace, err := routing.ParseWorkspaceSlug(workspaceRaw)
	if err != nil {
		return err
	}
	return s.state.ApplyPermission(id, workspace, expectedRevision, key, false)
}

func (s Service) Revision() string { return s.state.Revision() }

// ApplyPermissionChecked owns the admitted Control command. Grant existence is
// serialized with Workspace destruction; Node admission repeats under the Access
// transaction lock on both apply and replay. authorize must not reenter Access.
func (s Service) ApplyPermissionChecked(ctx context.Context, accessRaw, workspaceRaw, expected, key string, grant bool, authorize func() error, expiry ...time.Time) (PermissionMutation, error) {
	unlock := s.bindings.Lock()
	defer unlock()
	id, err := accessidentity.RestoreManaged(accessRaw)
	if err != nil {
		return PermissionMutation{}, err
	}
	workspace, err := routing.ParseWorkspaceSlug(workspaceRaw)
	if err != nil {
		return PermissionMutation{}, err
	}
	if grant {
		if _, ok := s.routing.Config().Workspace(workspace); !ok {
			return PermissionMutation{}, fmt.Errorf("%w: workspace %q", routing.ErrNotFound, workspaceRaw)
		}
	}
	result, err := s.state.ApplyPermissionChecked(ctx, id, workspace, expected, key, grant, authorize, expiry...)
	if err != nil {
		return PermissionMutation{}, err
	}
	return PermissionMutation{PermissionState: PermissionState{Principal: id.String(), Workspace: workspace.String(), Revision: result.Revision, Permission: projectPermission(result.Permission), Allowed: result.Allowed}, Replayed: result.Replayed}, nil
}

// ApplyPermissionServicesChecked binds Service scope to the same Workspace
// relationship revision and atomic receipt as existing permission grants.
func (s Service) ApplyPermissionServicesChecked(ctx context.Context, accessRaw, workspaceRaw, expected, key string, grant bool, authorize func() error, services []routing.ServiceKind, expiry ...time.Time) (PermissionMutation, error) {
	return s.ApplyPermissionServicesWithRatesChecked(ctx, accessRaw, workspaceRaw, expected, key, grant, authorize, services, nil, expiry...)
}

func (s Service) ApplyPermissionServicesWithRatesChecked(ctx context.Context, accessRaw, workspaceRaw, expected, key string, grant bool, authorize func() error, services []routing.ServiceKind, rates []accessstate.RequestRateLimit, expiry ...time.Time) (PermissionMutation, error) {
	unlock := s.bindings.Lock()
	defer unlock()
	id, err := accessidentity.RestoreManaged(accessRaw)
	if err != nil {
		return PermissionMutation{}, err
	}
	workspace, err := routing.ParseWorkspaceSlug(workspaceRaw)
	if err != nil {
		return PermissionMutation{}, err
	}
	if grant {
		w, ok := s.routing.Config().Workspace(workspace)
		if !ok {
			return PermissionMutation{}, fmt.Errorf("%w: workspace %q", routing.ErrNotFound, workspaceRaw)
		}
		for _, kind := range services {
			if !w.HasService(kind) {
				return PermissionMutation{}, fmt.Errorf("Service %s is not enabled in Workspace %s", kind, workspaceRaw)
			}
		}
	}
	var result accessstate.PermissionMutation
	if grant {
		result, err = s.state.ApplyPermissionServicesWithRatesChecked(ctx, id, workspace, expected, key, authorize, services, rates, expiry...)
	} else {
		result, err = s.state.ApplyPermissionServicesChecked(ctx, id, workspace, expected, key, false, authorize, nil)
	}
	if err != nil {
		return PermissionMutation{}, err
	}
	return PermissionMutation{PermissionState: PermissionState{Principal: id.String(), Workspace: workspace.String(), Revision: result.Revision, Permission: projectPermission(result.Permission), Allowed: result.Allowed}, Replayed: result.Replayed}, nil
}

// PermissionMutation projects the locked apply result. Replayed distinguishes
// receipt acknowledgement from a new effect; state is current, not historical.
type PermissionMutation struct {
	PermissionState
	Replayed bool `json:"replayed"`
}

// PermissionPreview is the authoritative advisory diff for a natural
// Principal/Workspace use relationship. It is not a retained approval token.
type PermissionPreview struct {
	Principal           string      `json:"principal"`
	Workspace           string      `json:"workspace"`
	Operation           string      `json:"operation"`
	Revision            string      `json:"revision"`
	Before              *Permission `json:"before"`
	After               *Permission `json:"after"`
	AffectedCredentials int         `json:"affected_credentials"`
	ApprovalClass       string      `json:"approval_class"`
	Warning             string      `json:"warning"`
}

// PreviewPermission serializes Workspace existence with destruction and shares
// store candidate validation with apply. It performs no persistence or issuance.
func (s Service) PreviewPermission(accessRaw, workspaceRaw, expected string, grant bool, expiry ...time.Time) (PermissionPreview, error) {
	if len(expiry) > 1 || (!grant && len(expiry) != 0) {
		return PermissionPreview{}, errors.New("expiry belongs to one permission grant")
	}
	unlock := s.bindings.Lock()
	defer unlock()
	id, err := accessidentity.RestoreManaged(accessRaw)
	if err != nil {
		return PermissionPreview{}, err
	}
	workspace, err := routing.ParseWorkspaceSlug(workspaceRaw)
	if err != nil {
		return PermissionPreview{}, err
	}
	if _, ok := s.routing.Config().Workspace(workspace); !ok {
		return PermissionPreview{}, fmt.Errorf("%w: workspace %q", routing.ErrNotFound, workspaceRaw)
	}
	var expiresAt time.Time
	if len(expiry) == 1 {
		expiresAt = expiry[0]
	}
	impact, err := s.state.PreviewPermission(id, workspace, expected, grant, expiresAt)
	if err != nil {
		return PermissionPreview{}, err
	}
	operation := "revoke"
	if grant {
		operation = "grant"
	}
	return PermissionPreview{Principal: id.String(), Workspace: workspace.String(), Operation: operation, Revision: impact.Revision, Before: projectPermission(impact.Before), After: projectPermission(impact.After), AffectedCredentials: impact.CredentialCount, ApprovalClass: "trusted_confirmation", Warning: "Preview is not approval. Permission changes affect all credentials for this Principal and Workspace; no credential is issued or erased."}, nil
}

func (s Service) PreviewPermissionServices(accessRaw, workspaceRaw, expected string, grant bool, services []routing.ServiceKind, expiry ...time.Time) (PermissionPreview, error) {
	return s.PreviewPermissionServicesWithRates(accessRaw, workspaceRaw, expected, grant, services, nil, expiry...)
}

func (s Service) PreviewPermissionServicesWithRates(accessRaw, workspaceRaw, expected string, grant bool, services []routing.ServiceKind, rates []accessstate.RequestRateLimit, expiry ...time.Time) (PermissionPreview, error) {
	if len(expiry) > 1 {
		return PermissionPreview{}, errors.New("one permission expiry maximum")
	}
	unlock := s.bindings.Lock()
	defer unlock()
	id, err := accessidentity.RestoreManaged(accessRaw)
	if err != nil {
		return PermissionPreview{}, err
	}
	workspace, err := routing.ParseWorkspaceSlug(workspaceRaw)
	if err != nil {
		return PermissionPreview{}, err
	}
	w, ok := s.routing.Config().Workspace(workspace)
	if !ok {
		return PermissionPreview{}, fmt.Errorf("%w: Workspace %s", routing.ErrNotFound, workspaceRaw)
	}
	for _, kind := range services {
		if !w.HasService(kind) {
			return PermissionPreview{}, fmt.Errorf("Service %s is not enabled in Workspace %s", kind, workspaceRaw)
		}
	}
	var exp time.Time
	if len(expiry) == 1 {
		exp = expiry[0]
	}
	impact, err := s.state.PreviewPermissionServicesWithRates(id, workspace, expected, grant, exp, services, rates)
	if err != nil {
		return PermissionPreview{}, err
	}
	operation := "revoke"
	if grant {
		operation = "grant"
	}
	return PermissionPreview{Principal: id.String(), Workspace: workspace.String(), Operation: operation, Revision: impact.Revision,
		Before: projectPermission(impact.Before), After: projectPermission(impact.After), AffectedCredentials: impact.CredentialCount,
		ApprovalClass: "trusted_confirmation", Warning: "Preview is not approval. All active credentials of this Principal/Workspace observe the new Service scope immediately. No credential is issued or erased."}, nil
}

func projectPermission(value *accessstate.Permission) *Permission {
	if value == nil {
		return nil
	}
	result := &Permission{Workspace: value.Workspace.String(), Action: "use", Services: append([]routing.ServiceKind(nil), value.Services...), RequestLimits: append([]accessstate.RequestRateLimit(nil), value.RequestLimits...)}
	if !value.ExpiresAt.IsZero() {
		expiresAt := value.ExpiresAt
		result.ExpiresAt = &expiresAt
	}
	return result
}

// PermissionState describes a natural relationship without returning credential
// material. Null Permission means no grant, not a missing Principal or Workspace.
type PermissionState struct {
	Principal  string      `json:"principal"`
	Workspace  string      `json:"workspace"`
	Revision   string      `json:"revision"`
	Permission *Permission `json:"permission"`
	Allowed    bool        `json:"allowed"`
}

// ReadPermission validates resource existence and reads relationship/revision
// atomically from the authority owner. It never grants or issues a credential.
func (s Service) ReadPermission(accessRaw, workspaceRaw string) (PermissionState, error) {
	unlock := s.bindings.Lock()
	defer unlock()
	id, err := accessidentity.RestoreManaged(accessRaw)
	if err != nil {
		return PermissionState{}, err
	}
	workspace, err := routing.ParseWorkspaceSlug(workspaceRaw)
	if err != nil {
		return PermissionState{}, err
	}
	if _, ok := s.routing.Config().Workspace(workspace); !ok {
		return PermissionState{}, fmt.Errorf("%w: workspace %q", routing.ErrNotFound, workspaceRaw)
	}
	snapshot, err := s.state.ReadPermission(id, workspace)
	if err != nil {
		return PermissionState{}, err
	}
	return PermissionState{Principal: id.String(), Workspace: workspace.String(), Revision: snapshot.Revision, Permission: projectPermission(snapshot.Permission), Allowed: snapshot.Allowed}, nil
}

type IssueResult struct {
	Access               Access     `json:"access"`
	Credential           Credential `json:"credential"`
	Token                string     `json:"token,omitempty"`
	SecretNotRecoverable bool       `json:"secret_not_recoverable,omitempty"`
}

func (s Service) List(context.Context) []Access {
	out := []Access{{ID: accessidentity.Local.String(), Name: "Local", Kind: "builtin", Status: "active", Credentials: []Credential{}}}
	for _, item := range s.state.List() {
		out = append(out, project(item))
	}
	sort.Slice(out[1:], func(i, j int) bool { return out[i+1].ID < out[j+1].ID })
	return out
}

// ListPrincipals derives the canonical Principal/Permission view from the
// existing Access store. It is read-only and keeps stable managed IDs intact.
func (s Service) ListPrincipals(context.Context) []Principal {
	out := make([]Principal, 0)
	for _, item := range s.state.List() {
		out = append(out, projectPrincipal(item))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func projectPrincipal(item accessstate.Access) Principal {
	principal := Principal{Revision: accessstate.PrincipalRevision(item), ID: item.ID.String(), Label: item.Name, Kind: "managed", Status: "active", CreatedAt: item.CreatedAt, Permissions: []Permission{}}
	if item.RevokedAt != nil {
		principal.Status = "revoked"
	}
	seen := map[string]struct{}{}
	for _, permission := range item.Permissions {
		workspace := permission.Workspace.String()
		if _, ok := seen[workspace]; ok {
			continue
		}
		seen[workspace] = struct{}{}
		projected := Permission{Workspace: workspace, Action: "use", Services: append([]routing.ServiceKind(nil), permission.Services...), RequestLimits: append([]accessstate.RequestRateLimit(nil), permission.RequestLimits...)}
		if !permission.ExpiresAt.IsZero() {
			expires := permission.ExpiresAt
			projected.ExpiresAt = &expires
		}
		principal.Permissions = append(principal.Permissions, projected)
	}
	sort.Slice(principal.Permissions, func(i, j int) bool { return principal.Permissions[i].Workspace < principal.Permissions[j].Workspace })
	return principal
}

// CreatePrincipalChecked creates identity metadata only. Permission and secret
// issuance are separate reviewed operations; replay projects the locked result.
func (s Service) CreatePrincipalChecked(ctx context.Context, label, key string, authorize func() error) (Principal, bool, error) {
	identity, replayed, err := s.state.CreatePrincipalChecked(ctx, label, key, authorize)
	if err != nil {
		return Principal{}, false, err
	}
	return projectPrincipal(identity), replayed, nil
}

// GetPrincipal returns the same redacted managed identity projection as listing.
// Revocation preserves attribution; it does not make the Principal disappear.
func (s Service) GetPrincipal(ctx context.Context, raw string) (Principal, error) {
	id, err := accessidentity.RestoreManaged(raw)
	if err != nil {
		return Principal{}, err
	}
	for _, principal := range s.ListPrincipals(ctx) {
		if principal.ID == id.String() {
			return principal, nil
		}
	}
	return Principal{}, accessstate.ErrNotFound
}

// UpdatePrincipalLabelChecked changes metadata without granting or revoking
// authority. The transaction returns the exact committed revision/projection.
func (s Service) UpdatePrincipalLabelChecked(ctx context.Context, raw, label, expected string, authorize func() error) (Principal, error) {
	id, err := accessidentity.RestoreManaged(raw)
	if err != nil {
		return Principal{}, err
	}
	identity, err := s.state.UpdatePrincipalLabelChecked(ctx, id, label, expected, authorize)
	if err != nil {
		return Principal{}, err
	}
	return projectPrincipal(identity), nil
}

// PrincipalRevocation projects either advisory impact or committed current state
// without exposing credential digests or retry metadata.
type PrincipalRevocation struct {
	Principal       Principal `json:"principal"`
	CredentialCount int       `json:"credential_count"`
	Replayed        bool      `json:"replayed"`
}

// PreviewPrincipalRevocation returns effect-free impact bound to exact consent.
func (s Service) PreviewPrincipalRevocation(raw, expected string, authorize func() error) (PrincipalRevocation, error) {
	id, err := accessidentity.RestoreManaged(raw)
	if err != nil {
		return PrincipalRevocation{}, err
	}
	identity, err := s.state.PreviewPrincipalRevocation(id, expected, authorize)
	if err != nil {
		return PrincipalRevocation{}, err
	}
	return PrincipalRevocation{Principal: projectPrincipal(identity), CredentialCount: len(identity.Credentials)}, nil
}

// ApplyPrincipalRevocation delegates authority and receipt to one transaction.
func (s Service) ApplyPrincipalRevocation(raw, expected, key string, authorize func() error) (PrincipalRevocation, error) {
	id, err := accessidentity.RestoreManaged(raw)
	if err != nil {
		return PrincipalRevocation{}, err
	}
	identity, replayed, err := s.state.ApplyPrincipalRevocation(id, expected, key, authorize)
	if err != nil {
		return PrincipalRevocation{}, err
	}
	return PrincipalRevocation{Principal: projectPrincipal(identity), CredentialCount: len(identity.Credentials), Replayed: replayed}, nil
}

// CredentialRevocationPreview is server-owned, effect-free selected impact.
// It contains public metadata only and is not trusted approval.
type CredentialRevocationPreview struct {
	Principal     string     `json:"principal"`
	Credential    Credential `json:"credential"`
	Revision      string     `json:"revision"`
	ApprovalClass string     `json:"approval_class"`
	Warning       string     `json:"warning"`
}

// CredentialRevocation acknowledges selected removal and current identity state.
// Replayed reports receipt recovery, not another effect or secret delivery.
type CredentialRevocation struct {
	Principal  Principal `json:"principal"`
	Credential string    `json:"credential"`
	Revoked    bool      `json:"revoked"`
	Replayed   bool      `json:"replayed"`
}

// PreviewCredentialRevocation delegates impact and revision to locked Access state.
func (s Service) PreviewCredentialRevocation(raw, credentialRaw string, authorize func() error) (CredentialRevocationPreview, error) {
	id, err := accessidentity.RestoreManaged(raw)
	if err != nil {
		return CredentialRevocationPreview{}, err
	}
	credential, err := accessstate.ParseCredentialID(credentialRaw)
	if err != nil {
		return CredentialRevocationPreview{}, err
	}
	impact, err := s.state.PreviewCredentialRevocation(id, credential, authorize)
	if err != nil {
		return CredentialRevocationPreview{}, err
	}
	return CredentialRevocationPreview{Principal: raw, Credential: Credential{ID: credentialRaw, Workspace: impact.Workspace.String(), CreatedAt: impact.CreatedAt}, Revision: impact.Revision, ApprovalClass: "trusted_confirmation", Warning: "Preview is not approval. Only this credential is removed; permissions and sibling credentials remain unchanged."}, nil
}

// ApplyCredentialRevocation projects the locked receipt/removal transaction.
func (s Service) ApplyCredentialRevocation(ctx context.Context, raw, credentialRaw, expected, key string, authorize func() error) (CredentialRevocation, error) {
	id, err := accessidentity.RestoreManaged(raw)
	if err != nil {
		return CredentialRevocation{}, err
	}
	credential, err := accessstate.ParseCredentialID(credentialRaw)
	if err != nil {
		return CredentialRevocation{}, err
	}
	identity, replayed, err := s.state.ApplyCredentialRevocation(ctx, id, credential, expected, key, authorize)
	if err != nil {
		return CredentialRevocation{}, err
	}
	return CredentialRevocation{Principal: projectPrincipal(identity), Credential: credentialRaw, Revoked: true, Replayed: replayed}, nil
}

// ListCredentials returns only currently retained credential metadata. The
// store never recovers plaintext, and digests do not cross this projection.
// A revoked Principal exists but has an empty credential collection.
func (s Service) ListCredentials(_ context.Context, raw string) ([]Credential, error) {
	id, err := accessidentity.RestoreManaged(raw)
	if err != nil {
		return nil, err
	}
	for _, identity := range s.state.List() {
		if identity.ID == id {
			return project(identity).Credentials, nil
		}
	}
	return nil, accessstate.ErrNotFound
}

func (s Service) Create(_ context.Context, name, workspaceRaw string) (IssueResult, error) {
	workspace, err := routing.ParseWorkspaceSlug(workspaceRaw)
	if err != nil {
		return IssueResult{}, err
	}
	unlock := s.bindings.Lock()
	defer unlock()
	if _, ok := s.routing.Config().Workspace(workspace); !ok {
		return IssueResult{}, fmt.Errorf("%w: workspace %q", routing.ErrNotFound, workspaceRaw)
	}
	identity, issued, err := s.state.Create(name, workspace)
	if err != nil {
		return IssueResult{}, err
	}
	return issueResult(identity, issued), nil
}

func (s Service) IssueCredential(_ context.Context, accessRaw, workspaceRaw string) (IssueResult, error) {
	id, err := accessidentity.RestoreManaged(accessRaw)
	if err != nil {
		return IssueResult{}, err
	}
	workspace, err := routing.ParseWorkspaceSlug(workspaceRaw)
	if err != nil {
		return IssueResult{}, err
	}
	unlock := s.bindings.Lock()
	defer unlock()
	if _, ok := s.routing.Config().Workspace(workspace); !ok {
		return IssueResult{}, fmt.Errorf("%w: workspace %q", routing.ErrNotFound, workspaceRaw)
	}
	issued, err := s.state.IssueCredential(id, workspace)
	if err != nil {
		return IssueResult{}, err
	}
	var identity accessstate.Access
	for _, item := range s.state.List() {
		if item.ID == id {
			identity = item
			break
		}
	}
	return issueResult(identity, issued), nil
}

// IssueCredentialChecked retains exact issuance intent under local admission.
// Duplicate delivery returns redacted metadata, never another one-shot secret.
func (s Service) IssueCredentialChecked(accessRaw, workspaceRaw, key string, authorize func() error) (IssueResult, error) {
	id, err := accessidentity.RestoreManaged(accessRaw)
	if err != nil {
		return IssueResult{}, err
	}
	workspace, err := routing.ParseWorkspaceSlug(workspaceRaw)
	if err != nil {
		return IssueResult{}, err
	}
	unlock := s.bindings.Lock()
	defer unlock()
	if _, ok := s.routing.Config().Workspace(workspace); !ok {
		return IssueResult{}, fmt.Errorf("%w: workspace %q", routing.ErrNotFound, workspaceRaw)
	}
	issued, replayed, err := s.state.IssueCredentialChecked(id, workspace, key, authorize)
	if err != nil {
		return IssueResult{}, err
	}
	var identity accessstate.Access
	for _, item := range s.state.List() {
		if item.ID == id {
			identity = item
			break
		}
	}
	result := issueResult(identity, issued)
	result.SecretNotRecoverable = replayed
	return result, nil
}

func (s Service) Revoke(_ context.Context, accessRaw string) error {
	id, err := accessidentity.RestoreManaged(accessRaw)
	if err != nil {
		return err
	}
	_, err = s.state.RevokeAccess(id)
	return err
}

func project(item accessstate.Access) Access {
	status := "active"
	if item.RevokedAt != nil {
		status = "revoked"
	}
	out := Access{ID: item.ID.String(), Name: item.Name, Kind: "managed", Status: status, CreatedAt: item.CreatedAt, Credentials: []Credential{}}
	for _, credential := range item.Credentials {
		out.Credentials = append(out.Credentials, Credential{ID: credential.ID.String(), Workspace: credential.Workspace.String(), CreatedAt: credential.CreatedAt})
	}
	return out
}
func issueResult(identity accessstate.Access, issued accessstate.IssuedCredential) IssueResult {
	return IssueResult{Access: project(identity), Credential: Credential{ID: issued.Credential.ID.String(), Workspace: issued.Credential.Workspace.String(), CreatedAt: issued.Credential.CreatedAt}, Token: issued.Token}
}

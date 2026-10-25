// Package connect owns the browser-safe application seam for local client
// discovery, reviewed planning, and apply. Filesystem mutation remains in
// clientconnect; one validated Target owns workspace identity and address.
// Discovery rejects cancellation before inspection and after its return;
// interrupted presence inventory cannot be acknowledged as complete.
// Planning repeats cancellation checks before projecting review or entering
// apply. After apply begins, its owner result retains effect/uncertainty truth;
// late cancellation does not manufacture rollback or erase readback.
// Before review projection or apply dispatch, planner ClientID and exact Target
// must match the request. Presentation cannot relabel another destination's plan.
// Post-effect readback must retain that binding too. Invalid readback returns no
// projected success and preserves the owner's error; effects are not rolled back.
// Preview revision binds exact destination, client, paths and semantic changes.
// It is content-derived review identity, not approval, a durable receipt or an
// ABA-resistant client-file generation; external apply recovery remains separate.
// Apply requires the original preview revision and exact reviewed evidence.
// Missing and changed review are distinct pre-effect failures, never refreshed
// consent. Trusted confirmation remains the consumer's responsibility.
// Preview binds the selected existing Principal and current Models permission.
// Access validates configured credentials; only confirmed durable apply may issue
// through Access's existing one-shot receipt. No identities or grants are created.
// Fresh configured acknowledgement requires both file convergence and acceptance
// of the exact read-back bearer for this Principal, Workspace and Models service.
// Reconcile only observes retained original intent; it never retries Apply or
// attributes a matching current configuration to a particular prior writer.
package connect

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	accessapp "github.com/swobuforge/swobu/internal/app/operator/access"
	"github.com/swobuforge/swobu/internal/clientconnect"
)

var ErrReviewChanged = errors.New("client configuration changed after review")
var ErrReviewRequired = errors.New("client configuration review revision is required")
var ErrModelsPermission = errors.New("selected Principal requires current Workspace Models permission")

type ClientService interface {
	Discover(context.Context, clientconnect.Target) []clientconnect.Client
	Plan(context.Context, clientconnect.ClientID, clientconnect.Target) (clientconnect.Plan, error)
	Apply(context.Context, clientconnect.Plan) (clientconnect.Plan, error)
}

// CredentialAuthority is implemented by Access, the sole owner of credential
// validity and one-shot issuance. Connect never creates identities or grants.
type CredentialAuthority interface {
	ClientPermission(string, string) (string, error)
	ClientCredential(string, string, string) (string, error)
	IssueClientCredentialChecked(string, string, string, string, func() error) (accessapp.IssueResult, error)
	ClientCredentialIssuance(string, string, string) (string, error)
}

type Service struct {
	clients     ClientService
	credentials CredentialAuthority
}

func NewService(clients ClientService, credentials CredentialAuthority) (*Service, error) {
	if clients == nil || credentials == nil {
		return nil, fmt.Errorf("client service and credential authority are required")
	}
	return &Service{clients: clients, credentials: credentials}, nil
}

type Client struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Change struct {
	Field        string `json:"field"`
	Before       string `json:"before"`
	BeforeExists bool   `json:"before_exists"`
	After        string `json:"after"`
}

type Review struct {
	Revision       string         `json:"revision"`
	ConfigPaths    []string       `json:"config_paths"`
	Changes        []Change       `json:"changes"`
	Authentication Authentication `json:"authentication"`
}

// Authentication contains public reviewed authority, never bearer material.
// PermissionRevision binds fresh provisioning to the exact reviewed grant.
type Authentication struct {
	Principal          string `json:"principal"`
	PermissionRevision string `json:"permission_revision"`
	IssueCredential    bool   `json:"issue_credential"`
	CredentialID       string `json:"credential_id,omitempty"`
}

type Plan struct {
	Revision          string         `json:"revision"`
	Workspace         string         `json:"workspace"`
	ModelID           string         `json:"model_id"`
	Client            Client         `json:"client"`
	PostApplyHint     string         `json:"post_apply_hint,omitempty"`
	ConfigPaths       []string       `json:"config_paths"`
	Changes           []Change       `json:"changes"`
	AlreadyConfigured bool           `json:"already_configured"`
	RequiresReplace   bool           `json:"requires_replace"`
	Authentication    Authentication `json:"authentication"`
}

func (plan Plan) Review() Review {
	return Review{Revision: plan.Revision, ConfigPaths: slices.Clone(plan.ConfigPaths), Changes: slices.Clone(plan.Changes), Authentication: plan.Authentication}
}

type SetupPrompt struct {
	Workspace string `json:"workspace"`
	Prompt    string `json:"prompt"`
}

func (s *Service) Prompt(target clientconnect.Target) (SetupPrompt, error) {
	if err := target.ValidateLocal(); err != nil {
		return SetupPrompt{}, err
	}
	return SetupPrompt{Workspace: target.WorkspaceSlug(), Prompt: clientconnect.OtherClientSetupPrompt(target)}, nil
}

func (s *Service) Discover(ctx context.Context, target clientconnect.Target) ([]Client, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := target.ValidateLocal(); err != nil {
		return nil, err
	}
	clients := s.clients.Discover(ctx, target)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := make([]Client, 0, len(clients))
	for _, client := range clients {
		out = append(out, Client{ID: string(client.ID), Name: client.Name})
	}
	return out, nil
}

func (s *Service) Plan(ctx context.Context, rawClient string, target clientconnect.Target) (Plan, error) {
	plan, _, err := s.prepare(ctx, rawClient, target)
	return plan, err
}

func (s *Service) prepare(ctx context.Context, rawClient string, target clientconnect.Target) (Plan, clientconnect.Plan, error) {
	if err := ctx.Err(); err != nil {
		return Plan{}, clientconnect.Plan{}, err
	}
	if err := target.ValidateLocal(); err != nil {
		return Plan{}, clientconnect.Plan{}, err
	}
	clientID, err := clientconnect.ParseClientID(rawClient)
	if err != nil {
		return Plan{}, clientconnect.Plan{}, err
	}
	revision, err := s.credentials.ClientPermission(target.Principal(), target.WorkspaceSlug())
	if err != nil {
		return Plan{}, clientconnect.Plan{}, errors.Join(ErrModelsPermission, err)
	}
	plan, err := s.clients.Plan(ctx, clientID, target)
	if err != nil {
		return Plan{}, clientconnect.Plan{}, err
	}
	if err := ctx.Err(); err != nil {
		return Plan{}, clientconnect.Plan{}, err
	}
	if plan.ClientID != clientID || plan.Target != target {
		return Plan{}, clientconnect.Plan{}, errors.New("client configuration planner changed requested binding")
	}
	authentication := Authentication{Principal: target.Principal(), PermissionRevision: revision}
	credential := plan.ConfiguredCredential()
	credentialID, credentialErr := s.credentials.ClientCredential(target.Principal(), target.WorkspaceSlug(), credential)
	if credentialErr != nil {
		authentication.IssueCredential = true
	} else {
		authentication.CredentialID = credentialID
		plan = plan.BindCredential(target.Principal(), credential)
	}
	if err := ctx.Err(); err != nil {
		return Plan{}, clientconnect.Plan{}, err
	}
	return projectAuthenticatedPlan(target, plan, authentication), plan, nil
}

func (s *Service) Apply(ctx context.Context, rawClient string, reviewed Review, target clientconnect.Target) (Plan, error) {
	return s.apply(ctx, rawClient, reviewed, target, "", nil)
}

func (s *Service) apply(ctx context.Context, rawClient string, reviewed Review, target clientconnect.Target, issuanceKey string, authorize func() error) (Plan, error) {
	if err := ctx.Err(); err != nil {
		return Plan{}, err
	}
	if err := target.ValidateLocal(); err != nil {
		return Plan{}, err
	}
	clientID, err := clientconnect.ParseClientID(rawClient)
	if err != nil {
		return Plan{}, err
	}
	projected, current, err := s.prepare(ctx, rawClient, target)
	if err != nil {
		return Plan{}, err
	}
	if err := ctx.Err(); err != nil {
		return Plan{}, err
	}
	if reviewed.Revision == "" {
		return Plan{}, ErrReviewRequired
	}
	if reviewed.Revision != projected.Revision || !reviewMatches(reviewed, projected) {
		return projected, ErrReviewChanged
	}
	if projected.Authentication.IssueCredential {
		if issuanceKey == "" || authorize == nil {
			return projected, errors.New("credential provisioning requires a confirmed durable Connect attempt")
		}
		issued, err := s.credentials.IssueClientCredentialChecked(target.Principal(), target.WorkspaceSlug(), issuanceKey, projected.Authentication.PermissionRevision, authorize)
		if err != nil {
			return projected, err
		}
		if issued.SecretNotRecoverable || issued.Token == "" {
			return projected, credentialDeliveryError(target.Principal(), issued.Credential.ID)
		}
		bound := target.WithAuthentication(target.Principal(), issued.Token)
		current, err = s.clients.Plan(ctx, clientID, bound)
		if err != nil {
			return projected, errors.Join(credentialDeliveryError(target.Principal(), issued.Credential.ID), err)
		}
		if current.ClientID != clientID || current.Target != bound || !reviewMatches(reviewed, projectAuthenticatedPlan(target, current, projected.Authentication)) {
			return projected, errors.Join(ErrReviewChanged, credentialDeliveryError(target.Principal(), issued.Credential.ID))
		}
	}
	if authorize != nil {
		if err := authorize(); err != nil {
			return projected, err
		}
	}
	verified, err := s.clients.Apply(ctx, current)
	if verified.ClientID != clientID || verified.Target != current.Target {
		return Plan{}, errors.Join(err, errors.New("client configuration apply readback changed requested binding; effect outcome is uncertain"))
	}
	if err != nil {
		currentProjection := projectAuthenticatedPlan(target, verified, projected.Authentication)
		if errors.Is(err, clientconnect.ErrPlanChanged) {
			return currentProjection, ErrReviewChanged
		}
		return currentProjection, err
	}
	credentialID, err := s.credentials.ClientCredential(target.Principal(), target.WorkspaceSlug(), verified.ConfiguredCredential())
	if err != nil {
		return Plan{}, errors.New("client files were written but Workspace Models authentication was not accepted; inspect the original Connect attempt")
	}
	if !verified.AlreadyConfigured() {
		return Plan{}, errors.New("client configuration did not converge")
	}
	revision, err := s.credentials.ClientPermission(target.Principal(), target.WorkspaceSlug())
	if err != nil {
		return Plan{}, err
	}
	return projectAuthenticatedPlan(target, verified, Authentication{Principal: target.Principal(), PermissionRevision: revision, CredentialID: credentialID}), nil
}

func projectAuthenticatedPlan(target clientconnect.Target, plan clientconnect.Plan, authentication Authentication) Plan {
	result := projectPlan(target, plan)
	result.Authentication = authentication
	result.Revision = reviewRevision(target, plan.ClientID, result.Review())
	return result
}

func projectPlan(target clientconnect.Target, plan clientconnect.Plan) Plan {
	changes := make([]Change, 0, len(plan.Changes))
	for _, change := range plan.Changes {
		changes = append(changes, Change{Field: change.Field, Before: change.Before, BeforeExists: change.BeforeExists, After: change.After})
	}
	return Plan{
		Revision:  reviewRevision(target, plan.ClientID, Review{ConfigPaths: plan.ConfigPaths, Changes: changes}),
		Workspace: target.WorkspaceSlug(), ModelID: target.ModelID(), Client: Client{ID: string(plan.ClientID), Name: plan.ClientName}, PostApplyHint: plan.PostApplyHint,
		ConfigPaths: slices.Clone(plan.ConfigPaths), Changes: changes,
		AlreadyConfigured: plan.AlreadyConfigured(), RequiresReplace: plan.RequiresReplace(),
	}
}

func reviewRevision(target clientconnect.Target, client clientconnect.ClientID, review Review) string {
	encoded, _ := json.Marshal(struct {
		Workspace      string
		ModelID        string
		Address        string
		Client         clientconnect.ClientID
		ConfigPaths    []string
		Changes        []Change
		Authentication Authentication
	}{target.WorkspaceSlug(), target.ModelID(), target.WorkspaceURL(), client, review.ConfigPaths, review.Changes, review.Authentication})
	return fmt.Sprintf("connect-%x", sha256.Sum256(encoded))
}

func reviewMatches(reviewed Review, current Plan) bool {
	return slices.Equal(reviewed.ConfigPaths, current.ConfigPaths) && slices.Equal(reviewed.Changes, current.Changes) && reviewed.Authentication == current.Authentication
}

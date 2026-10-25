package accessstate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/routing"
)

// PermissionRevision returns an opaque revision of one relationship and its
// credential impact, including absence. Sibling edits do not invalidate it.
// Publication retains counters for removed relationships to prevent consent
// resurrection. This is metadata on the natural relation, not a Grant identity.
func (s *Store) PermissionRevision(id accessidentity.ID, workspace routing.WorkspaceSlug) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	index, ok := s.byAccess[id]
	if !ok {
		return "", ErrNotFound
	}
	return permissionRevision(s.state[index], workspace), nil
}

func permissionRevision(access Access, workspace routing.WorkspaceSlug) string {
	impact := permissionRevisionImpact(access, workspace)
	raw := fmt.Appendf(nil, "%s\x00%s\x00%d\x00%s", access.ID.String(), workspace.String(), access.PermissionGenerations[workspace], impact)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// permissionRevisionImpact is the reviewed scope: relationship state, identity
// revocation and affected credential identities. It deliberately excludes label,
// sibling permissions, sibling credentials and receipt metadata.
func permissionRevisionImpact(access Access, workspace routing.WorkspaceSlug) string {
	impact := struct {
		RevokedAt     *time.Time
		Present       bool
		ExpiresAt     time.Time
		Services      []routing.ServiceKind `json:"services,omitempty"`
		RequestLimits []RequestRateLimit    `json:"request_limits,omitempty"`
		Credentials   []string
	}{RevokedAt: access.RevokedAt}
	for _, permission := range access.Permissions {
		if permission.Workspace == workspace {
			impact.Present = true
			impact.ExpiresAt = permission.ExpiresAt.UTC()
			impact.Services = permission.Services
			impact.RequestLimits = permission.RequestLimits
		}
	}
	for _, credential := range access.Credentials {
		if credential.Workspace == workspace {
			impact.Credentials = append(impact.Credentials, credential.ID.String())
		}
	}
	slices.Sort(impact.Credentials)
	raw, _ := json.Marshal(impact)
	return string(raw)
}

func stampPermissionGenerations(previous Access, candidate *Access) error {
	workspaces := make(map[routing.WorkspaceSlug]struct{})
	for _, access := range []Access{previous, *candidate} {
		for workspace := range access.PermissionGenerations {
			workspaces[workspace] = struct{}{}
		}
		for _, permission := range access.Permissions {
			workspaces[permission.Workspace] = struct{}{}
		}
		for _, credential := range access.Credentials {
			workspaces[credential.Workspace] = struct{}{}
		}
	}
	generations := make(map[routing.WorkspaceSlug]uint64, len(workspaces))
	for workspace := range workspaces {
		generation := previous.PermissionGenerations[workspace]
		if permissionRevisionImpact(previous, workspace) != permissionRevisionImpact(*candidate, workspace) {
			if generation == ^uint64(0) {
				return errors.New("permission generation exhausted")
			}
			generation++
		}
		if generation != 0 {
			generations[workspace] = generation
		}
	}
	candidate.PermissionGenerations = generations
	return nil
}

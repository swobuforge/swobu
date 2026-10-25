package workspaces

import "context"

// DeletionPreview is advisory concurrency-bound impact, not an approval or a
// reservation. It deliberately excludes provider connections and credentials.
type DeletionPreview struct {
	Workspace          string `json:"workspace"`
	ManagementETag     string `json:"management_etag"`
	ManagementRevision uint64 `json:"management_revision"`
	RouteCount         int    `json:"route_count"`
	ApprovalClass      string `json:"approval_class"`
	Diff               string `json:"diff"`
	Warning            string `json:"warning"`
}

// PreviewDeletion reads one authoritative snapshot without revoking bindings
// or reserving state. Apply must still check this exact lifetime and revision.
func (s Service) PreviewDeletion(ctx context.Context, slug string) (DeletionPreview, error) {
	workspace, err := s.GetWorkspace(ctx, slug)
	if err != nil {
		return DeletionPreview{}, err
	}
	return DeletionPreview{
		Workspace: workspace.Slug, ManagementETag: workspace.ManagementETag,
		ManagementRevision: workspace.ManagementRevision, RouteCount: len(workspace.Routes),
		ApprovalClass: "high_risk", Diff: "Delete Workspace and routes after independent Access, publication and spending constraints are cleared; retain historical evidence.",
		Warning: "Unpublish the Workspace and clear spending limits first. Deletion revokes its Access credentials before committing the routing change; a failed commit does not restore those credentials.",
	}, nil
}

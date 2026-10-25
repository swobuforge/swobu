package configstore

import (
	"errors"
	"slices"
	"time"

	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/routing"
	"gopkg.in/yaml.v3"
)

// WorkspaceDisplayNameAudit retains a checked presentation change, not slug
// migration or permission. Empty before/after values explicitly mean no label.
type WorkspaceDisplayNameAudit struct {
	Action      string    `yaml:"action" json:"action"`
	Caller      string    `yaml:"caller" json:"caller"`
	Workspace   string    `yaml:"workspace" json:"workspace"`
	ETag        string    `yaml:"etag" json:"etag"`
	Before      string    `yaml:"before" json:"before"`
	After       string    `yaml:"after" json:"after"`
	CommittedAt time.Time `yaml:"committed_at" json:"committed_at"`
}

func decodeDisplayNameAudits(raw []byte) ([]WorkspaceDisplayNameAudit, error) {
	var document yaml.Node
	if err := yaml.Unmarshal(raw, &document); err != nil {
		return nil, err
	}
	if len(document.Content) == 1 {
		root := document.Content[0]
		for index := 0; index+1 < len(root.Content); index += 2 {
			if root.Content[index].Value != "workspace_display_name_audits" {
				continue
			}
			entries := root.Content[index+1]
			if entries.Kind != yaml.SequenceNode {
				return nil, errors.New("display name audits require a sequence")
			}
			for _, entry := range entries.Content {
				if entry.Kind != yaml.MappingNode {
					return nil, errors.New("display name audit requires explicit facts")
				}
				seen := map[string]bool{}
				for field := 0; field+1 < len(entry.Content); field += 2 {
					if entry.Content[field+1].Tag == "!!null" {
						return nil, errors.New("null display name audit fact")
					}
					seen[entry.Content[field].Value] = true
				}
				for _, name := range []string{"action", "caller", "workspace", "etag", "before", "after", "committed_at"} {
					if !seen[name] {
						return nil, errors.New("incomplete display name audit")
					}
				}
			}
		}
	}
	var dto documentDTO
	if err := yaml.Unmarshal(raw, &dto); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, audit := range dto.DisplayNameAudits {
		caller, callerErr := accessidentity.Parse(audit.Caller)
		binding := WorkspaceDeletionAudit{Action: "workspace.delete", Caller: audit.Caller, Workspace: audit.Workspace, ETag: audit.ETag, CommittedAt: audit.CommittedAt}
		if !validWorkspaceDeletionAudit(binding) || audit.Action != "workspace.display_name" || callerErr != nil || caller.String() != audit.Caller || audit.Before == audit.After || seen[audit.ETag] {
			return nil, errors.New("invalid display name audit")
		}
		slug, _ := routing.ParseWorkspaceSlug(audit.Workspace)
		workspace, err := routing.NewWorkspace(slug, nil)
		if err != nil {
			return nil, err
		}
		for _, name := range []string{audit.Before, audit.After} {
			validated, err := workspace.WithDisplayName(name)
			if err != nil || validated.DisplayName() != name {
				return nil, errors.New("noncanonical audited display name")
			}
		}
		seen[audit.ETag] = true
	}
	return dto.DisplayNameAudits, nil
}

// WorkspaceDisplayNameAudits reads detached retained checked changes, including
// deleted Workspaces. Historical/unchecked changes are never reconstructed.
func (store *Store) WorkspaceDisplayNameAudits() ([]WorkspaceDisplayNameAudit, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.state == storeClosed {
		return nil, ErrStoreClosed
	}
	return slices.Clone(store.displayNameAudits), nil
}

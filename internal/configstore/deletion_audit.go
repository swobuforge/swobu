package configstore

import (
	"encoding/hex"
	"errors"
	"reflect"
	"strconv"
	"strings"

	"github.com/swobuforge/swobu/internal/routing"
	"gopkg.in/yaml.v3"
)

func validWorkspaceDeletionAudit(audit WorkspaceDeletionAudit) bool {
	slug, err := routing.ParseWorkspaceSlug(audit.Workspace)
	identity, revision, found := strings.Cut(audit.ETag, "-")
	digest, identityErr := hex.DecodeString(identity)
	number, revisionErr := strconv.ParseUint(revision, 10, 64)
	return err == nil && slug.String() == audit.Workspace && audit.Action == "workspace.delete" && audit.Caller != "" && !audit.CommittedAt.IsZero() && found && identityErr == nil && len(digest) == 32 && hex.EncodeToString(digest) == identity && revisionErr == nil && number > 0 && strconv.FormatUint(number, 10) == revision
}

func decodeWorkspaceDeletionAudits(raw []byte) (map[string]WorkspaceDeletionAudit, error) {
	var document yaml.Node
	if err := yaml.Unmarshal(raw, &document); err != nil {
		return nil, err
	}
	if len(document.Content) == 1 {
		root := document.Content[0]
		for index := 0; index+1 < len(root.Content); index += 2 {
			name, entries := root.Content[index].Value, root.Content[index+1]
			if name != "workspace_deletion_audits" && name != "workspace_deletions" {
				continue
			}
			if entries.Kind != yaml.MappingNode {
				return nil, errors.New("Workspace deletion evidence requires mapping")
			}
			for entry := 1; entry < len(entries.Content); entry += 2 {
				value := entries.Content[entry]
				if value.Kind != yaml.MappingNode {
					return nil, errors.New("Workspace deletion evidence requires explicit facts")
				}
				if name == "workspace_deletions" {
					for field := 0; field+1 < len(value.Content); field += 2 {
						if value.Content[field].Value == "audit" && value.Content[field+1].Kind != yaml.MappingNode {
							return nil, errors.New("Workspace deletion audit cannot be null")
						}
					}
				}
			}
		}
	}
	var dto documentDTO
	if err := yaml.Unmarshal(raw, &dto); err != nil {
		return nil, err
	}
	audits := make(map[string]WorkspaceDeletionAudit, len(dto.DeletionAudits))
	for identity, audit := range dto.DeletionAudits {
		if identity != audit.ETag || !validWorkspaceDeletionAudit(audit) {
			return nil, errors.New("invalid retained Workspace deletion audit")
		}
		audits[identity] = audit
	}
	receipts, err := decodeDeletionReceipts(raw)
	if err != nil {
		return nil, err
	}
	for _, receipt := range receipts {
		if receipt.Audit == nil {
			continue
		}
		audit := *receipt.Audit
		if retained, exists := audits[audit.ETag]; exists && !reflect.DeepEqual(retained, audit) {
			return nil, errors.New("conflicting Workspace deletion audit")
		}
		audits[audit.ETag] = audit
	}
	return audits, nil
}

// WorkspaceDeletionAudits returns detached committed Routing-removal facts,
// independently retained after retry expiry and slug recreation.
func (store *Store) WorkspaceDeletionAudits() ([]WorkspaceDeletionAudit, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.state == storeClosed {
		return nil, ErrStoreClosed
	}
	result := []WorkspaceDeletionAudit{}
	for _, audit := range store.deletionAudits {
		result = append(result, audit)
	}
	return result, nil
}

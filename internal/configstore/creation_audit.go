package configstore

import (
	"encoding/hex"
	"errors"
	"reflect"

	"github.com/swobuforge/swobu/internal/routing"
	"gopkg.in/yaml.v3"
)

func decodeWorkspaceCreationAudits(raw []byte) (map[string]WorkspaceCreationAudit, error) {
	var document yaml.Node
	if err := yaml.Unmarshal(raw, &document); err != nil {
		return nil, err
	}
	if len(document.Content) == 1 {
		root := document.Content[0]
		for index := 0; index+1 < len(root.Content); index += 2 {
			if root.Content[index].Value != "workspace_creation_audits" {
				continue
			}
			entries := root.Content[index+1]
			if entries.Kind != yaml.MappingNode {
				return nil, errors.New("Workspace creation audits require a mapping")
			}
			for entry := 1; entry < len(entries.Content); entry += 2 {
				if entries.Content[entry].Kind != yaml.MappingNode {
					return nil, errors.New("Workspace creation audit requires explicit facts")
				}
			}
		}
	}
	var dto documentDTO
	if err := yaml.Unmarshal(raw, &dto); err != nil {
		return nil, err
	}
	audits := make(map[string]WorkspaceCreationAudit, len(dto.CreationAudits))
	for identity, audit := range dto.CreationAudits {
		slug, scopeErr := routing.ParseWorkspaceSlug(audit.Workspace)
		decoded, identityErr := hex.DecodeString(identity)
		if scopeErr != nil || slug.String() != audit.Workspace || identityErr != nil || len(decoded) != 32 || hex.EncodeToString(decoded) != identity || audit.Incarnation != identity || audit.Caller == "" || audit.Action != "workspace.create" || audit.CommittedAt.IsZero() {
			return nil, errors.New("invalid retained Workspace creation audit")
		}
		audits[identity] = audit
	}
	receipts, err := decodeCreationReceipts(raw)
	if err != nil {
		return nil, err
	}
	for _, receipt := range receipts {
		if receipt.Audit == nil {
			continue
		}
		audit := *receipt.Audit
		if retained, exists := audits[audit.Incarnation]; exists && !reflect.DeepEqual(retained, audit) {
			return nil, errors.New("conflicting Workspace creation audit")
		}
		audits[audit.Incarnation] = audit
	}
	return audits, nil
}

// Package nodeauthority validates one complete, immutable Node desired-state
// snapshot. Its only durable input is swobu.yaml. Domain owners retain their
// codecs; this layer checks references between them without a second index or
// another configuration authority.
package nodeauthority

import (
	"fmt"

	"github.com/swobuforge/swobu/internal/accessstate"
	"github.com/swobuforge/swobu/internal/accounting"
	"github.com/swobuforge/swobu/internal/configstore"
	"github.com/swobuforge/swobu/internal/connections"
	"github.com/swobuforge/swobu/internal/nodefile"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/publications"
	"github.com/swobuforge/swobu/internal/routing"
)

// Summary is a deliberately redacted control projection. Neither credential
// references/digests nor historic operation receipts belong in a Node summary.
type Summary struct {
	Workspaces     int
	Connections    int
	Principals     int
	Publications   int
	BudgetPolicies int
}

// Admit is the commit-time form of Validate. The Node publisher runs it under
// its file mutex, against the complete candidate bytes, before atomic replace.
func Admit(raw []byte) error {
	_, err := Validate(raw)
	return err
}

// Validate is pure: no files, credentials, network or state modifications. A
// caller must supply one complete revision; independently sampling different
// stores can otherwise allow an invalid mixed-generation authority to pass.
func Validate(raw []byte) (Summary, error) {
	var result Summary
	config, err := configstore.ValidateDocument(raw)
	if err != nil {
		return result, fmt.Errorf("routing: %w", err)
	}
	result.Workspaces = config.WorkspaceCount()
	section := func(name string) ([]byte, error) {
		value, err := nodefile.SectionDocument(raw, name)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		return value, nil
	}
	connectionRaw, err := section("connections")
	if err != nil {
		return result, err
	}
	var records []connections.Connection
	if len(connectionRaw) > 0 {
		records, err = connections.ValidateDocument(connectionRaw, profile.SupportsSpec)
		if err != nil {
			return result, fmt.Errorf("connections: %w", err)
		}
	}
	if err := connections.ValidateRoutingReferences(config, records); err != nil {
		return result, fmt.Errorf("Workspace Connection references: %w", err)
	}
	result.Connections = len(records)

	principalRaw, err := section("principals")
	if err != nil {
		return result, err
	}
	principals, err := accessstate.ValidateDocument(principalRaw)
	if err != nil {
		return result, fmt.Errorf("principals: %w", err)
	}
	result.Principals = len(principals)
	for _, principal := range principals {
		for _, permission := range principal.Permissions {
			if _, found := config.Workspace(permission.Workspace); !found {
				return result, fmt.Errorf("principals.%s.permissions.%s: missing Workspace", principal.ID, permission.Workspace)
			}
		}
		for _, credential := range principal.Credentials {
			if _, found := config.Workspace(credential.Workspace); !found {
				return result, fmt.Errorf("principals.%s.credentials.%s: missing Workspace %s", principal.ID, credential.ID, credential.Workspace)
			}
		}
	}

	publicationRaw, err := section("publications")
	if err != nil {
		return result, err
	}
	publications, err := publications.ValidateDocument(publicationRaw)
	if err != nil {
		return result, fmt.Errorf("publications: %w", err)
	}
	result.Publications = len(publications)
	for _, slug := range publications {
		if _, found := config.Workspace(slug); !found {
			return result, fmt.Errorf("publications: missing Workspace %s", slug)
		}
	}

	accountingRaw, err := section("accounting")
	if err != nil {
		return result, err
	}
	limits, err := accounting.ValidatePolicyDocument(accountingRaw)
	if err != nil {
		return result, fmt.Errorf("accounting: %w", err)
	}
	result.BudgetPolicies = len(limits)
	for workspace := range limits {
		slug, err := routing.ParseWorkspaceSlug(workspace)
		if err != nil {
			return result, fmt.Errorf("accounting.limits.%s: %w", workspace, err)
		}
		if _, found := config.Workspace(slug); !found {
			return result, fmt.Errorf("accounting.limits.%s: missing Workspace", workspace)
		}
	}
	return result, nil
}

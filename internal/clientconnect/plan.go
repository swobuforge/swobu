package clientconnect

import (
	"fmt"
	"slices"
)

// ClientID is the closed automatic-wiring client set shared by Cockpit and CLI.
type ClientID string

// AutomaticClientIDs returns the canonical CLI spellings in presentation
// order. Callers receive a copy so clientconnect remains the sole authority.
func AutomaticClientIDs() []ClientID {
	ids := make([]ClientID, 0, len(adapters))
	for _, adapter := range adapters {
		ids = append(ids, adapter.id)
	}
	return ids
}

// ParseClientID accepts only a canonical automatic-client spelling.
func ParseClientID(raw string) (ClientID, error) {
	adapter, ok := adapterFor(ClientID(raw))
	if !ok {
		return "", fmt.Errorf("unsupported client %q", raw)
	}
	return adapter.id, nil
}

// Client describes one locally discoverable named integration.
type Client struct {
	ID   ClientID
	Name string
}

// Change is one reviewed, Swobu-owned semantic leaf in a client mutation.
type Change struct {
	Field        string
	Before       string
	After        string
	BeforeExists bool
}

// Plan is inert reviewed evidence for one semantic client-backend operation.
// Changes is the sole mutation truth used by preview, replacement admission,
// freshness comparison, and Apply.
type Plan struct {
	ClientID   ClientID
	ClientName string
	ConfigPath string
	Target     Target
	Changes    []Change
}

func semanticChange(field, before string, exists bool, after string) []Change {
	if exists && before == after {
		return nil
	}
	return []Change{{Field: field, Before: before, After: after, BeforeExists: exists}}
}

// AlreadyConfigured reports that inspection found the intended binding and no
// write is necessary.
func (p Plan) AlreadyConfigured() bool { return len(p.Changes) == 0 }

// RequiresReplace reports that at least one reviewed mutation replaces an
// existing client configuration value.
func (p Plan) RequiresReplace() bool {
	for _, change := range p.Changes {
		if change.BeforeExists && change.Before != change.After {
			return true
		}
	}
	return false
}

func (p Plan) equal(other Plan) bool {
	return p.ClientID == other.ClientID && p.ClientName == other.ClientName &&
		p.ConfigPath == other.ConfigPath && p.Target == other.Target &&
		slices.Equal(p.Changes, other.Changes)
}

func (p Plan) withClient(adapter adapter) Plan {
	p.ClientID = adapter.id
	p.ClientName = adapter.name
	return p
}

type plannedMutation struct {
	plan  Plan
	apply func() error
}

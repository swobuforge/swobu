package clientconnect

import (
	"crypto/sha256"
	"fmt"
)

// WithAuthentication binds an existing Principal and an ephemeral credential to
// this destination. Only Access may establish its authority. An empty bearer is
// useful for pure provisioning preview and can never be applied.
func (t Target) WithAuthentication(principal, bearer string) Target {
	t.principal, t.bearer = principal, bearer
	return t
}

func (t Target) Principal() string { return t.principal }

// GoString prevents diagnostic formatting of a target from disclosing a token.
func (t Target) GoString() string {
	return fmt.Sprintf("Target{Workspace:%q, URL:%q, Model:%q, Principal:%q}", t.workspaceSlug, t.workspaceURL, t.modelID, t.principal)
}

func (t Target) String() string { return t.GoString() }

// credentialChange retains an exact opaque before-value for freshness, without
// putting credential material in review, logs, or durable Connect intent. The
// after-value stays stable across preview and one-shot credential issuance.
func credentialChange(current string, exists bool, target Target) []Change {
	if target.bearer != "" && current == target.bearer {
		return nil
	}
	before := ""
	if exists {
		before = fmt.Sprintf("credential fingerprint %x", sha256.Sum256([]byte(current)))
	}
	return []Change{{Field: "authentication", Before: before, BeforeExists: exists, After: "Workspace credential for Principal " + target.principal}}
}

// ConfiguredCredential is transient adapter evidence for Access validation. It
// must never be projected to a consumer or persisted in a Connect attempt.
func (p Plan) ConfiguredCredential() string { return p.credential }

// BindCredential attaches the credential Access just accepted to inert file
// evidence. Apply still re-inspects every owned leaf before any mutation. This
// avoids a second preview inspection, which could mix two foreign snapshots.
func (p Plan) BindCredential(principal, bearer string) Plan {
	p.Target = p.Target.WithAuthentication(principal, bearer)
	if bearer != "" && p.credential == bearer {
		changes := make([]Change, 0, len(p.Changes))
		for _, change := range p.Changes {
			if change.Field != "authentication" {
				changes = append(changes, change)
			}
		}
		p.Changes = changes
	}
	return p
}

func (p Plan) GoString() string {
	return fmt.Sprintf("Plan{Client:%q, Target:%s, Paths:%q, Changes:%#v}", p.ClientID, p.Target, p.ConfigPaths, p.Changes)
}

func (p Plan) String() string { return p.GoString() }

package search

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/swobuforge/swobu/internal/connections"
	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/domain/credentialref"
)

// ConnectionBinding freezes one reusable Search upstream and credential binding
// for an invocation. A later registry edit cannot change its execution identity.
// Construct a fresh binding from the current Connection for each new invocation;
// its ID/revision are the facts the evidence owner must record for that attempt.
type ConnectionBinding struct {
	id       connectionidentity.ID
	revision connections.Revision
	source   JSONSource
}

func (b ConnectionBinding) ConnectionID() connectionidentity.ID      { return b.id }
func (b ConnectionBinding) ConnectionRevision() connections.Revision { return b.revision }

// OpenConnectionBinding resolves only the captured Connection's exact credential
// under Connection scope. It performs no discovery or query. Endpoint/query and
// response semantics remain owned by JSONSource. Search endpoints reject fixed
// query parameters; resolver errors are sanitized at the runtime boundary.
func OpenConnectionBinding(ctx context.Context, value connections.Connection, resolve ConnectionCredentialResolver, client *http.Client) (ConnectionBinding, error) {
	if err := ctx.Err(); err != nil {
		return ConnectionBinding{}, err
	}
	if _, err := connections.Restore(value.ID(), value.DisplayName(), value.Spec(), value.Revision(), value.CreatedAt(), value.UpdatedAt()); err != nil {
		return ConnectionBinding{}, ErrNotConfigured
	}
	spec, ok := value.Spec().(connections.SearchSpec)
	if !ok {
		return ConnectionBinding{}, errors.New("Search Connection required")
	}
	source, err := NewJSONSource(spec.Endpoint().String(), client)
	if err != nil {
		return ConnectionBinding{}, ErrNotConfigured
	}
	if spec.Credential().Kind() != credentialref.KindEmpty {
		if resolve == nil {
			return ConnectionBinding{}, errors.New("Search Connection credential unavailable")
		}
		scope, err := credentialref.NewConnectionScope(value.ID().String())
		if err != nil {
			return ConnectionBinding{}, ErrNotConfigured
		}
		token, err := resolve(ctx, scope, spec.Credential().String())
		if err := ctx.Err(); err != nil {
			return ConnectionBinding{}, err
		}
		if err != nil || strings.TrimSpace(token) == "" {
			return ConnectionBinding{}, errors.New("Search Connection credential unavailable")
		}
		source = source.WithBearer(token)
	}
	return ConnectionBinding{id: value.ID(), revision: value.Revision(), source: source}, nil
}

func (b ConnectionBinding) Search(ctx context.Context, query Query) ([]Result, error) {
	if b.id.String() == "" {
		return nil, ErrNotConfigured
	}
	return b.source.Search(ctx, query)
}

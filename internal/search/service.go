package search

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/swobuforge/swobu/internal/accounting"
	"github.com/swobuforge/swobu/internal/connections"
	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
)

var ErrNotConfigured = errors.New("search operation is not configured")

type Query struct {
	Text  string `json:"query"`
	Limit int    `json:"limit"`
}

type Result struct {
	URL         string `json:"url"`
	Title       string `json:"title"`
	Snippet     string `json:"snippet"`
	Source      string `json:"source"`
	RetrievedAt string `json:"retrieved_at,omitempty"`
}

// Evidence is the content-free lifecycle fact for a direct Search operation.
// Query text and result bodies are intentionally excluded; the source identity
// and workspace are sufficient for operational attribution.
type Evidence struct {
	Operation string
	Workspace string
	Source    string
	// Production bindings carry exact Connection identity and revision.
	// In-memory test sources may be explicitly unbound.
	ConnectionID       string
	ConnectionRevision string
	StartedAt          time.Time
	EndedAt            time.Time
	Outcome            string
	Uncertain          bool
}

type connectionIdentity interface {
	ConnectionID() connectionidentity.ID
	ConnectionRevision() connections.Revision
}

type Source interface {
	Search(context.Context, Query) ([]Result, error)
}

type Service struct {
	// Sources is keyed by upstream identity. Workspace selection always belongs
	// to the current configuration; a missing resolver cannot silently treat a
	// Workspace name as a source ID.
	Sources map[string]Source
	// ConnectionIDs owns the ordered Workspace route from the current publication.
	ConnectionIDs func(string) ([]string, bool)
	// SourceFor resolves the current Connection revision at each attempt. In-flight
	// requests hold the resulting immutable Source across subsequent edits.
	SourceFor  func(context.Context, string) (Source, error)
	Accounting *accounting.Store
	CostCents  int64
	KnownPrice bool
	Evidence   func(Evidence)
}

func (s Service) Search(ctx context.Context, workspace string, query Query) ([]Result, error) {
	if strings.TrimSpace(workspace) == "" {
		return nil, ErrNotConfigured
	}
	var ids []string
	if s.ConnectionIDs != nil {
		resolved, ok := s.ConnectionIDs(workspace)
		if !ok {
			return nil, ErrNotConfigured
		}
		ids = resolved
	}
	if len(ids) == 0 {
		return nil, ErrNotConfigured
	}
	query.Text = strings.TrimSpace(query.Text)
	if query.Text == "" || len(query.Text) > 2048 {
		return nil, errors.New("search query must be 1-2048 characters")
	}
	if query.Limit == 0 {
		query.Limit = 10
	}
	if query.Limit < 1 || query.Limit > 50 {
		return nil, errors.New("search limit must be 1-50")
	}

	var last error
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		result, err, mayFallback := s.attempt(ctx, workspace, id, query)
		if err == nil {
			return result, nil
		} // Successful empty result is still final.
		if !mayFallback {
			return nil, err
		}
		last = err
	}
	if last == nil {
		return nil, ErrNotConfigured
	}
	return nil, fmt.Errorf("all Search upstream attempts failed: %w", last)
}

// attempt resolves one candidate and accounts only an actual upstream call.
// A failed accounting write or admission never advances to a second provider.
func (s Service) attempt(ctx context.Context, workspace, id string, query Query) ([]Result, error, bool) {
	var source Source
	if s.SourceFor != nil {
		resolved, err := s.SourceFor(ctx, id)
		// Credential resolution can outlive cancellation. Never reserve or dispatch
		// cancelled work, and preserve cancellation rather than a binding error.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr, false
		}
		if err != nil {
			return nil, ErrNotConfigured, true
		}
		source = resolved
	} else {
		source = s.Sources[id]
	}
	if source == nil {
		return nil, ErrNotConfigured, true
	}
	connectionID, connectionRevision := "", ""
	if bound, ok := source.(connectionIdentity); ok {
		connectionID = bound.ConnectionID().String()
		connectionRevision = bound.ConnectionRevision().String()
	}
	if strings.HasPrefix(id, "cn_") && (connectionID != id || connectionRevision == "") {
		return nil, ErrNotConfigured, true
	}
	operation, err := searchOperationID(workspace)
	if err != nil {
		return nil, err, false
	}
	reservationWindow := ""
	if s.Accounting != nil {
		reservation, err := s.Accounting.Reserve(workspace, operation, s.CostCents, s.KnownPrice)
		if err != nil {
			return nil, err, false
		}
		reservationWindow = reservation.WindowKey
	}
	startedAt := time.Now().UTC()
	if s.Evidence != nil {
		s.Evidence(Evidence{Operation: operation, Workspace: workspace, Source: id, ConnectionID: connectionID, ConnectionRevision: connectionRevision, StartedAt: startedAt, Outcome: "started"})
	}
	results, searchErr := source.Search(ctx, query)
	uncertain := searchErr != nil
	var settleErr error
	if s.Accounting != nil {
		if reservationWindow != "" {
			settleErr = s.Accounting.SettleReservation(workspace, reservationWindow, operation, s.CostCents, uncertain)
		} else {
			settleErr = s.Accounting.Settle(workspace, operation, s.CostCents, uncertain)
		}
		if settleErr != nil && searchErr == nil {
			searchErr = settleErr
			results = nil
			uncertain = true
		}
	}
	if s.Evidence != nil {
		outcome := "succeeded"
		if searchErr != nil {
			outcome = "failed"
		}
		s.Evidence(Evidence{Operation: operation, Workspace: workspace, Source: id, ConnectionID: connectionID, ConnectionRevision: connectionRevision, StartedAt: startedAt, EndedAt: time.Now().UTC(), Outcome: outcome, Uncertain: uncertain})
	}
	if settleErr != nil {
		return nil, errors.Join(searchErr, settleErr), false
	}
	if err := ctx.Err(); err != nil {
		return nil, err, false
	}
	return results, searchErr, searchErr != nil
}

func searchOperationID(workspace string) (string, error) {
	raw := make([]byte, 12)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("search operation identity: %w", err)
	}
	return "search:" + strings.TrimSpace(workspace) + ":" + hex.EncodeToString(raw), nil
}

// StaticSource is a deterministic fixture/source useful for local qualification.
type StaticSource []Result

func (s StaticSource) Search(_ context.Context, q Query) ([]Result, error) {
	out := make([]Result, 0, q.Limit)
	needle := strings.ToLower(q.Text)
	for _, result := range s {
		if strings.Contains(strings.ToLower(result.Title+" "+result.Snippet), needle) {
			out = append(out, result)
		}
		if len(out) == q.Limit {
			break
		}
	}
	return out, nil
}

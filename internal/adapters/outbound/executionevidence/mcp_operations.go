package executionevidence

import (
	"crypto/rand"
	"errors"
	"sort"
	"time"

	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/domain/execution"
	domain "github.com/swobuforge/swobu/internal/domain/executionevidence"
	"github.com/swobuforge/swobu/internal/routing"
)

// MCPOperation is a content-free direct Workspace call, not an LLM Exchange.
// Source identity is the persisted binding ID, never its display label.
type MCPOperation struct {
	OperationID string               `json:"operation_id"`
	AccessID    string               `json:"access_id"`
	Workspace   string               `json:"workspace"`
	Source      string               `json:"source"`
	Tool        string               `json:"tool"`
	StartedAt   time.Time            `json:"started_at"`
	SettledAt   time.Time            `json:"settled_at,omitzero"`
	Outcome     execution.Outcome    `json:"outcome,omitempty"`
	Occurrence  execution.Occurrence `json:"execution_possibility,omitempty"`
}

// BeginMCP records dispatch after catalog and authority validation. Active
// operations are never evicted by the settled-history retention limit.
func (s *Store) BeginMCP(access accessidentity.ID, workspace routing.WorkspaceSlug, source, tool string) (string, error) {
	if s == nil || !access.Valid() || workspace.String() == "" || source == "" || tool == "" {
		return "", errors.New("direct MCP operation requires authority and source identity")
	}
	identity := "mcp_" + rand.Text()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.mcpActive == nil {
		s.mcpActive = make(map[string]MCPOperation)
	}
	s.mcpActive[identity] = MCPOperation{OperationID: identity, AccessID: access.String(), Workspace: workspace.String(), Source: source, Tool: tool, StartedAt: time.Now().UTC()}
	return identity, nil
}

// SettleMCP preserves transport uncertainty independently of client delivery.
// An error after source dispatch cannot establish that no effect occurred.
// Success requires observed execution, matching canonical Invocation settlement;
// rejected outcomes leave the active operation available for truthful settlement.
func (s *Store) SettleMCP(identity string, outcome execution.Outcome, occurrence execution.Occurrence) error {
	if s == nil || !outcome.Valid() || !occurrence.Valid() {
		return errors.New("invalid direct MCP settlement")
	}
	if outcome == execution.Succeeded && occurrence != execution.Occurred {
		return errors.New("successful direct MCP settlement requires observed execution")
	}
	s.mu.Lock()
	record, ok := s.mcpActive[identity]
	if !ok {
		s.mu.Unlock()
		return errors.New("direct MCP settlement requires an active operation")
	}
	record.SettledAt = time.Now().UTC()
	record.Outcome, record.Occurrence = outcome, occurrence
	access, err := accessidentity.Parse(record.AccessID)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	workspace, err := routing.ParseWorkspaceSlug(record.Workspace)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	completed, err := domain.NewCompletedMCPOperation(record.OperationID, access, workspace, record.Source, record.Tool, record.StartedAt, record.SettledAt, record.Outcome, record.Occurrence)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	delete(s.mcpActive, identity)
	s.mcpSettled = append(s.mcpSettled, record)
	if len(s.mcpSettled) > s.settledLimit {
		s.mcpSettled = append([]MCPOperation(nil), s.mcpSettled[len(s.mcpSettled)-s.settledLimit:]...)
	}
	s.mu.Unlock()
	if s.offerMCP != nil {
		s.offerMCP(completed)
	}
	return nil
}

// MCPOperation reads one exact standalone identity without fabricating model
// traffic. Missing process-local evidence is not a claim of no execution.
func (s *Store) MCPOperation(identity string) (MCPOperation, bool) {
	if s == nil || identity == "" {
		return MCPOperation{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if operation, found := s.mcpActive[identity]; found {
		return operation, true
	}
	for _, operation := range s.mcpSettled {
		if operation.OperationID == identity {
			return operation, true
		}
	}
	return MCPOperation{}, false
}

func (s *Store) mcpOperations(scope ProjectionScope) []MCPOperation {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]MCPOperation, 0, len(s.mcpActive)+len(s.mcpSettled))
	for _, record := range s.mcpActive {
		if scope.Kind != ProjectionScopeWorkspace || record.Workspace == scope.Workspace {
			result = append(result, record)
		}
	}
	for _, record := range s.mcpSettled {
		if scope.Kind != ProjectionScopeWorkspace || record.Workspace == scope.Workspace {
			result = append(result, record)
		}
	}
	sort.Slice(result, func(first, second int) bool { return result[first].StartedAt.After(result[second].StartedAt) })
	return result
}

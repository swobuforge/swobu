package executionevidence

import (
	"errors"
	"time"

	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/domain/execution"
	"github.com/swobuforge/swobu/internal/routing"
)

type CompletedMCPOperation struct {
	id         string
	access     accessidentity.ID
	workspace  routing.WorkspaceSlug
	source     string
	tool       string
	started    time.Time
	settled    time.Time
	outcome    execution.Outcome
	occurrence execution.Occurrence
}

func NewCompletedMCPOperation(id string, access accessidentity.ID, workspace routing.WorkspaceSlug, source, tool string, started, settled time.Time, outcome execution.Outcome, occurrence execution.Occurrence) (CompletedMCPOperation, error) {
	if id == "" || !access.Valid() || workspace.String() == "" || source == "" || tool == "" || started.IsZero() || settled.IsZero() || settled.Before(started) || !outcome.Valid() || !occurrence.Valid() {
		return CompletedMCPOperation{}, errors.New("completed MCP operation requires identity, authority, source and valid settlement")
	}
	if outcome == execution.Succeeded && occurrence != execution.Occurred {
		return CompletedMCPOperation{}, errors.New("successful MCP operation requires observed execution")
	}
	return CompletedMCPOperation{id, access, workspace, source, tool, started.UTC(), settled.UTC(), outcome, occurrence}, nil
}

func (operation CompletedMCPOperation) ID() string                       { return operation.id }
func (operation CompletedMCPOperation) AccessID() accessidentity.ID      { return operation.access }
func (operation CompletedMCPOperation) Workspace() routing.WorkspaceSlug { return operation.workspace }
func (operation CompletedMCPOperation) Source() string                   { return operation.source }
func (operation CompletedMCPOperation) Tool() string                     { return operation.tool }
func (operation CompletedMCPOperation) StartedAt() time.Time             { return operation.started }
func (operation CompletedMCPOperation) SettledAt() time.Time             { return operation.settled }
func (operation CompletedMCPOperation) Outcome() execution.Outcome       { return operation.outcome }
func (operation CompletedMCPOperation) Occurrence() execution.Occurrence { return operation.occurrence }

func (operation CompletedMCPOperation) Validate() error {
	_, err := NewCompletedMCPOperation(operation.id, operation.access, operation.workspace, operation.source, operation.tool, operation.started, operation.settled, operation.outcome, operation.occurrence)
	return err
}

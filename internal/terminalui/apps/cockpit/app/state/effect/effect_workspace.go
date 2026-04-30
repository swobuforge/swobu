package effect

import (
	"context"
	"strings"
	"time"

	"github.com/metrofun/swobu/internal/domain/endpointintent"
	stateModel "github.com/metrofun/swobu/internal/terminalui/apps/cockpit/app/state/model"
	"github.com/metrofun/swobu/internal/terminalui/engine/retained/update"
)

// SaveWorkspaceNameEffect renames an existing workspace through the daemon.
type SaveWorkspaceNameEffect struct {
	CurrentName string
	Name        string
}

func (cmd SaveWorkspaceNameEffect) Execute(ctx context.Context) []update.Action {
	currentName := strings.TrimSpace(cmd.CurrentName)
	nextName := strings.TrimSpace(cmd.Name)
	if currentName == "" || nextName == "" {
		return []update.Action{WorkspaceSaveFailed{Message: "endpoint rename requires current and next names"}}
	}
	if currentName == nextName {
		return []update.Action{WorkspaceSaveSucceeded{PreviousName: currentName, Name: nextName}}
	}
	c := operatorClient()
	ep, err := c.Get(ctx, currentName)
	if err != nil {
		return []update.Action{WorkspaceSaveFailed{Message: normalizeOperatorSurfaceError(err)}}
	}
	newName, err := endpointintent.ParseEndpointName(nextName)
	if err != nil {
		return []update.Action{WorkspaceSaveFailed{Message: err.Error()}}
	}
	newEp, err := endpointintent.NewEndpoint(newName, ep.ProviderConfigs(), ep.SelectedProviderConfigRef())
	if err != nil {
		return []update.Action{WorkspaceSaveFailed{Message: err.Error()}}
	}
	if _, err := c.Put(ctx, newEp); err != nil {
		return []update.Action{WorkspaceSaveFailed{Message: normalizeOperatorSurfaceError(err)}}
	}
	if err := c.Delete(ctx, currentName); err != nil {
		return []update.Action{WorkspaceSaveFailed{Message: normalizeOperatorSurfaceError(err)}}
	}
	return []update.Action{WorkspaceSaveSucceeded{PreviousName: currentName, Name: nextName}}
}

// SaveNewWorkspaceEffect creates a new workspace through the daemon.
type SaveNewWorkspaceEffect struct {
	Name           string
	ProviderConfig stateModel.ProviderConfigSnapshot
}

func (cmd SaveNewWorkspaceEffect) Execute(ctx context.Context) []update.Action {
	name := strings.TrimSpace(cmd.Name)
	if name == "" {
		return []update.Action{WorkspaceSaveFailed{Message: "workspace create requires name"}}
	}
	parsedName, err := endpointintent.ParseEndpointName(name)
	if err != nil {
		return []update.Action{WorkspaceSaveFailed{Message: err.Error()}}
	}
	pc, err := argsToProviderConfig(cmd.ProviderConfig)
	if err != nil {
		return []update.Action{WorkspaceSaveFailed{Message: err.Error()}}
	}
	ep, err := endpointintent.NewEndpoint(parsedName, []endpointintent.ProviderConfig{pc}, pc.Ref())
	if err != nil {
		return []update.Action{WorkspaceSaveFailed{Message: err.Error()}}
	}
	c := operatorClient()
	if _, err := c.Put(ctx, ep); err != nil {
		return []update.Action{WorkspaceSaveFailed{Message: normalizeOperatorSurfaceError(err)}}
	}
	// Keep busy-save visible for one render so the transition screen is observable.
	time.Sleep(150 * time.Millisecond)
	return []update.Action{WorkspaceSaveSucceeded{PreviousName: "", Name: name}}
}

// WorkspaceSaveFailed reports that a workspace save operation failed.
type WorkspaceSaveFailed struct{ Message string }

// WorkspaceSaveSucceeded reports that a workspace save operation succeeded.
type WorkspaceSaveSucceeded struct {
	PreviousName string
	Name         string
}

// DeleteWorkspaceEffect deletes an existing workspace through the daemon.
type DeleteWorkspaceEffect struct {
	Name string
}

func (cmd DeleteWorkspaceEffect) Execute(ctx context.Context) []update.Action {
	name := strings.TrimSpace(cmd.Name)
	if name == "" {
		return []update.Action{WorkspaceDeleteFailed{Message: "workspace delete requires name"}}
	}
	c := operatorClient()
	if err := c.Delete(ctx, name); err != nil {
		return []update.Action{WorkspaceDeleteFailed{Message: normalizeOperatorSurfaceError(err)}}
	}
	return []update.Action{WorkspaceDeleteSucceeded{Name: name}}
}

// WorkspaceDeleteFailed reports that workspace deletion failed.
type WorkspaceDeleteFailed struct{ Message string }

// WorkspaceDeleteSucceeded reports that workspace deletion succeeded.
type WorkspaceDeleteSucceeded struct{ Name string }

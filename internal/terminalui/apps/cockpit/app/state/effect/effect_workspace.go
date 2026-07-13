package effect

import (
	"context"
	"strings"
	"time"

	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	stateModel "github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state/model"
)

const workspaceCreateSaveTimeout = 60 * time.Second

// SaveWorkspaceNameEffect renames an existing workspace through the daemon.
type SaveWorkspaceNameEffect struct {
	CurrentName string
	Name        string
}

func (cmd SaveWorkspaceNameEffect) Run(ctx context.Context) any {
	currentName := strings.TrimSpace(cmd.CurrentName) // swobu:io-string source=boundary
	nextName := strings.TrimSpace(cmd.Name)           // swobu:io-string source=boundary
	if currentName == "" || nextName == "" {
		return WorkspaceSaveFailed{Message: "endpoint rename requires current and next names"}
	}
	if currentName == nextName {
		return WorkspaceSaveSucceeded{PreviousName: currentName, Name: nextName}
	}
	c := operatorClient()
	ep, err := c.Get(ctx, currentName)
	if err != nil {
		return WorkspaceSaveFailed{Message: normalizeOperatorSurfaceError(err)}
	}
	newName, err := endpointintent.ParseEndpointName(nextName)
	if err != nil {
		return WorkspaceSaveFailed{Message: err.Error()}
	}
	newEp, err := endpointintent.NewEndpoint(newName, ep.ProviderConfigs(), ep.SelectedProviderConfigRef())
	if err != nil {
		return WorkspaceSaveFailed{Message: err.Error()}
	}
	if _, err := c.Put(ctx, newEp); err != nil {
		return WorkspaceSaveFailed{Message: normalizeOperatorSurfaceError(err)}
	}
	if err := c.Delete(ctx, currentName); err != nil {
		return WorkspaceSaveFailed{Message: normalizeOperatorSurfaceError(err)}
	}
	return WorkspaceSaveSucceeded{PreviousName: currentName, Name: nextName}
}

// SaveNewWorkspaceEffect creates a new workspace through the daemon.
type SaveNewWorkspaceEffect struct {
	Name           string
	ProviderConfig stateModel.ProviderConfigSnapshot
}

func (cmd SaveNewWorkspaceEffect) Run(ctx context.Context) any {
	name := strings.TrimSpace(cmd.Name) // swobu:io-string source=boundary
	if name == "" {
		return WorkspaceSaveFailed{Message: "workspace create requires name"}
	}
	parsedName, err := endpointintent.ParseEndpointName(name)
	if err != nil {
		return WorkspaceSaveFailed{Message: err.Error()}
	}
	pc, err := argsToProviderConfig(cmd.ProviderConfig)
	if err != nil {
		return WorkspaceSaveFailed{Message: err.Error()}
	}
	ep, err := endpointintent.NewEndpoint(parsedName, []endpointintent.ProviderConfig{pc}, pc.Ref())
	if err != nil {
		return WorkspaceSaveFailed{Message: err.Error()}
	}
	// Create can include daemon-side model-aware protocol resolution and probe
	// retries, so it uses a longer wait budget than the generic daemon GET path.
	c := operatorClientWithTimeout(workspaceCreateSaveTimeout)
	if _, err := c.Put(ctx, ep); err != nil {
		return WorkspaceSaveFailed{Message: normalizeOperatorSurfaceError(err)}
	}
	// Keep busy-save visible for one render so the transition screen is observable.
	time.Sleep(150 * time.Millisecond)
	return WorkspaceSaveSucceeded{PreviousName: "", Name: name}
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

func (cmd DeleteWorkspaceEffect) Run(ctx context.Context) any {
	name := strings.TrimSpace(cmd.Name) // swobu:io-string source=boundary
	if name == "" {
		return WorkspaceDeleteFailed{Message: "workspace delete requires name"}
	}
	c := operatorClient()
	if err := c.Delete(ctx, name); err != nil {
		return WorkspaceDeleteFailed{Message: normalizeOperatorSurfaceError(err)}
	}
	return WorkspaceDeleteSucceeded{Name: name}
}

// WorkspaceDeleteFailed reports that workspace deletion failed.
type WorkspaceDeleteFailed struct{ Message string }

// WorkspaceDeleteSucceeded reports that workspace deletion succeeded.
type WorkspaceDeleteSucceeded struct{ Name string }

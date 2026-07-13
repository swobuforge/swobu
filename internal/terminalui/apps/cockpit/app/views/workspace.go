// Workspace section: name editing and endpoint preview.
package views

import (
	"strings"

	"github.com/swobuforge/swobu/internal/domain/credentialref"
	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/selectors"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/components/compound"
	"github.com/swobuforge/swobu/internal/terminalui/core"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	toolkitviews "github.com/swobuforge/swobu/internal/terminalui/toolkit/views"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

// BuildWorkspaceSection composes the workspace section rows (retained bridge).
// TODO(v2-migration): full migration of inline editor to core.Node once
// core.Input supports commit/cancel intent routing.
func BuildWorkspaceSection(ctx *retained.Context[state.Model]) retained.ViewSpec[state.Model] {
	model := ctx.Model()
	if model.WorkspaceEditing {
		return buildWorkspaceEditorRetained(ctx)
	}
	return CoreNodeAsRetained(BuildWorkspaceSectionNode(model))
}

// BuildWorkspaceSectionNode returns the canonical core.Node workspace section
// for the non-editing path.
func BuildWorkspaceSectionNode(model state.Model) core.Node[state.Action] {
	current := strings.TrimSpace(model.CurrentEndpoint) // swobu:io-string source=boundary
	if current == "" {
		return buildWorkspaceCreateNode(model)
	}
	return buildWorkspaceCurrentNode(model, current)
}

func buildWorkspaceEditorRetained(ctx *retained.Context[state.Model]) retained.ViewSpec[state.Model] {
	model := ctx.Model()
	current := model.CurrentEndpoint
	isCreate := current == ""
	currentValue := current
	if isCreate {
		currentValue = selectors.CreateDraftName(model)
	}
	draft := model.WorkspaceDraft
	if draft == "" {
		draft = currentValue
	}

	editor := InlineEditor(
		RowName, draft, "choose a workspace name",
		func(_ string) []update.Action { return nil }, // onChange — controlled by key events; draft updated on commit
		func(value string) []update.Action {
			if isCreate {
				parsed, message := validateCreateDraftWorkspaceName(value, model.Endpoints)
				if message != "" {
					return []update.Action{state.SetWorkspaceErrMsg{Message: message}}
				}
				return []update.Action{
					state.SetWorkspaceEditing{Editing: false},
					state.SetCreateDraftName{Name: parsed},
					state.SetInteractionMode{Mode: state.InteractionModeNAV},
				}
			}
			parsed, message := validateWorkspaceName(value, model.Endpoints, current)
			if message != "" {
				return []update.Action{state.SetWorkspaceErrMsg{Message: message}}
			}
			return []update.Action{
				state.SetWorkspaceEditing{Editing: false},
				state.WorkspaceRenameRequested{CurrentName: current, Name: parsed},
			}
		},
		func() []update.Action {
			return []update.Action{
				state.SetWorkspaceEditing{Editing: false},
				state.SetWorkspaceDraft{Draft: currentValue},
				state.SetInteractionMode{Mode: state.InteractionModeNAV},
			}
		},
	)
	if model.WorkspaceErrMsg != "" {
		return toolkitviews.NewAnchoredDisclosure(editor, retained.Named[state.Model]("name-error", SettingStaticRow("", "-> "+model.WorkspaceErrMsg)))
	}
	return editor
}

func buildWorkspaceCurrentNode(model state.Model, current string) core.Node[state.Action] {
	endpoint := selectors.ClientBaseURL(model)
	endpointSummary := selectors.EmptyOr(endpoint, "none")
	if strings.TrimSpace(endpointSummary) == "" { // swobu:io-string source=boundary
		endpointSummary = "none"
	}
	nameValue := current
	nameRow := workspaceEditRowNode(nameValue, model.HeaderStatus == "saved")
	if message := strings.TrimSpace(model.WorkspaceSaveError); message != "" {
		nameRow = workspaceDetailNode("save-error", nameRow, message)
	}

	endpointRow := SettingActionRowNode(
		core.K("workspace/endpoint"),
		RowEndpoint,
		endpointSummary,
		"copy",
		state.EndpointCopyRequested{Value: endpoint},
		endpoint == "" || endpoint == "not set" || endpoint == "invalid",
	)
	if message := strings.TrimSpace(model.WorkspaceCopyNote); message != "" {
		endpointRow = workspaceDetailNode("copy", endpointRow, message)
	}

	rows := []core.Node[state.Action]{
		nameRow,
		endpointRow,
	}
	if model.HeaderStatus == "saved" {
		rows = append(rows, settingStaticRowNode("delete workspace", ""))
	} else {
		rows = append(rows, workspaceDeleteRowNode(current))
	}
	return compound.SectionNode[state.Action]("workspace", rows...)
}

func buildWorkspaceCreateNode(model state.Model) core.Node[state.Action] {
	endpoint := selectors.CreateDraftEndpointValue(model)
	nameValue := selectors.EmptyOr(currentCreateName(model), "choose a workspace name")
	nameRow := workspaceEditRowNode(nameValue, false)
	if message := strings.TrimSpace(model.WorkspaceSaveError); message != "" && selectors.InteractionMode(model) != state.InteractionModeBusySave {
		nameRow = workspaceDetailNode("save-error", nameRow, message)
	}

	rows := []core.Node[state.Action]{
		nameRow,
		settingStaticRowNode(RowEndpoint, selectors.EmptyOr(endpoint, "none")),
	}
	if selectors.InteractionMode(model) == state.InteractionModeBusySave {
		rows[0] = settingStaticRowNode(RowName, selectors.EmptyOr(currentCreateName(model), "choose a workspace name"))
	}
	return compound.SectionNode[state.Action]("workspace", rows...)
}

func workspaceEditRowNode(value string, disabled bool) core.Node[state.Action] {
	if disabled {
		return SettingActionRowNode(
			core.K("workspace/name"),
			RowName,
			value,
			"edit",
			state.SetInteractionMode{Mode: state.InteractionModeNAV},
			true,
		)
	}
	return SettingActionRowNode(
		core.K("workspace/name"),
		RowName,
		value,
		"edit",
		state.SetWorkspaceEditing{Editing: true},
		false,
	)
}

func workspaceDetailNode(kind string, primary core.Node[state.Action], message string) core.Node[state.Action] {
	message = strings.TrimSpace(message) // swobu:io-string source=boundary
	if message == "" {
		return primary
	}
	return core.Box[state.Action](
		primary,
		workspaceNoteNode(kind, message),
	)
}

func workspaceNoteNode(kind, message string) core.Node[state.Action] {
	noteKey := core.K("workspace/note/" + strings.TrimSpace(kind))
	if strings.TrimSpace(kind) == "" {
		noteKey = core.K("workspace/note")
	}
	return settingRowNode(
		noteKey,
		"",
		"-> "+strings.TrimSpace(message),
		"",
		core.SignalEvent[state.Action]{Kind: cockpitStaticRowSignalKind},
		core.SignalEvent[state.Action]{},
		true,
	)
}

func workspaceDeleteRowNode(endpoint string) core.Node[state.Action] {
	endpoint = strings.TrimSpace(endpoint) // swobu:io-string source=boundary
	var action state.Action
	if endpoint != "" {
		action = state.WorkspaceDeleteRequested{Name: endpoint}
	}
	if action == nil {
		action = state.SetInteractionMode{Mode: state.InteractionModeNAV}
	}
	return SettingActionRowNode(
		core.K("workspace/delete"),
		"delete workspace",
		"",
		"delete",
		action,
		false,
	)
}

func validateCreateDraftWorkspaceName(value string, existing []string) (string, string) {
	trimmed := strings.TrimSpace(value) // swobu:io-string source=boundary
	if trimmed == "" {
		return "", ""
	}
	return validateWorkspaceName(trimmed, existing, "")
}

func validateWorkspaceName(value string, existing []string, current string) (string, string) {
	parsed, err := endpointintent.ParseEndpointName(value)
	if err != nil {
		return value, err.Error()
	}
	parsedName := parsed.String()
	for _, existingName := range existing {
		if strings.TrimSpace(existingName) == strings.TrimSpace(parsedName) && strings.TrimSpace(existingName) != strings.TrimSpace(current) { // swobu:io-string source=boundary
			return parsedName, "workspace name already exists"
		}
	}
	return parsedName, ""
}

func createWorkspaceStatus(model state.Model) string {
	if selectors.InteractionMode(model) == state.InteractionModeBusySave {
		return "creating…"
	}
	if len(createWorkspaceActions(model)) == 0 {
		return "not ready"
	}
	return "ready"
}

func currentCreateName(model state.Model) string {
	return strings.TrimSpace(selectors.CreateDraftName(model)) // swobu:io-string source=boundary
}

func createWorkspaceActions(model state.Model) []state.Action {
	name := selectors.CreateDraftName(model)
	if strings.TrimSpace(name) == "" { // swobu:io-string source=boundary
		return nil
	}
	parsed, message := validateWorkspaceName(name, model.Endpoints, "")
	if message != "" {
		return nil
	}
	provider := selectors.CreateDraftProviderConfig(model)
	if provider == nil {
		return nil
	}
	flow := state.EvaluateCreateDraftRouteSetup(*provider)
	if !flow.Ready {
		return nil
	}
	if state.ProviderRequiresExplicitExecuteBaseURL(provider.ProviderSpec) && strings.TrimSpace(provider.BaseURL) == "" { // swobu:io-string source=boundary
		return nil
	}
	if strings.TrimSpace(provider.ModelID) == "" { // swobu:io-string source=boundary
		return nil
	}
	credentialRef := strings.TrimSpace(provider.CredentialRef) // swobu:io-string source=boundary
	parsedCredentialRef := credentialref.Parse(credentialRef)
	if state.ProviderRequiresCredential(provider.ProviderSpec, provider.BaseURL) {
		if parsedCredentialRef.Kind() == credentialref.KindEmpty || parsedCredentialRef.IsEmptyFileSelection() {
			return nil
		}
	}
	if parsedCredentialRef.IsEmptyFileSelection() {
		return nil
	}
	return []state.Action{
		state.SetCreateDraftName{Name: parsed},
		state.WorkspaceCreateRequested{Name: parsed},
	}
}

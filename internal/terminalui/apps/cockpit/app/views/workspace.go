// Workspace section: name editing and endpoint preview.
package views

import (
	"strings"

	"github.com/swobuforge/swobu/internal/domain/credentialref"
	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/selectors"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	toolkitviews "github.com/swobuforge/swobu/internal/terminalui/toolkit/views"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

// BuildWorkspaceSection composes the workspace section rows.
func BuildWorkspaceSection(ctx *retained.Context[state.Model]) retained.ViewSpec[state.Model] {
	model := ctx.Model()
	name := model.CurrentEndpoint
	isCreate := name == ""
	endpoint := ""
	if isCreate {
		endpoint = selectors.CreateDraftEndpointValue(model)
	} else if name != "" {
		endpoint = "/c/" + name + "/"
	}
	var out retained.ViewSpec[state.Model]
	if !isCreate {
		endpointSummary := selectors.EmptyOr(endpoint, "none")
		if strings.TrimSpace(endpointSummary) == "" { // trimlowerlint:allow boundary canonicalization
			endpointSummary = "none"
		}
		nameRow := retained.Named[state.Model]("name", retained.Build[state.Model](buildWorkspaceNameRow))
		if model.HeaderStatus == "saved" {
			nameRow = retained.Named[state.Model]("name", RowKVWithHooks(RowName, name, "edit ↵", nil, nil, nil))
		}
		endpointRow := RowActionWithHooks(RowEndpoint, endpointSummary, "copy", func() []update.Action {
			if endpoint == "" || endpoint == "not set" || endpoint == "invalid" {
				return nil
			}
			return []update.Action{state.EndpointCopyRequested{Value: endpoint}}
		}, nil, focusAffordance("copy", false))
		if model.WorkspaceCopyNote != "" {
			endpointRow = toolkitviews.NewAnchoredDisclosure(endpointRow, RowStatic("", "-> "+model.WorkspaceCopyNote))
		}

		rows := []retained.ViewSpec[state.Model]{
			nameRow,
			retained.Named[state.Model]("endpoint", endpointRow),
		}
		if model.HeaderStatus == "saved" {
			rows = append(rows, retained.Named[state.Model]("delete", RowStatic("delete workspace", "")))
		} else {
			rows = append(rows, retained.Named[state.Model]("delete", workspaceDeleteRow(name)))
		}
		out = NewCollapsibleSection(
			SectionWorkspace,
			true,
			"edit",
			SummaryRow(name+" · "+endpointSummary),
			rows...,
		)
	} else {
		rows := []retained.ViewSpec[state.Model]{
			retained.Named[state.Model]("name", retained.Build[state.Model](buildWorkspaceNameRow)),
			RowStatic(RowEndpoint, selectors.EmptyOr(endpoint, "none")),
		}
		if selectors.InteractionMode(model) == state.InteractionModeBusySave {
			rows = []retained.ViewSpec[state.Model]{
				RowStatic(RowName, selectors.EmptyOr(currentCreateName(model), "choose a workspace name")),
				RowStatic(RowEndpoint, selectors.EmptyOr(endpoint, "none")),
				busyCreateRow("creating…"),
			}
		} else if len(createWorkspaceActions(model)) == 0 {
			rows = append(rows, RowKVWithHooks("create", createWorkspaceStatus(model), "", nil, nil, focusAffordance("create", false)))
		} else {
			rows = append(rows, RowActionWithHooks("create", createWorkspaceStatus(model), "create", func() []update.Action {
				return createWorkspaceActions(model)
			}, nil, focusAffordance("create", false)))
		}
		out = retained.Named[state.Model](
			"workspace-create",
			NewCollapsibleSection(
				SectionWorkspace,
				true,
				"edit",
				nil,
				rows...,
			),
		)
	}
	return out
}

// buildWorkspaceNameRow owns the workspace name editing interaction.
func buildWorkspaceNameRow(ctx *retained.Context[state.Model]) retained.ViewSpec[state.Model] {
	model := ctx.Model()
	current := model.CurrentEndpoint
	isCreate := current == ""
	currentValue := current
	if isCreate {
		currentValue = selectors.CreateDraftName(model)
	}

	editing, setEditing := retained.UseState(ctx, func() bool { return false })
	draft, setDraft := retained.UseState(ctx, func() string { return currentValue })
	errMsg, setErrMsg := retained.UseState(ctx, func() string { return "" })

	parent := RowEditWithHooks(RowName, selectors.EmptyOr(currentValue, "choose a workspace name"), func() []update.Action {
		seed := currentValue
		if isCreate {
			// In first-run/create mode, edit always starts from a blank input.
			// The prompt text is a placeholder, not an editable default value.
			seed = ""
		}
		setDraft(seed)
		setEditing(true)
		setErrMsg("")
		return []update.Action{state.SetInteractionMode{Mode: state.InteractionModeEditText}}
	}, func() []update.Action {
		if !editing {
			return nil
		}
		setErrMsg("")
		setDraft(currentValue)
		setEditing(false)
		return []update.Action{state.SetInteractionMode{Mode: state.InteractionModeNAV}}
	}, focusAffordance("edit", false))
	var out retained.ViewSpec[state.Model]
	if !editing {
		if message := selectors.EmptyOr(model.WorkspaceSaveError, ""); message != "" {
			out = toolkitviews.NewAnchoredDisclosure(parent, RowStatic("", "-> "+message))
		} else {
			out = parent
		}
	} else {
		editor := retained.Named[state.Model]("name-editor", InlineEditor(
			RowName, draft, "choose a workspace name",
			func(value string) []update.Action {
				setDraft(value)
				setErrMsg("")
				return nil
			},
			func(value string) []update.Action {
				if isCreate {
					parsed, message := validateCreateDraftWorkspaceName(value, model.Endpoints)
					if message != "" {
						setErrMsg(message)
						return nil
					}
					setDraft(parsed)
					setErrMsg("")
					setEditing(false)
					return []update.Action{
						state.SetCreateDraftName{Name: parsed},
						state.SetInteractionMode{Mode: state.InteractionModeNAV},
					}
				}
				parsed, message := validateWorkspaceName(value, model.Endpoints, current)
				if message != "" {
					setErrMsg(message)
					return nil
				}
				setDraft(parsed)
				setErrMsg("")
				setEditing(false)
				return []update.Action{
					state.WorkspaceRenameRequested{CurrentName: current, Name: parsed},
				}
			},
			func() []update.Action {
				setErrMsg("")
				setDraft(currentValue)
				setEditing(false)
				return []update.Action{state.SetInteractionMode{Mode: state.InteractionModeNAV}}
			},
		))
		if errMsg != "" {
			out = toolkitviews.NewAnchoredDisclosure(editor, retained.Named[state.Model]("name-error", RowStatic("", "-> "+errMsg)))
		} else {
			out = editor
		}
	}
	return out
}

func validateCreateDraftWorkspaceName(value string, existing []string) (string, string) {
	trimmed := strings.TrimSpace(value) // trimlowerlint:allow boundary canonicalization
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
		if strings.TrimSpace(existingName) == strings.TrimSpace(parsedName) && strings.TrimSpace(existingName) != strings.TrimSpace(current) { // trimlowerlint:allow boundary canonicalization
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
	return strings.TrimSpace(selectors.CreateDraftName(model)) // trimlowerlint:allow boundary canonicalization
}

func workspaceDeleteRow(endpoint string) retained.ViewSpec[state.Model] {
	endpoint = strings.TrimSpace(endpoint) // trimlowerlint:allow boundary canonicalization
	return RowActionWithHooks("delete workspace", "", "delete", func() []update.Action {
		if endpoint == "" {
			return nil
		}
		return []update.Action{state.WorkspaceDeleteRequested{Name: endpoint}}
	}, nil, focusAffordance("delete", false))
}

func busyCreateRow(value string) retained.ViewSpec[state.Model] {
	value = strings.TrimSpace(value) // trimlowerlint:allow boundary canonicalization
	return retained.FromRenderNode[state.Model](toolkitviews.NewAction(6+toolkitviews.RuneLen(value), true, false, func(_ bool, width int) string {
		line := ">   create"
		if value != "" {
			line += "            " + value
		}
		return toolkitviews.PadRight(toolkitviews.TrimToWidth(line, width), width)
	}, func(string) []update.Action { return nil }, nil))
}

func createWorkspaceActions(model state.Model) []update.Action {
	name := selectors.CreateDraftName(model)
	if strings.TrimSpace(name) == "" { // trimlowerlint:allow boundary canonicalization
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
	if provider.ProviderSpec == "openai_compatible" && strings.TrimSpace(provider.BaseURL) == "" { // trimlowerlint:allow boundary canonicalization
		return nil
	}
	if strings.TrimSpace(provider.ModelID) == "" { // trimlowerlint:allow boundary canonicalization
		return nil
	}
	credentialRef := strings.TrimSpace(provider.CredentialRef) // trimlowerlint:allow boundary canonicalization
	parsedCredentialRef := credentialref.Parse(credentialRef)
	if state.ProviderRequiresCredential(provider.ProviderSpec, provider.BaseURL) {
		if parsedCredentialRef.Kind() == credentialref.KindEmpty || parsedCredentialRef.IsEmptyFileSelection() {
			return nil
		}
	}
	if parsedCredentialRef.IsEmptyFileSelection() {
		return nil
	}
	return []update.Action{
		state.SetCreateDraftName{Name: parsed},
		state.WorkspaceCreateRequested{Name: parsed},
	}
}

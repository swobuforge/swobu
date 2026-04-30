// Workspace section: name editing and endpoint preview.
package views

import (
	"strings"

	"github.com/metrofun/swobu/internal/domain/endpointintent"
	"github.com/metrofun/swobu/internal/terminalui/apps/cockpit/app/selectors"
	"github.com/metrofun/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/metrofun/swobu/internal/terminalui/engine/retained/update"
	"github.com/metrofun/swobu/internal/terminalui/engine/retained/view"
	toolkitviews "github.com/metrofun/swobu/internal/terminalui/toolkit/views"
)

// BuildWorkspaceSection composes the workspace section rows.
func BuildWorkspaceSection(ctx *view.Context[state.Model]) view.ViewSpec[state.Model] {
	model := ctx.Model()
	name := model.CurrentEndpoint
	isCreate := name == ""
	endpoint := ""
	if isCreate {
		endpoint = selectors.CreateDraftEndpointValue(model)
	} else if name != "" {
		endpoint = "/c/" + name + "/"
	}
	var out view.ViewSpec[state.Model]
	if !isCreate {
		endpointSummary := selectors.EmptyOr(endpoint, "none")
		if strings.TrimSpace(endpointSummary) == "" {
			endpointSummary = "none"
		}
		nameRow := view.Named[state.Model]("name", view.Build[state.Model](buildWorkspaceNameRow))
		if model.HeaderStatus == "saved" {
			nameRow = view.Named[state.Model]("name", RowKVWithHooks(RowName, name, "edit ↵", nil, nil, nil))
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

		rows := []view.ViewSpec[state.Model]{
			nameRow,
			view.Named[state.Model]("endpoint", endpointRow),
		}
		if model.HeaderStatus == "saved" {
			rows = append(rows, view.Named[state.Model]("delete", RowStatic("delete workspace", "")))
		} else {
			rows = append(rows, view.Named[state.Model]("delete", workspaceDeleteRow(name)))
		}
		out = NewCollapsibleSection(
			SectionWorkspace,
			true,
			"edit",
			SummaryRow(name+" · "+endpointSummary),
			rows...,
		)
	} else {
		rows := []view.ViewSpec[state.Model]{
			view.Named[state.Model]("name", view.Build[state.Model](buildWorkspaceNameRow)),
			RowStatic(RowEndpoint, selectors.EmptyOr(endpoint, "none")),
		}
		if selectors.InteractionMode(model) == state.InteractionModeBusySave {
			rows = []view.ViewSpec[state.Model]{
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
		out = view.Named[state.Model](
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
func buildWorkspaceNameRow(ctx *view.Context[state.Model]) view.ViewSpec[state.Model] {
	model := ctx.Model()
	current := model.CurrentEndpoint
	isCreate := current == ""
	currentValue := current
	if isCreate {
		currentValue = selectors.CreateDraftName(model)
	}

	editing, setEditing := view.UseState(ctx, func() bool { return false })
	draft, setDraft := view.UseState(ctx, func() string { return currentValue })
	errMsg, setErrMsg := view.UseState(ctx, func() string { return "" })

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
	var out view.ViewSpec[state.Model]
	if !editing {
		if message := selectors.EmptyOr(model.WorkspaceSaveError, ""); message != "" {
			out = toolkitviews.NewAnchoredDisclosure(parent, RowStatic("", "-> "+message))
		} else {
			out = parent
		}
	} else {
		editor := view.Named[state.Model]("name-editor", InlineEditor(
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
			out = toolkitviews.NewAnchoredDisclosure(editor, view.Named[state.Model]("name-error", RowStatic("", "-> "+errMsg)))
		} else {
			out = editor
		}
	}
	return out
}

func validateCreateDraftWorkspaceName(value string, existing []string) (string, string) {
	trimmed := strings.TrimSpace(value)
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
		if strings.TrimSpace(existingName) == strings.TrimSpace(parsedName) && strings.TrimSpace(existingName) != strings.TrimSpace(current) {
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
	return strings.TrimSpace(selectors.CreateDraftName(model))
}

func workspaceDeleteRow(endpoint string) view.ViewSpec[state.Model] {
	endpoint = strings.TrimSpace(endpoint)
	return RowActionWithHooks("delete workspace", "", "delete", func() []update.Action {
		if endpoint == "" {
			return nil
		}
		return []update.Action{state.WorkspaceDeleteRequested{Name: endpoint}}
	}, nil, focusAffordance("delete", false))
}

func busyCreateRow(value string) view.ViewSpec[state.Model] {
	value = strings.TrimSpace(value)
	return view.FromRenderNode[state.Model](toolkitviews.NewAction(6+toolkitviews.RuneLen(value), true, false, func(_ bool, width int) string {
		line := ">   create"
		if value != "" {
			line += "            " + value
		}
		return toolkitviews.PadRight(toolkitviews.TrimToWidth(line, width), width)
	}, func(string) []update.Action { return nil }, nil))
}

func createWorkspaceActions(model state.Model) []update.Action {
	name := selectors.CreateDraftName(model)
	if strings.TrimSpace(name) == "" {
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
	if provider.ProviderSpec == "custom" && strings.TrimSpace(provider.BaseURL) == "" {
		return nil
	}
	if strings.TrimSpace(provider.ModelID) == "" {
		return nil
	}
	credentialRef := strings.TrimSpace(provider.CredentialRef)
	if state.ProviderRequiresCredential(provider.ProviderSpec, provider.BaseURL) {
		if credentialRef == "" || strings.EqualFold(credentialRef, "file") || strings.EqualFold(credentialRef, "file:") {
			return nil
		}
	}
	if strings.HasPrefix(strings.ToLower(credentialRef), "file:") && strings.TrimSpace(strings.TrimPrefix(credentialRef, "file:")) == "" {
		return nil
	}
	return []update.Action{
		state.SetCreateDraftName{Name: parsed},
		state.WorkspaceCreateRequested{Name: parsed},
	}
}

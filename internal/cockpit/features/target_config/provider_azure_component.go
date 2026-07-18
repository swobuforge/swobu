package target_config

import (
	"net/url"
	"strings"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/ui"
	"github.com/swobuforge/swobu/internal/profile"
)

type azureProviderForm struct {
	target            *TargetConfig
	endpointCommitted bool
	endpointDraft     *tui.State[string]
}

func AzureProviderForm(w *TargetConfig) tui.Component {
	return &azureProviderForm{target: w, endpointDraft: tui.NewState(w.BaseURL.Get())}
}

func (f *azureProviderForm) BindApp(app *tui.App) { f.endpointDraft.BindApp(app) }

func (f *azureProviderForm) UpdateProps(fresh tui.Component) {
	if next, ok := fresh.(*azureProviderForm); ok {
		f.target = next.target
	}
}

// azureReadiness requires an explicit project endpoint, then a credential.
func azureReadiness(w *TargetConfig, base providerSetupState) providerSetupState {
	setup := base
	endpointValue := strings.TrimSpace(w.BaseURL.Get()) // swobu:io-string source=boundary
	_, endpointErr := profile.NormalizeAzureProjectEndpoint(endpointValue)
	if endpointErr != nil && w.mode == targetConfigModeEdit {
		// Older persisted Azure targets contain the runtime resource root rather
		// than a Foundry project locator. It remains a valid edit subject even
		// though new targets require a project locator before catalog probing.
		_, endpointErr = profile.NormalizeAzureResourceLocator(endpointValue)
	}
	setup.RequiresEndpoint = endpointValue == "" || endpointErr != nil
	if setup.RequiresEndpoint {
		setup.CredentialLabel = "enter " + setup.EndpointLabel
		setup.BlockReason = setup.CredentialLabel
		return setup
	}
	if setup.CredentialRequired && strings.TrimSpace(setup.CredentialRef) == "" {
		setup.CredentialLabel = "enter credential"
		setup.BlockReason = setup.CredentialLabel
		return setup
	}
	setup.CredentialLabel = setup.CredentialRef
	setup.ReadyForCatalog = true
	return setup
}

func (w *TargetConfig) IsAzureFlow() bool {
	return profile.ProviderID(w.Draft.Get().ProviderSpec) == profile.ProviderSpecAzure
}

// AzureCredentialBlocked is the Azure form's local prerequisite row. Azure
// requires a project locator before credential setup can be meaningful.
func AzureCredentialBlocked(w *TargetConfig) *ui.SelectableRow {
	return ui.NewSelectableRow(
		TargetAddMountKey(w, "credential-display"),
		"credential",
		"blocked",
		"project first",
		nil,
	)
}

// AzureProjectEndpointInput renders Azure's project locator as a provider-owned
// row because this value is not a generic provider base URL.
func AzureProjectEndpointInput(w *TargetConfig, autoFocus bool, onSubmitted ...func()) *ui.EditableRow {
	return AzureProjectEndpointDraftInput(w, w.BaseURL, autoFocus, onSubmitted...)
}

func AzureProjectEndpointDraftInput(w *TargetConfig, value *tui.State[string], autoFocus bool, onSubmitted ...func()) *ui.EditableRow {
	setup := w.setupState()
	label := strings.TrimSpace(setup.EndpointLabel)
	if label == "" {
		label = "project"
	}
	row := ui.NewEditableRow(
		TargetAddMountKey(w, "azure-project-endpoint"),
		label,
		value,
	)
	row.Placeholder = "endpoint required"
	row.ValidationText = "paste https://.../api/projects/..."
	row.ViewValue = azureProjectEndpointDisplay
	row.ViewAction = "edit ↵"
	row.EditAction = "save ↵"
	row.AutoFocus = autoFocus
	row.OnSubmit = func(raw string) {
		projectEndpoint, err := profile.NormalizeAzureProjectEndpoint(strings.TrimSpace(raw))
		if err != nil {
			w.Error.Set(azureProjectEndpointError(err))
			value.Set(raw)
			w.Phase.Set(PhaseConfiguring)
			return
		}
		w.Error.Set("")
		w.BaseURL.Set(projectEndpoint)
		value.Set(projectEndpoint)
		w.advanceFromSetup()
		if len(onSubmitted) > 0 && onSubmitted[0] != nil {
			onSubmitted[0]()
		}
	}
	row.CloseAfterSubmit = func() bool { return strings.TrimSpace(w.Error.Get()) == "" }
	return row
}

func azureProjectEndpointDisplay(raw string) string {
	projectEndpoint, err := profile.NormalizeAzureProjectEndpoint(raw)
	if err != nil {
		return strings.TrimSpace(raw) // swobu:io-string source=boundary
	}
	parsed, err := url.Parse(projectEndpoint)
	if err != nil {
		return projectEndpoint
	}
	resource := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".services.ai.azure.com")
	project := strings.Trim(strings.TrimPrefix(strings.ToLower(parsed.EscapedPath()), "/api/projects/"), "/")
	if resource == "" || project == "" || strings.Contains(project, "/") {
		return projectEndpoint
	}
	return resource + "/" + project
}

func azureProjectEndpointError(err error) string {
	msg := strings.TrimSpace(err.Error())
	if strings.Contains(msg, "not an Azure AI project endpoint") {
		return "not an Azure AI project endpoint"
	}
	return "not a project endpoint"
}

func azureCatalogOperatorError(result readmodel.ModelCatalogReadModel) string {
	errText := strings.ToLower(strings.TrimSpace(result.Error)) // swobu:io-string source=boundary
	switch {
	case errText == "":
		return ""
	case strings.Contains(errText, "project not found") || strings.Contains(errText, "notfound"):
		return "project not found"
	case strings.Contains(errText, "unauthorized") || strings.Contains(errText, "401") || strings.Contains(errText, "access denied"):
		return "unauthorized"
	default:
		return strings.TrimSpace(result.Error)
	}
}

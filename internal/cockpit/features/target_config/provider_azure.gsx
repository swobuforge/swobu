package target_config

import (
	"net/url"
	"strings"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/ui"
	"github.com/swobuforge/swobu/internal/profile"
)

// azureReadiness requires an explicit project endpoint, then a credential.
func azureReadiness(w *TargetConfig, base providerSetupState) providerSetupState {
	setup := base
	endpointValue := strings.TrimSpace(w.BaseURL.Get()) // swobu:io-string source=boundary
	_, endpointErr := profile.NormalizeAzureProjectEndpoint(endpointValue)
	if endpointValue == "" || endpointErr != nil {
		setup.Status = setupMissingLocator
		return setup
	}
	if setup.CredentialRequired && strings.TrimSpace(setup.CredentialRef) == "" {
		setup.Status = setupMissingCredential
		return setup
	}
	setup.Status = setupReady
	return setup
}

func (w *TargetConfig) IsAzureFlow() bool {
	return profile.ProviderID(w.Draft.Get().ProviderSpec) == profile.ProviderSpecAzure
}

// AzureCredentialBlocked is the Azure form's dependent prerequisite row. Azure
// requires a project locator before credential setup can be meaningful.
func AzureCredentialBlocked(w *TargetConfig) *ui.SelectableRow {
	return ui.NewSelectableRow(
		TargetAddMountKey(w, "credential-display"),
		"credential",
		"waiting for project endpoint",
		"",
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
	row.Placeholder = "required"
	if autoFocus {
		row.ValidationText = "Paste https://.../api/projects/..."
	}
	row.ViewValue = azureProjectEndpointDisplay
	row.ViewAction = "edit ↵"
	row.EditAction = "save ↵"
	row.AutoFocus = autoFocus
	row.OnSubmit = func(raw string) {
		projectEndpoint, err := profile.NormalizeAzureProjectEndpoint(strings.TrimSpace(raw))
		if err != nil {
			w.Error.Set(azureProjectEndpointError(err))
			value.Set(raw)
			w.Lifecycle.Set(LifecycleOpen)
			return
		}
		w.Error.Set("")
		w.BaseURL.Set(projectEndpoint)
		w.invalidateCatalogEvidence()
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

func azureCatalogOperatorError(raw string) string {
	errText := strings.ToLower(strings.TrimSpace(raw)) // swobu:io-string source=boundary
	switch {
	case errText == "":
		return ""
	case strings.Contains(errText, "project not found") || strings.Contains(errText, "notfound"):
		return "project not found"
	case strings.Contains(errText, "unauthorized") || strings.Contains(errText, "401") || strings.Contains(errText, "access denied"):
		return "unauthorized"
	default:
		return strings.TrimSpace(raw)
	}
}

templ (f *azureProviderForm) Render() {
	<div class="flex-col w-full" deps={f.target.Draft, f.target.BaseURL}>
		if shouldRenderEndpointRow(f.target) { @AzureProjectEndpointDraftInput(f.target, f.endpointDraft, setupRequiresLocator(f.target), func() { f.endpointCommitted = true }) }
		if setupRequiresLocator(f.target) { @AzureCredentialBlocked(f.target)
		} else { <div key={credentialRegionKey(f.target)} class="w-full">@CredentialControlRegion(f.target, f.endpointCommitted)</div> }
	</div>
}

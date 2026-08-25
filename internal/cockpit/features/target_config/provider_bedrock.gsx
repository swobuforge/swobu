package target_config

import (
	"strings"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/ui"
	"github.com/swobuforge/swobu/internal/domain/credentialref"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
)

type bedrockProviderForm struct{ target *TargetConfig }

type bedrockAuthenticationMenu struct {
	target *TargetConfig
	backout func()
	// chooser is stable mounted interaction state. Keeping its identity in
	// reactive state prevents generated prop reconciliation from replacing an
	// open child editor with callbacks bound to a discarded parent menu.
	chooser *tui.State[*credentialRow]
	choosing *tui.State[bool]
}

func newBedrockAuthenticationMenu(w *TargetConfig, backout func()) *bedrockAuthenticationMenu {
	row := newCredentialRow(w, false)
	row.props.Apply = func(ref string) { w.changeCredentialRef(strings.TrimSpace(ref)); backout() }
	menu := &bedrockAuthenticationMenu{target: w, backout: backout, chooser: tui.NewState(row), choosing: tui.NewState(false)}
	row.stage.Set(credStageMenu)
	return menu
}

func (m *bedrockAuthenticationMenu) BindApp(app *tui.App) { m.bindAppFields(app); m.chooser.Get().BindApp(app) }
func (m *bedrockAuthenticationMenu) UnbindApp() {}
func (m *bedrockAuthenticationMenu) KeyMap() tui.KeyMap {
	return ui.BackScope(func() bool { return true }, func() {
		if m.choosing.Get() {
			if m.chooser.Get().stage.Get() != credStageMenu {
				m.chooser.Get().stage.Set(credStageMenu)
				return
			}
			m.choosing.Set(false)
			return
		}
		m.backout()
	})
}

func BedrockAuthenticationField(w *TargetConfig) *ui.Select {
	ref := strings.TrimSpace(w.Draft.Get().CredentialRef)
	value := "AWS identity"
	if ref == "" {
		if evidence := w.catalogResult().BedrockAuthentication.AWSIdentity; evidence != nil && evidence.State == "resolved" { value = bedrockAWSIdentitySummary(*evidence) }
		if w.catalogResult().BedrockAuthentication.FailureStage == "authentication" { value = "AWS identity unavailable" }
	} else {
		value = "Bedrock API key · " + bedrockCredentialSource(ref)
		if w.catalogResult().BedrockAuthentication.FailureStage == "authentication" { value = "Bedrock API key unavailable" }
	}
	return ui.NewSelect(ui.SelectProps{ID: TargetAddMountKey(w, "bedrock-authentication"), Label: "authentication", Value: value, Detail: BedrockAuthenticationDetail(w), Action: "manage ↵", Body: func(backout func()) tui.Component { return newBedrockAuthenticationMenu(w, backout) }})
}

func bedrockCredentialSource(ref string) string {
	return credentialSourceDisplay(credentialref.Parse(ref))
}

func bedrockCredentialDetail(ref string) string {
	parsed := credentialref.Parse(ref)
	_, detail, ok := strings.Cut(parsed.String(), ":")
	if !ok { return parsed.String() }
	return detail
}

func BedrockRefreshIdentityOption(m *bedrockAuthenticationMenu) *ui.SelectableRow {
	value, action := "refresh identity", "refresh ↵"
	return ui.NewSelectableRow(TargetAddMountKey(m.target, "refresh-identity"), "", value, action, m.target.RefreshBedrockIdentity)
}

func BedrockUseAPIKeyOption(m *bedrockAuthenticationMenu) *ui.SelectableRow {
	return ui.NewSelectableRow(TargetAddMountKey(m.target, "use-api-key"), "", "use Bedrock API key", "select ↵", func() { m.choosing.Set(true) })
}

func BedrockChangeCredentialOption(m *bedrockAuthenticationMenu) *ui.SelectableRow {
	return ui.NewSelectableRow(TargetAddMountKey(m.target, "change-api-key"), "", "change credential", "enter ↵", func() { m.choosing.Set(true) })
}

func BedrockUseAWSIdentityOption(m *bedrockAuthenticationMenu) *ui.SelectableRow {
	return ui.NewSelectableRow(TargetAddMountKey(m.target, "use-aws-identity"), "", "use AWS identity", "select ↵", func() { m.target.changeCredentialRef(""); m.backout() })
}

func BedrockProviderForm(w *TargetConfig) tui.Component { return &bedrockProviderForm{target: w} }

// bedrockReadiness deliberately has no authentication picker. The catalog
// probe resolves a configured credential reference as a Bedrock API key; when
// absent it uses the AWS SDK default credential chain.
func bedrockReadiness(w *TargetConfig, base providerSetupState) providerSetupState {
	setup := base
	setup.CredentialRequired = false
	region := strings.TrimSpace(w.Draft.Get().Locator)
	if region == "" {
		setup.Status = setupMissingLocator
		return setup
	}
	setup.Status = setupReady
	return setup
}

func (w *TargetConfig) IsBedrockFlow() bool {
	return profile.ProviderID(w.Draft.Get().ProviderSpec) == profile.ProviderSpecBedrock
}

// BedrockEndpointRow is the single API-URL authoring surface: the complete API
// base URL including its AWS namespace (e.g. …/openai/v1), as free text in the
// shared ui.EditableRow — never a Select/SearchPicker (a URL is open text, not a
// closed set). Draft.Endpoint is the durable operator-authored value. Validation
// and the helper line are pure
// logic over profile.ResolveBedrockEndpoint (the single endpoint boundary living
// in internal/profile), kept here as plain functions rather than a new .go file.
func BedrockEndpointRow(w *TargetConfig) *ui.EditableRow {
	row := ui.NewEditableRow(TargetAddMountKey(w, "bedrock-endpoint"), "API URL", w.BaseURL)
	row.ViewAction = "edit ↵"
	row.EditAction = "save ↵"
	row.OpenAtStart = true
	w.syncBedrockEndpointRow(row)
	row.OnSubmit = func(raw string) {
		// Submitted input is validated transactionally. Invalid input remains in
		// the shared row's private editor draft; valid input is canonicalized into
		// the target draft's one explicit endpoint fact.
		if _, err := profile.ResolveBedrockEndpoint(raw, strings.TrimSpace(w.Draft.Get().Locator), bedrockEndpointProtocolKind(w)); err != nil {
			return
		}
		resolution, _ := profile.ResolveBedrockEndpoint(raw, strings.TrimSpace(w.Draft.Get().Locator), bedrockEndpointProtocolKind(w))
		w.Draft.Update(func(d readmodel.TargetDraft) readmodel.TargetDraft {
			d.Endpoint = resolution.BaseURL
			return d
		})
		w.BaseURL.Set(resolution.BaseURL)
		w.Error.Set("")
		w.CommitEdit(w.actionContext())
	}
	row.CloseAfterSubmit = func() bool {
		_, err := profile.ResolveBedrockEndpoint(w.BaseURL.Get(), strings.TrimSpace(w.Draft.Get().Locator), bedrockEndpointProtocolKind(w))
		return err == nil
	}
	row.OnClose = func() {
		w.BaseURL.Set(strings.TrimSpace(w.Draft.Get().Endpoint))
	}
	return row
}

// syncBedrockEndpointRow recomputes the row's validation and helper line from
// the last submitted value plus selected protocol before each render. Editing
// remains transactional; validation does not publish partial text while typing.
func (w *TargetConfig) syncBedrockEndpointRow(row *ui.EditableRow) {
	validation, text := bedrockEndpointValidation(w.BaseURL.Get(), strings.TrimSpace(w.Draft.Get().Locator), bedrockEndpointProtocolKind(w))
	row.Validation = validation
	row.ValidationText = text
}

// bedrockEndpointProtocolKind maps the selected provider protocol to its wire
// kind for the endpoint↔namespace coherence check. An unresolved protocol (e.g.
// a derived Bedrock protocol before a model is chosen) skips the contradiction
// check — only forgiveness then applies, and the row stays None.
func bedrockEndpointProtocolKind(w *TargetConfig) protocolkind.ProtocolKind {
	draft := w.Draft.Get()
	if kind, ok := profile.ProviderProtocolKind(draft.ProviderSpec, draft.ProviderProtocol); ok {
		return kind
	}
	return protocolkind.ProtocolKind("")
}

// bedrockEndpointValidation is the pure text-input state for the Bedrock API URL
// row, computed from the row's current value + selected protocol on every render.
// Required when empty, Invalid when the resolver rejects a recognized cross-family
// namespace contradiction, otherwise None with an inline helper line. No host or
// region validation here — that is the adapter validator's job.
func bedrockEndpointValidation(value, region string, kind protocolkind.ProtocolKind) (ui.EditableRowValidation, string) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ui.EditableRowValidationRequired, "Paste the API URL shown by AWS."
	}
	resolution, err := profile.ResolveBedrockEndpoint(trimmed, region, kind)
	if err != nil {
		return ui.EditableRowValidationInvalid, err.Error()
	}
	if resolution.InputWasComplete {
		return ui.EditableRowValidationNone, "Complete request URL normalized to its API base."
	}
	return ui.EditableRowValidationNone, "Explicit API URL."
}

func BedrockRegionControl(w *TargetConfig) *ui.Select {
	region := strings.TrimSpace(w.Draft.Get().Locator)
	value, action := profile.BedrockMantleRegionLabel(region), "change ↵"
	if region == "" {
		value, action = "required", "choose ↵"
	}
	return ui.NewSelect(ui.SelectProps{
		ID: TargetAddMountKey(w, "bedrock-region"), Label: "region", Value: value, Action: action,
		AutoFocus: w.setupState().RequiresLocator() && region == "",
		Body:      func(backout func()) tui.Component { return BedrockRegionPicker(w, backout) },
	})
}

func BedrockAuthenticationDetail(w *TargetConfig) string {
	if ref := strings.TrimSpace(w.Draft.Get().CredentialRef); ref != "" {
		if evidence := w.catalogResult().BedrockAuthentication; evidence.FailureStage == "authentication" {
			if strings.TrimSpace(evidence.Error) != "" { return strings.TrimSpace(evidence.Error) }
			return "Bedrock API key could not be resolved."
		}
		return bedrockCredentialDetail(ref)
	}
	catalog := w.catalogResult()
	diagnostics := catalog.BedrockAuthentication
	if diagnostics.FailureStage == "authentication" && strings.TrimSpace(diagnostics.Error) != "" {
		return strings.TrimSpace(diagnostics.Error)
	}
	if diagnostics.AWSIdentity == nil {
		return ""
	}
	switch diagnostics.AWSIdentity.State {
	case "resolved":
		return diagnostics.AWSIdentity.ARN
	case "credentials_missing":
		return "No AWS credentials found."
	case "identity_probe_failed":
		return strings.TrimSpace(diagnostics.AWSIdentity.Error)
	default:
		return ""
	}
}

func bedrockAWSIdentitySummary(identity readmodel.AWSIdentityReadModel) string {
	compact := compactAWSIdentity(identity.ARN, identity.Account)
	if compact == "" {
		return "AWS identity"
	}
	return "AWS identity · " + compact
}

func compactAWSIdentity(arn, account string) string {
	name := ""
	for _, marker := range []string{"assumed-role/", "role/", "user/"} {
		if _, tail, ok := strings.Cut(arn, marker); ok {
			name = strings.SplitN(tail, "/", 2)[0]
			break
		}
	}
	if strings.TrimSpace(account) == "" {
		return name
	}
	if name == "" {
		return account
	}
	return name + " · " + account
}

func BedrockRegionPicker(w *TargetConfig, backout func()) *ui.SearchPicker {
	regions := profile.BedrockMantleRegions()
	opts := make([]ui.SearchOption, len(regions))
	for i, region := range regions {
		opts[i] = ui.SearchOption{ID: region.ID, Label: region.Label, Keywords: append([]string{region.ID}, region.Keywords...)}
	}
	picker := ui.NewSearchPicker(TargetAddMountKey(w, "bedrock-region-picker"), "region", opts, func(sel ui.Selection) {
		w.SelectBedrockRegion(sel.Value)
		if backout != nil {
			backout()
		}
	}, func() {
		if backout != nil {
			backout()
		}
	})
	picker.AutoFocus = true
	return picker
}

templ (f *bedrockProviderForm) Render() {
	<div class="flex-col w-full" deps={f.target.Draft, f.target.Catalog, f.target.BaseURL}>
		@BedrockRegionControl(f.target)
		@BedrockAuthenticationField(f.target)
	</div>
}

templ (m *bedrockAuthenticationMenu) Render() {
	<div class="flex-col w-full">
		if m.choosing.Get() { @CredentialChooser(m.chooser.Get())
		} else if strings.TrimSpace(m.target.Draft.Get().CredentialRef) == "" {
			if detail := BedrockAuthenticationDetail(m.target); detail != "" { <div class="w-full">@FlowText(detail)</div> }
			@BedrockRefreshIdentityOption(m)
			@BedrockUseAPIKeyOption(m)
		} else {
			<div class="w-full">@FlowText(bedrockCredentialDetail(m.target.Draft.Get().CredentialRef))</div>
			@BedrockChangeCredentialOption(m)
			@BedrockUseAWSIdentityOption(m)
		}
	</div>
}

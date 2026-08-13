package target_config

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/routing"
)

func credentialDisplay(w *TargetConfig) string {
	return credentialRefDisplay(w.Draft.Get().CredentialRef)
}

// CredentialControlRegion is the narrow feature adapter: it resolves provider
// policy and effects into props, then mounts the provider-neutral field.
func CredentialControlRegion(w *TargetConfig, autoFocus ...bool) tui.Component {
	focus := len(autoFocus) > 0 && autoFocus[0]
	return newCredentialRow(w, focus)
}

func credentialRegionKey(w *TargetConfig) string {
	return TargetAddMountKey(w, "credential:"+strings.TrimSpace(w.Draft.Get().ProviderSpec))
}

func newCredentialRow(target *TargetConfig, autoFocus bool) *credentialRow {
	provider, _ := providerProfileForSpec(target.Draft.Get().ProviderSpec)
	props := CredentialFieldProps{
		ID: credentialRegionKey(target), Optional: providerAllowsNoCredential(target),
		SuggestedEnvVar: provider.Credential.SuggestedEnvVar,
		Ref:             target.Draft.Get().CredentialRef, AutoFocus: autoFocus,
	}
	props.Apply = func(ref string) { target.changeCredentialRef(strings.TrimSpace(ref)) }
	props.Store = func(secret string) (string, error) {
		return target.storePastedCredential(target.actionContext(), secret)
	}
	row := newCredentialField(props)
	if target.credentialReadDir != nil {
		row.readDir = target.credentialReadDir
	}
	if target.credentialInitialPath != "" {
		row.filePath.Set(target.credentialInitialPath)
	}
	return row
}

func ambientOrReferenceAuthenticationProps(target *TargetConfig, credential profile.CredentialSpec) AmbientOrReferenceAuthenticationProps {
	return AmbientOrReferenceAuthenticationProps{
		ID:              credentialRegionKey(target),
		AmbientLabel:    credential.AmbientLabel,
		ReferenceLabel:  credential.ReferenceLabel,
		SuggestedEnvVar: credential.SuggestedEnvVar,
		Ref:             target.Draft.Get().CredentialRef,
		Apply: func(ref string) {
			target.changeCredentialRef(strings.TrimSpace(ref))
		},
		Store: func(secret string) (string, error) {
			return target.storePastedCredential(target.actionContext(), secret)
		},
	}
}

func (w *TargetConfig) changeCredentialRef(ref string) {
	w.Draft.Update(func(d readmodel.TargetDraft) readmodel.TargetDraft {
		d.CredentialRef = strings.TrimSpace(ref)
		return d
	})
	w.invalidateCatalogEvidence()
	w.advanceFromSetup()
	w.CommitEdit(w.actionContext())
}

func (w *TargetConfig) storePastedCredential(ctx context.Context, secret string) (string, error) {
	if w.CredentialCommands == nil {
		return "", errors.New("credential store is not wired yet")
	}
	result, err := w.CredentialCommands.StorePastedCredential(ctx, ports.StorePastedCredentialRequest{ProviderSpec: w.Draft.Get().ProviderSpec, Name: w.credentialSlot, Secret: secret})
	if err != nil {
		return "", err
	}
	ref := strings.TrimSpace(result.CredentialRef)
	if ref == "" {
		return "", errors.New("credential store returned empty ref")
	}
	return ref, nil
}

func newCredentialSlot(workspaceID readmodel.WorkspaceID, routeID readmodel.RouteID, targetID readmodel.TargetID) string {
	targetPart := string(targetID)
	if strings.TrimSpace(targetPart) == "" {
		targetPart = fmt.Sprintf("draft-%d", time.Now().UTC().UnixNano())
	}
	return strings.Join([]string{"cockpit", "target", safeCredentialSlotPart(string(workspaceID), "workspace"), safeCredentialSlotPart(string(routeID), "route"), safeCredentialSlotPart(targetPart, "target")}, "/")
}

func safeCredentialSlotPart(raw, fallback string) string {
	raw = strings.TrimSpace(strings.ToLower(raw)) // swobu:io-string source=boundary
	var out []rune
	lastDash := false
	for _, r := range raw {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' {
			out = append(out, r)
			lastDash = false
		} else if !lastDash {
			out = append(out, '-')
			lastDash = true
		}
	}
	if cleaned := strings.Trim(string(out), "-_"); cleaned != "" {
		return cleaned
	}
	return fallback
}

// effects.go owns target_config product transitions and all calls to cockpit
// ports. UI primitives call these methods; no effect owns entered-row state.

// SetCatalogResult updates the catalog result and transitions to ChoosingModel
// or CatalogFailed.
func (w *TargetConfig) SetCatalogResult(result readmodel.ModelCatalogReadModel, probeErr error) {
	errText := ""
	if probeErr != nil {
		errText = strings.TrimSpace(probeErr.Error())
		if w.IsAzureFlow() {
			errText = azureCatalogOperatorError(errText)
		}
	}
	state := catalogOperationState{Result: result, Err: errText}
	if errText != "" {
		w.Catalog.Set(state)
		return
	}
	w.Catalog.Set(state)
	if w.reconcileSelectedModel(result.Deployments) {
		return
	}
	if w.IsAzureFlow() && len(result.Deployments) == 0 {
		state.Err = "none found"
		w.Catalog.Set(state)
		return
	}
}

// reconcileSelectedModel enriches a typed selection when discovery returns a
// matching row. A missing row never invalidates the operator-authored model.
func (w *TargetConfig) reconcileSelectedModel(deployments []readmodel.ModelDeploymentReadModel) bool {
	selected := w.SelectedModel.Get()
	if strings.TrimSpace(selected.ModelName) == "" {
		return false
	}
	if w.hydrateSelectedModel(deployments) {
		return true
	}
	return true
}

func (w *TargetConfig) hydrateSelectedModel(deployments []readmodel.ModelDeploymentReadModel) bool {
	selected := w.SelectedModel.Get()
	if strings.TrimSpace(selected.ModelName) == "" {
		return false
	}
	for _, deployment := range deployments {
		if deployment.ID != selected.ID && deployment.ModelName != selected.ModelName {
			continue
		}
		w.SelectedModel.Set(deployment)
		options := resolveProtocolOptions(w.Draft.Get().ProviderSpec, deployment)
		for _, option := range options {
			if option.ID == w.Draft.Get().ProviderProtocol {
				return true
			}
		}
		defaultProtocol := ""
		if !w.derivesProviderProtocol() && len(options) > 0 {
			defaultProtocol = options[0].ID
		}
		w.Draft.Update(func(d readmodel.TargetDraft) readmodel.TargetDraft {
			d.ProviderProtocol = defaultProtocol
			return d
		})
		return true
	}
	return false
}

// SelectModel commits a model choice with the provider's first resolved
// protocol as its editable default.
func (w *TargetConfig) SelectModel(model readmodel.ModelDeploymentReadModel) {
	w.SelectedModel.Set(model)
	w.Draft.Update(func(d readmodel.TargetDraft) readmodel.TargetDraft { d.ProviderProtocol = ""; return d })
	w.Error.Set("")
	if w.derivesProviderProtocol() {
		w.CommitEdit(w.actionContext())
		return
	}
	options := resolveProtocolOptions(w.Draft.Get().ProviderSpec, model)
	if len(options) == 0 {
		w.Error.Set("no supported protocol for selected model")
		return
	}
	w.Draft.Update(func(d readmodel.TargetDraft) readmodel.TargetDraft {
		d.ProviderProtocol = options[0].ID
		return d
	})
	w.CommitEdit(w.actionContext())
}

func (w *TargetConfig) selectModelByID(id string) {
	for _, deployment := range w.catalogResult().Deployments {
		if deployment.ID == id {
			w.SelectModel(deployment)
			return
		}
	}
	w.SelectModel(readmodel.ModelDeploymentReadModel{ID: id, Name: id, ModelName: id})
}

func (w *TargetConfig) selectProtocol(protocol string) {
	protocol = strings.TrimSpace(protocol)
	for _, option := range w.CurrentProtocolOptions() {
		if option.ID != protocol {
			continue
		}
		w.Draft.Update(func(d readmodel.TargetDraft) readmodel.TargetDraft { d.ProviderProtocol = protocol; return d })
		if endpoint := strings.TrimSpace(w.Draft.Get().Endpoint); endpoint != "" {
			if err := validateTargetDraftEndpoint(w.Draft.Get()); err != nil {
				w.Error.Set(err.Error())
				return
			}
		}
		w.Error.Set("")
		w.CommitEdit(w.actionContext())
		return
	}
	if protocol != "" {
		w.Error.Set("unsupported protocol " + protocol)
	}
}

func (w *TargetConfig) SelectProvider(spec string) {
	spec = strings.TrimSpace(spec) // swobu:io-string source=boundary
	w.resetFlowState()
	w.Draft.Update(func(d readmodel.TargetDraft) readmodel.TargetDraft { d.ProviderSpec = spec; return d })
	w.Error.Set("")
	w.seedSetupDefaults()
	seedProviderDefaults(w)
	w.refreshSetup()
	w.Lifecycle.Set(LifecycleOpen)
}

func (w *TargetConfig) SetSetupReady(credentialRef, baseURL string) {
	credentialRef, baseURL = strings.TrimSpace(credentialRef), strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = profile.DefaultExecuteBaseURL(w.Draft.Get().ProviderSpec)
	}
	w.Draft.Update(func(d readmodel.TargetDraft) readmodel.TargetDraft {
		d.CredentialRef = credentialRef
		return d
	})
	w.BaseURL.Set(baseURL)
	w.Error.Set("")
	w.advanceFromSetup()
}

func seedProviderDefaults(w *TargetConfig) {
	if profile.ProviderID(w.Draft.Get().ProviderSpec) == profile.ProviderSpecCustom {
		w.reseedInferredCredentialHeader()
	}
}

// ProbeCatalog resolves the catalog for the current provider and advances
// the phase. If TargetSetupQueries is set it calls the daemon; otherwise
// it falls back to the static catalog for Tier 1-2 providers.
func (w *TargetConfig) ProbeCatalog() {
	if !w.catalogLoading() {
		return
	}
	draft := currentTargetDraft(w.Draft.Get(), w.BaseURL.Get(), "", "", w.Route.ID)
	result, err := probeCatalogSnapshot(
		w.actionContext(),
		w.TargetSetupQueries,
		draft,
	)
	w.SetCatalogResult(result, err)
}

// ReadyAndProbe commits setup inputs. Z.AI stops there because it has no model
// discovery; other providers enter LoadingCatalog and probe asynchronously
// when a live app exists. Callers without a live app must drive ProbeCatalog
// after the loading state is visible.
func (w *TargetConfig) ReadyAndProbe(credentialRef, baseURL string) {
	credentialRef = strings.TrimSpace(credentialRef) // swobu:io-string source=boundary
	baseURL = strings.TrimSpace(baseURL)             // swobu:io-string source=boundary
	if baseURL == "" {
		baseURL = profile.DefaultExecuteBaseURL(w.Draft.Get().ProviderSpec)
	}
	w.Draft.Update(func(d readmodel.TargetDraft) readmodel.TargetDraft {
		d.CredentialRef = credentialRef
		return d
	})
	if !w.IsBedrockFlow() {
		w.BaseURL.Set(baseURL)
	}
	if w.usesManualModelInput() {
		w.invalidateCatalogEvidence()
		w.Error.Set("")
		return
	}
	w.invalidateCatalogEvidence()
	w.startCatalogProbe()
}

// startCatalogProbe owns both the loading transition and probe execution.
func (w *TargetConfig) startCatalogProbe() {
	if w.usesManualModelInput() {
		w.invalidateCatalogEvidence()
		return
	}
	if !w.refreshSetup().Ready() {
		w.Catalog.Set(catalogOperationState{})
		return
	}
	w.invalidateCatalogEvidence()
	w.Catalog.Set(catalogOperationState{Loading: true})
	w.Error.Set("")
	w.launchPendingCatalogProbe()
}

// launchPendingCatalogProbe starts catalog I/O only after the feature has a
// live app that can safely receive the result. Open may establish the loading
// state before mount; the in-flight guard makes BindApp resumption idempotent.
func (w *TargetConfig) launchPendingCatalogProbe() {
	if !w.catalogLoading() || w.catalogProbeInFlight || !w.hasLiveApp() {
		return
	}
	w.catalogProbeInFlight = true
	seq := w.catalogProbeSeq
	app := w.app
	draft := currentTargetDraft(w.Draft.Get(), w.BaseURL.Get(), "", "", w.Route.ID)
	queries := w.TargetSetupQueries
	ctx := w.actionContext()
	go func() {
		result, probeErr := probeCatalogSnapshot(ctx, queries, draft)
		app.QueueUpdate(func() {
			if seq != w.catalogProbeSeq || !w.catalogLoading() {
				return
			}
			w.catalogProbeInFlight = false
			w.SetCatalogResult(result, probeErr)
		})
	}()
}

// ContinueSetup advances from provider-specific setup when the current draft
// snapshot is ready, or starts an interactive auth branch such as ChatGPT.
func (w *TargetConfig) ContinueSetup() {
	if w.Lifecycle.Get() != LifecycleOpen {
		return
	}
	setup := w.refreshSetup()
	if w.RequiresInteractiveAuth() {
		w.startInteractiveAuth()
		return
	}
	if setup.Ready() {
		w.ReadyAndProbe(setup.CredentialRef, w.BaseURL.Get())
		return
	}
}

// SelectPlacement commits the routing choice selected by the placement picker.
func (w *TargetConfig) SelectPlacement(p readmodel.PlacementOptionReadModel) {
	w.Placement.Set(p)
	w.CommitEdit(w.actionContext())
}

func (w *TargetConfig) SelectBedrockRegion(region string) {
	region = strings.TrimSpace(region)
	if region == "" || !profile.SupportsBedrockMantleRegion(region) {
		return
	}
	w.Draft.Update(func(d readmodel.TargetDraft) readmodel.TargetDraft {
		d.Locator = region
		return d
	})
	w.invalidateCatalogEvidence()
	w.Error.Set("")
	w.advanceFromSetup()
	w.CommitEdit(w.actionContext())
}

// SelectZAIAccess commits one closed Z.AI access product. The operator-authored
// open-set model remains stable; the fixed protocol is never draft state.
func (w *TargetConfig) SelectZAIAccess(raw string) {
	access, err := routing.ParseZAIAccess(raw)
	if err != nil {
		w.Error.Set(err.Error())
		return
	}
	w.Draft.Update(func(d readmodel.TargetDraft) readmodel.TargetDraft {
		d.ZAIAccess = string(access)
		return d
	})
	w.Error.Set("")
	w.advanceFromSetup()
}

func (w *TargetConfig) RefreshBedrockIdentity() {
	w.startCatalogProbe()
}

// invalidateCatalogEvidence makes the previous probe non-authoritative without
// destroying the visible selection. The next result reconciles that selection.
func (w *TargetConfig) invalidateCatalogEvidence() {
	w.Catalog.Set(catalogOperationState{})
	w.catalogProbeSeq++
	w.catalogProbeInFlight = false
}

// ChangeProvider returns to the empty provider picker. Changing
// provider is a reset, not a disclosure toggle: the draft provider clears, setup
// state resets, and the view shows the provider picker (ProviderEmpty). Picking
// a new provider re-seeds defaults; Escape from the empty picker abandons.
func (w *TargetConfig) ChangeProvider() {
	if !w.IsOpen() {
		return
	}
	if w.mode == targetConfigModeEdit {
		return
	}
	w.resetFlowState()
	w.Draft.Update(func(d readmodel.TargetDraft) readmodel.TargetDraft { d.ProviderSpec = ""; return d })
	w.Lifecycle.Set(LifecycleOpen)
}

// seedSetupDefaults seeds only parent-owned lifecycle trivia: the default
// endpoint for providers that do not require an explicit one. Provider arms are
// opaque to the parent — each provider seeds its own defaults (e.g. the
// custom-endpoint header defaults at the domain boundary when empty).
func (w *TargetConfig) seedSetupDefaults() {
	spec := w.Draft.Get().ProviderSpec
	if spec == "" {
		return
	}
	if w.BaseURL.Get() == "" && !profile.RequiresLocator(spec) {
		if defaultBaseURL := profile.DefaultExecuteBaseURL(spec); defaultBaseURL != "" {
			w.BaseURL.Set(defaultBaseURL)
		}
	}
}

func (w *TargetConfig) refreshSetup() providerSetupState {
	return w.setupState()
}

func (w *TargetConfig) advanceFromSetup() {
	seedProviderDefaults(w)
	setup := w.refreshSetup()
	if setup.Ready() {
		w.ReadyAndProbe(setup.CredentialRef, w.BaseURL.Get())
		return
	}
}

// invalidateCatalogSelection clears authored model intent only when the
// provider namespace changes. Credential, endpoint, and region refreshes use
// invalidateCatalogEvidence so exact operator input survives discovery drift.
func (w *TargetConfig) invalidateCatalogSelection() {
	w.Catalog.Set(catalogOperationState{})
	w.SelectedModel.Set(readmodel.ModelDeploymentReadModel{})
	w.Draft.Update(func(d readmodel.TargetDraft) readmodel.TargetDraft {
		d.ModelID = ""
		d.ProviderProtocol = ""
		return d
	})
}

// RetryCatalog clears the failed catalog operation and reuses setup inputs.
func (w *TargetConfig) RetryCatalog() {
	if !w.catalogFailed() {
		return
	}
	w.Error.Set("")
	w.Catalog.Set(catalogOperationState{})
	w.ContinueSetup()
}

func probeCatalogSnapshot(ctx context.Context, queries ports.TargetSetupQueries, draft readmodel.TargetDraft) (readmodel.ModelCatalogReadModel, error) {
	provider := strings.TrimSpace(draft.ProviderSpec)
	if queries != nil {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		providerProtocol, _ := profile.ResolveConcreteProtocolForAutoAtBoundary(provider)
		request := ports.ProbeProviderModelsRequest{ProviderProtocol: providerProtocol}
		if profile.ProviderID(provider) == profile.ProviderSpecBedrock {
			request.Probe = ports.BedrockCatalogProbe{
				Region:        strings.TrimSpace(draft.Locator),
				CredentialRef: strings.TrimSpace(draft.CredentialRef),
			}
		} else {
			connection, err := connectionFromDraft(draft)
			if err != nil {
				return readmodel.ModelCatalogReadModel{}, err
			}
			request.Probe = ports.ConnectionCatalogProbe{Connection: connection}
		}
		result, err := queries.ProbeProviderModels(ctx, request)
		if err != nil {
			return result, err
		}
		return result, nil
	}
	return readmodel.ModelCatalogReadModel{}, errors.New("model catalog queries are unavailable")
}

// ---------------------------------------------------------------------------
// Interactive auth (browser/device) — e.g. ChatGPT. The target config drives
// the daemon auth session through ports.TargetAuthCommands and projects session
// state onto the ChatGPT-owned session and shared form error.
// ---------------------------------------------------------------------------

const chatGPTAuthPollInterval = time.Second

func authSubjectLocator(w *TargetConfig) string {
	return fmt.Sprintf("subject:%s#%s", strings.TrimSpace(string(w.WorkspaceID)), strings.TrimSpace(string(w.Route.ID)))
}

func (w *TargetConfig) setAuthFailure(message string) {
	message = strings.TrimSpace(message)
	session := w.AuthSession.Get()
	session.State = "failed"
	session.ErrorMessage = message
	w.AuthSession.Set(session)
	w.Error.Set(message)
}

func (w *TargetConfig) startInteractiveAuth() {
	w.stopAuthSessionObserver()
	mode, _ := w.interactiveAuthMode()
	if mode == "" {
		w.setAuthFailure("interactive auth is unavailable for provider " + w.Draft.Get().ProviderSpec)
		return
	}
	if w.TargetAuthCommands == nil {
		w.setAuthFailure("auth session commands are not wired yet")
		return
	}
	ctx, cancel := context.WithTimeout(w.actionContext(), 10*time.Second)
	defer cancel()
	if w.authSessionFailed() {
		if sessionID := strings.TrimSpace(w.AuthSession.Get().SessionID); sessionID != "" {
			session, err := w.TargetAuthCommands.RetryAuthSession(ctx, sessionID)
			if err != nil {
				w.setAuthFailure(err.Error())
				return
			}
			w.applyAuthSessionResult(session)
			return
		}
	}
	session, err := w.TargetAuthCommands.StartAuthSession(ctx, ports.StartAuthSessionRequest{
		ProviderSpec: w.Draft.Get().ProviderSpec,
		Workspace:    string(w.WorkspaceID),
		Route:        string(w.Route.ID),
		TargetID:     string(w.Target.ID),
		DraftSubject: func() string {
			if w.Target.ID == "" {
				return authSubjectLocator(w)
			}
			return ""
		}(),
		AuthMode: mode,
	})
	if err != nil {
		w.setAuthFailure(err.Error())
		return
	}
	w.applyAuthSessionResult(session)
}

// replaceInteractiveAuth retires the active ChatGPT session before starting a
// session in the newly selected mode. A failed cancel preserves the current
// pending session and prevents parallel browser/device observers.
func (w *TargetConfig) replaceInteractiveAuth(mode chatGPTAuthMode) {
	if !w.authSessionPending() {
		w.ChatGPTAuthMode.Set(mode)
		w.startInteractiveAuth()
		return
	}
	if w.TargetAuthCommands == nil {
		w.Error.Set("auth session commands are not wired yet")
		return
	}

	sessionID := strings.TrimSpace(w.AuthSession.Get().SessionID)
	w.stopAuthSessionObserver()
	if sessionID != "" {
		ctx, cancel := context.WithTimeout(w.actionContext(), 10*time.Second)
		err := w.TargetAuthCommands.CancelAuthSession(ctx, sessionID)
		cancel()
		if err != nil {
			w.Error.Set(err.Error())
			w.launchPendingAuthSessionObserver()
			return
		}
	}

	w.resetFlowState()
	w.Lifecycle.Set(LifecycleOpen)
	w.ChatGPTAuthMode.Set(mode)
	w.startInteractiveAuth()
}

func (w *TargetConfig) RefreshAuthSession() {
	if !w.authSessionPending() {
		return
	}
	session := w.AuthSession.Get()
	if strings.TrimSpace(session.SessionID) == "" {
		return
	}
	if w.TargetAuthCommands == nil {
		w.setAuthFailure("auth session commands are not wired yet")
		return
	}
	w.stopAuthSessionObserver()
	ctx, cancel := context.WithTimeout(w.actionContext(), 10*time.Second)
	defer cancel()
	result, err := w.TargetAuthCommands.PollAuthSession(ctx, session.SessionID)
	if err != nil {
		w.setAuthFailure(err.Error())
		return
	}
	w.applyAuthSessionResult(result)
}

func (w *TargetConfig) CancelAuthSession() {
	w.stopAuthSessionObserver()
	session := w.AuthSession.Get()
	if w.TargetAuthCommands != nil && strings.TrimSpace(session.SessionID) != "" {
		ctx, cancel := context.WithTimeout(w.actionContext(), 10*time.Second)
		_ = w.TargetAuthCommands.CancelAuthSession(ctx, session.SessionID)
		cancel()
	}
	w.resetFlowState()
	w.Lifecycle.Set(LifecycleOpen)
}

func (w *TargetConfig) applyAuthSessionResult(result readmodel.AuthSessionReadModel) {
	current := w.AuthSession.Get()
	if strings.EqualFold(strings.TrimSpace(result.State), "pending") {
		// Poll responses intentionally contain lifecycle state only. Preserve the
		// browser URL and device code established by Start while the same session
		// remains pending.
		if result.AuthorizeURL == "" {
			result.AuthorizeURL = current.AuthorizeURL
		}
		if result.UserCode == "" {
			result.UserCode = current.UserCode
		}
		if result.ProviderSpec == "" {
			result.ProviderSpec = current.ProviderSpec
		}
	}
	w.AuthSession.Set(result)
	switch strings.ToLower(strings.TrimSpace(result.State)) {
	case "", "pending":
		w.Error.Set("")
		w.launchPendingAuthSessionObserver()
	case "succeeded":
		w.stopAuthSessionObserver()
		credentialRef := strings.TrimSpace(result.CredentialRef)
		if credentialRef == "" {
			w.setAuthFailure("auth session succeeded without credential ref")
			return
		}
		w.SetSetupReady(credentialRef, "")
	case "canceled":
		w.stopAuthSessionObserver()
		w.resetFlowState()
		w.Lifecycle.Set(LifecycleOpen)
	case "expired", "failed":
		w.stopAuthSessionObserver()
		msg := strings.TrimSpace(result.ErrorMessage)
		if msg == "" {
			msg = "auth session " + strings.TrimSpace(result.State)
		}
		w.setAuthFailure(msg)
	default:
		w.stopAuthSessionObserver()
		msg := strings.TrimSpace(result.ErrorMessage)
		if msg == "" {
			msg = "auth session " + strings.TrimSpace(result.State)
		}
		w.setAuthFailure(msg)
	}
}

// launchPendingAuthSessionObserver observes the daemon-owned ChatGPT session
// only while this mounted feature truthfully projects it as pending. OAuth and
// credential persistence remain daemon concerns; the observer merely requests
// the next lifecycle projection and returns terminal state through QueueUpdate.
func (w *TargetConfig) launchPendingAuthSessionObserver() {
	if !w.authSessionPending() || w.TargetAuthCommands == nil || !w.hasLiveApp() || w.cancelAuthObserver != nil {
		return
	}
	sessionID := strings.TrimSpace(w.AuthSession.Get().SessionID)
	if sessionID == "" {
		return
	}

	ctx, cancel := context.WithCancel(w.actionContext())
	w.cancelAuthObserver = cancel
	w.authObserverSeq++
	seq := w.authObserverSeq
	app := w.app
	commands := w.TargetAuthCommands

	go func() {
		ticker := time.NewTicker(chatGPTAuthPollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-app.StopCh():
				return
			case <-ticker.C:
				pollCtx, cancelPoll := context.WithTimeout(ctx, 10*time.Second)
				result, err := commands.PollAuthSession(pollCtx, sessionID)
				cancelPoll()
				if err == nil && strings.EqualFold(strings.TrimSpace(result.State), "pending") {
					continue
				}
				app.QueueUpdate(func() {
					if seq != w.authObserverSeq {
						return
					}
					w.cancelAuthObserver = nil
					if err != nil {
						w.setAuthFailure(err.Error())
						return
					}
					w.applyAuthSessionResult(result)
				})
				return
			}
		}
	}()
}

// stopAuthSessionObserver retires the current observer and invalidates any
// result already queued by it. It is safe to call on every terminal/reset edge.
func (w *TargetConfig) stopAuthSessionObserver() {
	w.authObserverSeq++
	if w.cancelAuthObserver != nil {
		w.cancelAuthObserver()
		w.cancelAuthObserver = nil
	}
}

// ---------------------------------------------------------------------------
// Save / create / edit-commit side-effects.
// ---------------------------------------------------------------------------

// Create attempts to persist the target.
func (w *TargetConfig) Create(ctx context.Context) {
	if !w.readyToCreate() {
		w.Error.Set("complete setup")
		return
	}
	if w.SaveTarget == nil {
		message := "target save is not wired yet"
		w.Error.Set(message)
		w.SaveOperation.Set(createOperationState{Err: message})
		return
	}
	model := w.SelectedModel.Get()
	placement := w.Placement.Get()
	protocol := strings.TrimSpace(w.Draft.Get().ProviderProtocol)
	if w.derivesProviderProtocol() {
		protocol = ""
	} else if protocol == "" {
		w.Error.Set("complete setup")
		return
	}
	draft := currentTargetDraft(w.Draft.Get(), w.BaseURL.Get(), model.ModelName, protocol, w.Route.ID)
	if err := validateTargetDraftEndpoint(draft); err != nil {
		w.Error.Set(err.Error())
		w.SaveOperation.Set(createOperationState{Err: err.Error()})
		return
	}
	connection, err := connectionFromDraft(draft)
	if err != nil {
		w.Error.Set(err.Error())
		w.SaveOperation.Set(createOperationState{Err: err.Error()})
		return
	}
	req := ports.SaveTargetRequest{
		WorkspaceID: w.WorkspaceID,
		RouteID:     w.Route.ID,
		TargetID:    w.Target.ID,
		ModelID:     draft.ModelID,
		Protocol:    draft.ProviderProtocol,
		Connection:  connection,
		Placement:   placement,
	}
	saved, err := w.SaveTarget(ctx, req)
	if err != nil {
		w.Error.Set(err.Error())
		w.SaveOperation.Set(createOperationState{Err: err.Error()})
		return
	}
	w.Error.Set("")
	w.SaveOperation.Set(createOperationState{})
	w.Lifecycle.Set(LifecycleCreated)
	if w.OnCreated != nil {
		w.OnCreated(saved)
	}
	if w.OnClose != nil {
		w.OnClose()
	}
}

// RetryCreate repeats the failed save with the current validated draft. Retry
// is an operation, not error dismissal.
func (w *TargetConfig) RetryCreate() {
	if !w.createFailed() {
		return
	}
	w.Create(w.actionContext())
}

// CommitEdit persists the current row-level target facts for an existing target.
// Create mode remains draft-only until the operator activates CreateControl.
func (w *TargetConfig) CommitEdit(ctx context.Context) {
	if w.mode != targetConfigModeEdit {
		return
	}
	if w.SaveTarget == nil {
		w.Error.Set("target save is not wired yet")
		return
	}
	model := w.SelectedModel.Get()
	modelID := strings.TrimSpace(model.ModelName)
	protocol := strings.TrimSpace(w.Draft.Get().ProviderProtocol)
	if w.derivesProviderProtocol() {
		protocol = ""
	} else if protocol == "" {
		return
	}
	if modelID == "" {
		return
	}
	draft := currentTargetDraft(w.Draft.Get(), w.BaseURL.Get(), modelID, protocol, w.Route.ID)
	if err := validateTargetDraftEndpoint(draft); err != nil {
		w.Error.Set(err.Error())
		return
	}
	connection, err := connectionFromDraft(draft)
	if err != nil {
		w.Error.Set(err.Error())
		return
	}
	req := ports.SaveTargetRequest{
		WorkspaceID: w.WorkspaceID,
		RouteID:     w.Route.ID,
		TargetID:    w.Target.ID,
		ModelID:     draft.ModelID,
		Protocol:    draft.ProviderProtocol,
		Connection:  connection,
		Placement:   w.Placement.Get(),
	}
	saved, err := w.SaveTarget(ctx, req)
	if err != nil {
		w.Error.Set(err.Error())
		return
	}
	w.Target = saved.Target
	w.Route = saved.Route
	w.Error.Set("")
	if w.OnSaved != nil {
		w.OnSaved(saved)
	}
}

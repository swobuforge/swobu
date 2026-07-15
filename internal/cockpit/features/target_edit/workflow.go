package target_edit

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
)

// SaveFunc is the narrow command boundary for target create and edit.
type SaveFunc func(context.Context, ports.SaveTargetRequest) (readmodel.TargetReadModel, error)

// DeleteFunc is the narrow command boundary for target delete.
type DeleteFunc func(context.Context, ports.DeleteTargetRequest) error

// Mode describes whether the workflow is editing an existing target or adding
// a new target to the open route.
type Mode int

const (
	ModeEdit Mode = iota
	ModeCreate
)

// Phase is the inline target form lifecycle.
type Phase int

const (
	PhaseEditing Phase = iota
	PhaseConfirmingDelete
	PhaseSubmitting
	PhaseFailed
)

type AuthShape string

const (
	AuthShapeGeneric        AuthShape = "generic"
	AuthShapeBedrock        AuthShape = "bedrock"
	AuthShapeChatGPTBrowser AuthShape = "chatgpt_browser"
	AuthShapeChatGPTDevice  AuthShape = "chatgpt_device"
	AuthShapeAzure          AuthShape = "azure"
)

// Workflow owns the complete inline target create/edit/delete lifecycle.
type Workflow struct {
	WorkspaceID readmodel.WorkspaceID
	Route       readmodel.RouteReadModel
	Target      readmodel.TargetReadModel
	Mode        *tui.State[Mode]
	Phase       *tui.State[Phase]
	Name        *tui.State[string]
	Provider    *tui.State[string]
	Model       *tui.State[string]
	BaseURL     *tui.State[string]
	Credential  *tui.State[string]
	Rank        *tui.State[string]
	Weight      *tui.State[string]
	Error       *tui.State[string]
	Save        SaveFunc
	Delete      DeleteFunc
	OnSaved     func(readmodel.TargetReadModel)
	OnDeleted   func(readmodel.TargetID)
	OnClose     func()
}

// NewEditWorkflow starts an inline edit for an existing target.
func NewEditWorkflow(workspaceID readmodel.WorkspaceID, route readmodel.RouteReadModel, target readmodel.TargetReadModel, save SaveFunc, delete DeleteFunc, onSaved func(readmodel.TargetReadModel), onDeleted func(readmodel.TargetID), onClose func()) *Workflow {
	return newWorkflow(workspaceID, route, target, ModeEdit, save, delete, onSaved, onDeleted, onClose)
}

// NewCreateWorkflow starts an inline add-target form for a route.
func NewCreateWorkflow(workspaceID readmodel.WorkspaceID, route readmodel.RouteReadModel, save SaveFunc, delete DeleteFunc, onSaved func(readmodel.TargetReadModel), onClose func()) *Workflow {
	target := readmodel.TargetReadModel{
		Model:  route.ModelName,
		Rank:   nextRank(route),
		Weight: 1,
	}
	return newWorkflow(workspaceID, route, target, ModeCreate, save, delete, onSaved, nil, onClose)
}

func newWorkflow(workspaceID readmodel.WorkspaceID, route readmodel.RouteReadModel, target readmodel.TargetReadModel, mode Mode, save SaveFunc, delete DeleteFunc, onSaved func(readmodel.TargetReadModel), onDeleted func(readmodel.TargetID), onClose func()) *Workflow {
	rank := target.Rank
	if rank <= 0 {
		rank = nextRank(route)
	}
	weight := target.Weight
	if weight <= 0 {
		weight = 1
	}
	return &Workflow{
		WorkspaceID: workspaceID,
		Route:       route,
		Target:      target,
		Mode:        tui.NewState(mode),
		Phase:       tui.NewState(PhaseEditing),
		Name:        tui.NewState(target.Name),
		Provider:    tui.NewState(target.Provider),
		Model:       tui.NewState(defaultModel(route, target)),
		BaseURL:     tui.NewState(target.BaseURL),
		Credential:  tui.NewState(target.CredentialRef),
		Rank:        tui.NewState(strconv.Itoa(rank)),
		Weight:      tui.NewState(strconv.Itoa(weight)),
		Error:       tui.NewState(""),
		Save:        save,
		Delete:      delete,
		OnSaved:     onSaved,
		OnDeleted:   onDeleted,
		OnClose:     onClose,
	}
}

func (w *Workflow) UpdateProps(fresh tui.Component) {
	f, ok := fresh.(*Workflow)
	if !ok {
		return
	}
	w.WorkspaceID = f.WorkspaceID
	w.Route = f.Route
	w.Target = f.Target
	w.Save = f.Save
	w.Delete = f.Delete
	w.OnSaved = f.OnSaved
	w.OnDeleted = f.OnDeleted
	w.OnClose = f.OnClose
}

func (w *Workflow) KeyMap() tui.KeyMap {
	return tui.KeyMap{
		tui.OnFocused(tui.KeyEscape, func(tui.KeyEvent) { w.Back() }),
	}
}

func (w *Workflow) Back() bool {
	w.Close()
	return true
}

func (w *Workflow) Close() {
	w.Error.Set("")
	if w.OnClose != nil {
		w.OnClose()
	}
}

func (w *Workflow) Submit(ctx context.Context) {
	request, err := w.SaveRequest()
	if err != nil {
		w.Error.Set(err.Error())
		w.Phase.Set(PhaseFailed)
		return
	}
	if w.Save == nil {
		w.Error.Set("target save is not wired yet")
		w.Phase.Set(PhaseFailed)
		return
	}

	w.Error.Set("")
	w.Phase.Set(PhaseSubmitting)
	target, err := w.Save(ctx, request)
	if err != nil {
		w.Error.Set(err.Error())
		w.Phase.Set(PhaseFailed)
		return
	}
	if w.OnSaved != nil {
		w.OnSaved(target)
	}
	w.Close()
}

func (w *Workflow) RequestDelete() {
	if w.Mode.Get() == ModeCreate {
		w.Close()
		return
	}
	w.Error.Set("")
	w.Phase.Set(PhaseConfirmingDelete)
}

func (w *Workflow) ConfirmDelete(ctx context.Context) {
	if w.Mode.Get() == ModeCreate {
		w.Close()
		return
	}
	if w.Delete == nil {
		w.Error.Set("target delete is not wired yet")
		w.Phase.Set(PhaseFailed)
		return
	}
	w.Error.Set("")
	w.Phase.Set(PhaseSubmitting)
	if err := w.Delete(ctx, ports.DeleteTargetRequest{
		WorkspaceID: w.WorkspaceID,
		RouteID:     w.Route.ID,
		TargetID:    w.Target.ID,
	}); err != nil {
		w.Error.Set(err.Error())
		w.Phase.Set(PhaseFailed)
		return
	}
	if w.OnDeleted != nil {
		w.OnDeleted(w.Target.ID)
	}
	w.Close()
}

func (w *Workflow) ActivateSave() {
	w.Submit(context.Background())
}

func (w *Workflow) ActivateDelete() {
	if w.Phase.Get() == PhaseConfirmingDelete {
		w.ConfirmDelete(context.Background())
		return
	}
	w.RequestDelete()
}

func (w *Workflow) DeleteActionLabel() string {
	if w.Mode.Get() == ModeCreate {
		return "cancel ↵"
	}
	if w.Phase.Get() == PhaseConfirmingDelete {
		return "confirm ↵"
	}
	return "delete ↵"
}

func (w *Workflow) DeleteValueLabel() string {
	if w.Mode.Get() == ModeCreate {
		return "new target"
	}
	if w.Phase.Get() == PhaseConfirmingDelete {
		return "delete " + w.targetName() + "?"
	}
	return w.targetName()
}

func (w *Workflow) SaveActionLabel() string {
	if w.Mode.Get() == ModeCreate {
		return "create ↵"
	}
	return "save ↵"
}

func (w *Workflow) ProviderSpec() string {
	return strings.ToLower(strings.TrimSpace(w.Provider.Get())) // swobu:io-string source=boundary
}

func (w *Workflow) AuthShape() AuthShape {
	spec := w.ProviderSpec()
	if spec == "azure" {
		return AuthShapeAzure
	}
	if spec == "bedrock" {
		return AuthShapeBedrock
	}
	if spec == "chatgpt" {
		if strings.Contains(strings.ToLower(strings.TrimSpace(w.Credential.Get())), "device") { // swobu:io-string source=boundary
			return AuthShapeChatGPTDevice
		}
		return AuthShapeChatGPTBrowser
	}
	return AuthShapeGeneric
}

func (w *Workflow) ModelLabel() string {
	if w.AuthShape() == AuthShapeAzure {
		return "deployment"
	}
	return "model"
}

func (w *Workflow) ShowBaseURL() bool {
	switch w.AuthShape() {
	case AuthShapeBedrock, AuthShapeChatGPTBrowser, AuthShapeChatGPTDevice:
		return false
	default:
		return true
	}
}

func (w *Workflow) BaseURLLabel() string {
	if w.AuthShape() == AuthShapeAzure {
		return "resource"
	}
	return "base URL"
}

func (w *Workflow) ShowCredential() bool {
	switch w.AuthShape() {
	case AuthShapeChatGPTBrowser, AuthShapeChatGPTDevice:
		return false
	default:
		return true
	}
}

func (w *Workflow) CredentialLabel() string {
	if w.AuthShape() == AuthShapeBedrock {
		return "AWS auth"
	}
	return "credential"
}

func (w *Workflow) CredentialValue() string {
	if w.AuthShape() == AuthShapeBedrock && strings.TrimSpace(w.Credential.Get()) == "" { // swobu:io-string source=boundary
		return "aws chain/profile"
	}
	return w.Credential.Get()
}

func (w *Workflow) ShowAuthDisclosure() bool {
	switch w.AuthShape() {
	case AuthShapeChatGPTBrowser, AuthShapeChatGPTDevice:
		return true
	default:
		return false
	}
}

func (w *Workflow) AuthDisclosureValue() string {
	if w.AuthShape() == AuthShapeChatGPTDevice {
		return "device code"
	}
	return "browser login"
}

func (w *Workflow) ShowDeviceCode() bool {
	return w.AuthShape() == AuthShapeChatGPTDevice
}

func (w *Workflow) DeviceCodeValue() string {
	value := strings.TrimSpace(w.Credential.Get()) // swobu:io-string source=boundary
	if value == "" || strings.EqualFold(value, "device") {
		return "pending"
	}
	return value
}

// SaveRequest validates draft fields and assembles the target command.
func (w *Workflow) SaveRequest() (ports.SaveTargetRequest, error) {
	provider := strings.TrimSpace(w.Provider.Get()) // swobu:io-string source=boundary
	if provider == "" {
		return ports.SaveTargetRequest{}, errors.New("enter a provider")
	}
	model := strings.TrimSpace(w.Model.Get()) // swobu:io-string source=boundary
	if model == "" {
		return ports.SaveTargetRequest{}, errors.New("enter a model")
	}
	baseURL := strings.TrimSpace(w.BaseURL.Get()) // swobu:io-string source=boundary
	if w.AuthShape() != AuthShapeAzure && w.ShowBaseURL() && baseURL != "" {
		parsed, err := url.ParseRequestURI(baseURL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return ports.SaveTargetRequest{}, fmt.Errorf("enter a valid %s", w.BaseURLLabel())
		}
	}
	rank, err := positiveInt("rank", w.Rank.Get())
	if err != nil {
		return ports.SaveTargetRequest{}, err
	}
	weight, err := positiveInt("weight", w.Weight.Get())
	if err != nil {
		return ports.SaveTargetRequest{}, err
	}
	return ports.SaveTargetRequest{
		WorkspaceID:   w.WorkspaceID,
		RouteID:       w.Route.ID,
		TargetID:      targetIDForMode(w.Mode.Get(), w.Target.ID),
		Name:          strings.TrimSpace(w.Name.Get()), // swobu:io-string source=boundary
		Provider:      provider,
		Model:         model,
		BaseURL:       baseURLForShape(w.AuthShape(), baseURL),
		CredentialRef: strings.TrimSpace(w.Credential.Get()), // swobu:io-string source=boundary
		Rank:          rank,
		Weight:        weight,
	}, nil
}

func (w *Workflow) ErrorMessage() string {
	if w.Error.Get() != "" {
		return w.Error.Get()
	}
	if _, err := w.SaveRequest(); err != nil {
		return err.Error()
	}
	return ""
}

func (w *Workflow) targetName() string {
	if strings.TrimSpace(w.Name.Get()) != "" { // swobu:io-string source=boundary
		return strings.TrimSpace(w.Name.Get()) // swobu:io-string source=boundary
	}
	if w.Target.Name != "" {
		return w.Target.Name
	}
	if w.Target.ID != "" {
		return string(w.Target.ID)
	}
	return "target"
}

func targetIDForMode(mode Mode, targetID readmodel.TargetID) readmodel.TargetID {
	if mode == ModeCreate {
		return ""
	}
	return targetID
}

func defaultModel(route readmodel.RouteReadModel, target readmodel.TargetReadModel) string {
	if target.Model != "" {
		return target.Model
	}
	return route.ModelName
}

func nextRank(route readmodel.RouteReadModel) int {
	maxRank := 0
	for _, target := range route.Targets {
		if target.Rank > maxRank {
			maxRank = target.Rank
		}
	}
	return maxRank + 1
}

func positiveInt(label, raw string) (int, error) {
	value, err := strconv.Atoi(strings.TrimSpace(raw)) // swobu:io-string source=boundary
	if err != nil || value < 1 {
		return 0, fmt.Errorf("%s must be at least 1", label)
	}
	return value, nil
}

func baseURLForShape(shape AuthShape, baseURL string) string {
	switch shape {
	case AuthShapeBedrock, AuthShapeChatGPTBrowser, AuthShapeChatGPTDevice:
		return ""
	default:
		return baseURL
	}
}

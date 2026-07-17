package target_config

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/ui"
	"github.com/swobuforge/swobu/internal/profile"
)

// credentialStage is the credential chooser's drill-down state: closed (just the
// row), menu (pick a source), or one source input (env / file / paste).
type credentialStage int

const (
	credStageClosed credentialStage = iota
	credStageMenu
	credStageEnv
	credStageFile
	credStagePaste
)

// credentialRow owns transient credential interaction state. TargetConfig owns
// only the persisted credential reference on its durable target draft.
type credentialRow struct {
	target       *TargetConfig
	autoFocus    bool
	providerSpec string
	stage        *tui.State[credentialStage]
	envName      *tui.State[string]
	filePath     *tui.State[string]
	secret       *tui.State[string]
}

var credentialRowForMount func(*TargetConfig, bool) *credentialRow

func newCredentialRow(target *TargetConfig, autoFocus bool) *credentialRow {
	return &credentialRow{
		target: target, autoFocus: autoFocus, providerSpec: target.Draft.Get().ProviderSpec,
		stage: tui.NewState(credStageClosed), envName: tui.NewState(""),
		filePath: tui.NewState(""), secret: tui.NewState(""),
	}
}

func (r *credentialRow) BindApp(app *tui.App) {
	r.stage.BindApp(app)
	r.envName.BindApp(app)
	r.filePath.BindApp(app)
	r.secret.BindApp(app)
}
func (r *credentialRow) UnbindApp() {}

func (r *credentialRow) KeyMap() tui.KeyMap {
	return ui.BackScope(func() bool { return r.stage.Get() != credStageClosed }, r.retreat)
}

// CredentialControlRegion returns the mountable credential chooser renderer.
func CredentialControlRegion(w *TargetConfig, autoFocus ...bool) tui.Component {
	focus := len(autoFocus) > 0 && autoFocus[0]
	if credentialRowForMount != nil {
		return credentialRowForMount(w, focus)
	}
	return newCredentialRow(w, focus)
}

func FileCredentialBrowser(r *credentialRow) *ui.FileBrowser {
	w, dir := r.target, r.filePath.Get()
	if info, err := os.Stat(dir); dir != "" && err == nil && !info.IsDir() { dir = filepath.Dir(dir) }
	browser := ui.NewFileBrowser(TargetAddMountKey(w, "file-browser"), "credential file", dir, ui.OSReadDir, func(path string) { r.filePath.Set(path); r.saveFile(path) }, r.retreat)
	browser.AutoFocus = true
	return browser
}

func credentialRegionKey(w *TargetConfig) string {
	return TargetAddMountKey(w, "credential:"+strings.TrimSpace(w.Draft.Get().ProviderSpec))
}

func (r *credentialRow) open() {
	w := r.target
	if !w.IsOpen() || strings.TrimSpace(w.Draft.Get().ProviderSpec) == "" {
		return
	}
	r.stage.Set(credStageMenu)
}

func (r *credentialRow) selectRef(ref string) {
	w := r.target
	ref = strings.TrimSpace(ref) // swobu:io-string source=boundary
	if ref == "" {
		return
	}
	d := w.Draft.Get()
	d.CredentialRef = ref
	w.Draft.Set(d)
	r.stage.Set(credStageClosed)
	w.advanceFromSetup()
	w.CommitEdit(w.actionContext())
}

func (r *credentialRow) enter(stage credentialStage) bool {
	if r.stage.Get() != credStageMenu {
		return false
	}
	r.stage.Set(stage)
	return true
}

func (r *credentialRow) retreat() {
	if r.stage.Get() == credStageMenu {
		r.stage.Set(credStageClosed)
		return
	}
	r.stage.Set(credStageMenu)
}

func (r *credentialRow) reset() {
	r.stage.Set(credStageClosed)
	r.envName.Set("")
	r.filePath.Set("")
	r.secret.Set("")
}

func (r *credentialRow) openEnv() {
	if !r.enter(credStageEnv) {
		return
	}
	if strings.TrimSpace(r.envName.Get()) == "" {
		r.envName.Set(credentialEnvNameSuggestionForSpec(r.target.Draft.Get().ProviderSpec))
	}
}

func (r *credentialRow) saveEnv(raw string) {
	w := r.target
	name := strings.TrimSpace(raw) // swobu:io-string source=boundary
	if name == "" {
		w.Error.Set("env var first")
		return
	}
	w.Error.Set("")
	r.selectRef("env:" + name)
}

func (r *credentialRow) openFile() {
	r.enter(credStageFile)
}

func (r *credentialRow) saveFile(raw string) {
	w := r.target
	path := strings.TrimSpace(raw) // swobu:io-string source=boundary
	if path == "" {
		w.Error.Set("file path first")
		return
	}
	w.Error.Set("")
	r.selectRef("file:" + path)
}

func (r *credentialRow) openPaste() {
	r.enter(credStagePaste)
}

func (r *credentialRow) savePasted(ctx context.Context) {
	w := r.target
	secret := strings.TrimSpace(r.secret.Get()) // swobu:io-string source=boundary
	if secret == "" {
		w.Error.Set("secret first")
		return
	}
	if w.CredentialCommands == nil {
		w.Error.Set("credential store is not wired yet")
		return
	}
	result, err := w.CredentialCommands.StorePastedCredential(ctx, ports.StorePastedCredentialRequest{
		ProviderSpec: w.Draft.Get().ProviderSpec,
		Name:         w.generatedPastedSecretSlot(),
		Secret:       secret,
	})
	if err != nil {
		w.Error.Set(err.Error())
		return
	}
	ref := strings.TrimSpace(result.CredentialRef)
	if ref == "" {
		w.Error.Set("credential store returned empty ref")
		return
	}
	r.secret.Set("")
	w.Error.Set("")
	r.selectRef(ref)
}

func (w *TargetConfig) generatedPastedSecretSlot() string {
	parts := []string{
		"cockpit",
		"target",
		safeCredentialSlotPart(w.Draft.Get().ProviderSpec, "provider"),
		safeCredentialSlotPart(string(w.WorkspaceID), "workspace"),
		safeCredentialSlotPart(string(w.Route.ID), "route"),
	}
	targetPart := string(w.Target.ID)
	if strings.TrimSpace(targetPart) == "" {
		targetPart = w.SelectedModel.Get().ModelName
	}
	parts = append(parts, safeCredentialSlotPart(targetPart, "target"))
	parts = append(parts, randomCredentialSlotSuffix())
	return strings.Join(parts, "/")
}

func safeCredentialSlotPart(raw string, fallback string) string {
	raw = strings.TrimSpace(strings.ToLower(raw)) // swobu:io-string source=boundary
	var out []rune
	lastDash := false
	for _, r := range raw {
		allowed := unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-'
		if allowed {
			out = append(out, r)
			lastDash = false
			continue
		}
		if !lastDash {
			out = append(out, '-')
			lastDash = true
		}
	}
	cleaned := strings.Trim(string(out), "-_")
	if cleaned == "" {
		return fallback
	}
	return cleaned
}

func randomCredentialSlotSuffix() string {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err == nil {
		return hex.EncodeToString(buf[:])
	}
	return strings.ToLower(strconv.FormatInt(time.Now().UTC().UnixNano(), 36))
}

func (r *credentialRow) selectNone() {
	w := r.target
	if !providerAllowsNoCredential(w) {
		w.Error.Set("credential first")
		return
	}
	d := w.Draft.Get()
	d.CredentialRef = ""
	w.Draft.Set(d)
	w.Error.Set("")
	r.stage.Set(credStageClosed)
	w.advanceFromSetup()
	w.CommitEdit(w.actionContext())
}

// genericCredentialDisplayVisible reports whether the credential row should show
// a value even with no credential ref — true when setup is ready and "none" was
// committed. Provider-agnostic; the readiness projection carries the meaning.
func genericCredentialDisplayVisible(w *TargetConfig) bool {
	setup := w.setupState()
	return setup.ReadyForCatalog && strings.TrimSpace(setup.CredentialLabel) == "none"
}

func (w *TargetConfig) ProviderAllowsNoCredential() bool {
	return providerAllowsNoCredential(w)
}

// CredentialControl shows the credential ref that unlocked catalog probing. The
// blocked state is provider-owned (CredentialBlockedReason); the chooser itself
// is the shared primitive.
func CredentialControl(r *credentialRow) *ui.SelectableRow { return CredentialControlWithAutoFocus(r) }

func CredentialControlWithAutoFocus(r *credentialRow) *ui.SelectableRow {
	w := r.target
	value := credentialDisplay(w)
	action := "change ↵"
	if strings.TrimSpace(value) == "" {
		value = "required"
		action = "choose ↵"
	}
	row := ui.NewSelectableRow(
		TargetAddMountKey(w, "credential-display"),
		"credential",
		value,
		action,
		r.open,
	)
	setup := w.setupState()
	row.AutoFocus = strings.TrimSpace(w.Draft.Get().CredentialRef) == "" &&
		setup.CredentialRequired && !setup.RequiresEndpoint && !setup.AuthModeRequired &&
		r.stage.Get() == credStageClosed && r.autoFocus
	return row
}

func CredentialNoneOption(r *credentialRow) *ui.SelectableRow {
	w := r.target
	row := ui.NewSelectableRow(
		TargetAddMountKey(w, "credential-none-action"),
		"",
		"none",
		"select ↵",
		r.selectNone,
	)
	row.AutoFocus = true
	row.OnEscape = r.retreat
	return row
}

func CredentialEnvOption(r *credentialRow) *ui.SelectableRow {
	w := r.target
	row := ui.NewSelectableRow(
		TargetAddMountKey(w, "credential-env-action"),
		"",
		"env var",
		"select ↵",
		r.openEnv,
	)
	row.AutoFocus = !w.ProviderAllowsNoCredential()
	row.OnEscape = r.retreat
	return row
}

func CredentialFileOption(r *credentialRow) *ui.SelectableRow {
	w := r.target
	row := ui.NewSelectableRow(
		TargetAddMountKey(w, "credential-file-action"),
		"",
		"file",
		"select ↵",
		r.openFile,
	)
	row.OnEscape = r.retreat
	return row
}

func CredentialPasteSecretOption(r *credentialRow) *ui.SelectableRow {
	w := r.target
	row := ui.NewSelectableRow(
		TargetAddMountKey(w, "credential-paste-secret-action"),
		"",
		"paste secret",
		"select ↵",
		r.openPaste,
	)
	row.OnEscape = r.retreat
	return row
}

func EnvCredentialInput(r *credentialRow) *ui.EditableRow {
	w := r.target
	row := ui.NewEditableRow(
		TargetAddMountKey(w, "credential-env-input"),
		"env var",
		r.envName,
	)
	row.Placeholder = "_"
	row.ViewAction = "edit ↵"
	row.EditAction = "save ↵"
	row.AutoFocus = true
	row.OnSubmit = func(raw string) {
		r.envName.Set(strings.TrimSpace(raw))
		r.saveEnv(r.envName.Get())
	}
	row.OnClose = r.retreat
	return row
}

func PasteSecretInput(r *credentialRow) *ui.EditableRow {
	w := r.target
	row := ui.NewEditableRow(
		TargetAddMountKey(w, "credential-secret-value"),
		"secret",
		r.secret,
	)
	row.Placeholder = "_"
	row.ViewAction = "edit ↵"
	row.EditAction = "save ↵"
	row.AutoFocus = true
	row.StartEditing = true
	row.OnSubmit = func(raw string) {
		r.savePasted(w.actionContext())
	}
	row.OnClose = r.retreat
	return row
}

func credentialSuggestion(provider profile.Profile) string {
	if envKey := strings.TrimSpace(provider.DefaultCredentialEnvVar); envKey != "" {
		return envKey
	}
	return "credential"
}

func credentialEnvSuggestionForSpec(spec string) string {
	envKey := credentialEnvNameSuggestionForSpec(spec)
	if envKey == "" {
		return ""
	}
	return "env:" + envKey
}

func credentialEnvNameSuggestionForSpec(spec string) string {
	return strings.TrimSpace(profile.DefaultEnvKeyForSpec(spec)) // swobu:io-string source=domain
}

templ (r *credentialRow) Render() {
	<div class="flex-col w-full">
		@CredentialControlWithAutoFocus(r)
		if r.stage.Get() != credStageClosed {
			<div class="pl-3 flex-col w-full">
				if r.stage.Get() == credStageMenu {
					if r.target.ProviderAllowsNoCredential() { @CredentialNoneOption(r) }
					@CredentialEnvOption(r)
					@CredentialFileOption(r)
					@CredentialPasteSecretOption(r)
				} else if r.stage.Get() == credStageEnv { @EnvCredentialInput(r)
				} else if r.stage.Get() == credStageFile { @FileCredentialBrowser(r)
				} else if r.stage.Get() == credStagePaste { @PasteSecretInput(r) }
			</div>
		}
	</div>
}

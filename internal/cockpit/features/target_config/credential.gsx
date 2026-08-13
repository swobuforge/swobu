package target_config

import (
	"os"
	"path/filepath"
	"strings"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/ui"
	"github.com/swobuforge/swobu/internal/domain/credentialref"
)

func credentialRefDisplay(raw string) string {
	ref := strings.TrimSpace(raw) // swobu:io-string source=boundary
	parsed := credentialref.Parse(ref)
	_, payload, hasPrefix := strings.Cut(parsed.String(), ":")
	if !hasPrefix { payload = parsed.String() }
	payload = strings.TrimSpace(payload) // swobu:io-string source=boundary
	switch parsed.Kind() {
	case credentialref.KindEnv:
		return credentialSourceDisplay(parsed) + " · " + payload
	case credentialref.KindFile:
		return credentialSourceDisplay(parsed) + " · " + payload
	case credentialref.KindSecret, credentialref.KindSecretFile:
		return credentialSourceDisplay(parsed) + " credential"
	default:
		return ref
	}
}

func credentialSourceDisplay(ref credentialref.Ref) string {
	switch ref.Kind() {
	case credentialref.KindEnv: return "environment"
	case credentialref.KindFile: return "file"
	case credentialref.KindSecret, credentialref.KindSecretFile: return "stored"
	default: return "credential"
	}
}

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
	props        CredentialFieldProps
	stage        *tui.State[credentialStage]
	envName      *tui.State[string]
	filePath     *tui.State[string]
	secret       *tui.State[string]
	localError   *tui.State[string]
	readDir      func(string) ([]ui.FileBrowserEntry, error)
}

// CredentialFieldProps is the complete feature-to-component contract. The
// credential component owns chooser interaction only; provider and target
// workflow policy stay in the composing form.
type CredentialFieldProps struct {
	ID           string
	Optional     bool
	SuggestedEnvVar string
	Ref          string
	AutoFocus    bool
	Apply        func(string)
	Store        func(string) (string, error)
	ChoiceAction string
}

func newCredentialField(props CredentialFieldProps) *credentialRow {
	return &credentialRow{
		props: props,
		stage: tui.NewState(credStageClosed), envName: tui.NewState(""),
		filePath: tui.NewState(""), secret: tui.NewState(""), localError: tui.NewState(""), readDir: ui.OSReadDir,
	}
}

func (r *credentialRow) key(suffix string) string { return r.props.ID + ":" + suffix }
func (r *credentialRow) optional() bool { return r.props.Optional }
func (r *credentialRow) fail(message string) { r.localError.Set(message) }

func (r *credentialRow) BindApp(app *tui.App) {
	r.stage.BindApp(app)
	r.envName.BindApp(app)
	r.filePath.BindApp(app)
	r.secret.BindApp(app)
	r.localError.BindApp(app)
}
func (r *credentialRow) UnbindApp() {}

func (r *credentialRow) KeyMap() tui.KeyMap {
	return ui.BackScope(func() bool { return r.stage.Get() != credStageClosed }, r.retreat)
}

func FileCredentialBrowser(r *credentialRow) *ui.FileBrowser {
	dir := r.filePath.Get()
	if info, err := os.Stat(dir); dir != "" && err == nil && !info.IsDir() { dir = filepath.Dir(dir) }
	browser := ui.NewFileBrowser(r.key("file-browser"), "credential file", dir, r.readDir, func(path string) { r.filePath.Set(path); r.saveFile(path) }, r.retreat)
	browser.AutoFocus = true
	return browser
}

func (r *credentialRow) open() {
	r.stage.Set(credStageMenu)
}

func (r *credentialRow) selectRef(ref string) {
	ref = strings.TrimSpace(ref) // swobu:io-string source=boundary
	if ref == "" {
		return
	}
	r.stage.Set(credStageClosed)
	if r.props.Apply != nil { r.props.Apply(ref) }
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
		r.envName.Set(strings.TrimSpace(r.props.SuggestedEnvVar))
	}
}

func (r *credentialRow) saveEnv(raw string) {
	name := strings.TrimSpace(raw) // swobu:io-string source=boundary
	if name == "" {
		r.fail("variable is required")
		return
	}
	r.fail("")
	r.selectRef("env:" + name)
}

func (r *credentialRow) openFile() {
	r.enter(credStageFile)
}

func (r *credentialRow) saveFile(raw string) {
	path := strings.TrimSpace(raw) // swobu:io-string source=boundary
	if path == "" {
		r.fail("file path is required")
		return
	}
	r.fail("")
	r.selectRef("file:" + path)
}

func (r *credentialRow) openPaste() {
	r.enter(credStagePaste)
}

func (r *credentialRow) savePasted() {
	secret := strings.TrimSpace(r.secret.Get()) // swobu:io-string source=boundary
	if secret == "" {
		r.fail("secret is required")
		return
	}
	if r.props.Store == nil {
		r.fail("credential store is not wired yet")
		return
	}
	ref, err := r.props.Store(secret)
	if err != nil {
		r.fail(err.Error())
		return
	}
	r.fail("")
	if r.props.Apply != nil { r.props.Apply(ref) }
	r.secret.Set("")
	r.stage.Set(credStageClosed)
}

func (r *credentialRow) selectNone() {
	if !r.optional() {
		r.fail("credential is required")
		return
	}
	r.fail("")
	r.stage.Set(credStageClosed)
	if r.props.Apply != nil { r.props.Apply("") }
}

func CredentialControlWithAutoFocus(r *credentialRow) *ui.SelectableRow {
	value := credentialRefDisplay(r.props.Ref)
	label := "credential"
	action := "change ↵"
	if strings.TrimSpace(value) == "" {
		if r.optional() {
			value, action = "none", "add ↵"
		} else {
			value, action = "required", "choose ↵"
		}
	}
	if r.stage.Get() != credStageClosed {
		action = "close ↵"
	}
	row := ui.NewSelectableRow(
		r.key("display"),
		label,
		value,
		action,
		r.open,
	)
	row.AutoFocus = strings.TrimSpace(r.props.Ref) == "" && r.stage.Get() == credStageClosed && r.props.AutoFocus
	return row
}

func CredentialRemoveOption(r *credentialRow) *ui.SelectableRow {
	row := ui.NewSelectableRow(
		r.key("none-action"),
		"",
		"remove credential",
		"remove ↵",
		r.selectNone,
	)
	row.AutoFocus = false
	return row
}

func CredentialEnvOption(r *credentialRow) *ui.SelectableRow {
	action := strings.TrimSpace(r.props.ChoiceAction)
	if action == "" { action = "enter ↵" }
	row := ui.NewSelectableRow(
		r.key("env-action"),
		"",
		"environment variable",
		action,
		r.openEnv,
	)
	row.AutoFocus = true
	return row
}

func CredentialFileOption(r *credentialRow) *ui.SelectableRow {
	action := strings.TrimSpace(r.props.ChoiceAction)
	if action == "" { action = "enter ↵" }
	row := ui.NewSelectableRow(
		r.key("file-action"),
		"",
		"file",
		action,
		r.openFile,
	)
	return row
}

func CredentialPasteSecretOption(r *credentialRow) *ui.SelectableRow {
	action := strings.TrimSpace(r.props.ChoiceAction)
	if action == "" { action = "enter ↵" }
	row := ui.NewSelectableRow(
		r.key("paste-secret-action"),
		"",
		"paste credential",
		action,
		r.openPaste,
	)
	return row
}

func EnvCredentialInput(r *credentialRow) *ui.EditableRow {
	row := ui.NewEditableRow(
		r.key("env-input"),
		"variable",
		r.envName,
	)
	row.Placeholder = "_"
	row.ViewAction = "edit ↵"
	row.EditAction = "save ↵"
	row.StartEditing = true
	row.AutoFocus = true
	row.OnSubmit = func(raw string) {
		r.envName.Set(strings.TrimSpace(raw))
		r.saveEnv(r.envName.Get())
	}
	row.OnClose = r.retreat
	return row
}

func PasteSecretInput(r *credentialRow) *ui.EditableRow {
	row := ui.NewEditableRow(
		r.key("secret-value"),
		"secret",
		r.secret,
	)
	row.Placeholder = "_"
	row.Sensitive = true
	row.ViewAction = "edit ↵"
	row.EditAction = "save ↵"
	row.AutoFocus = true
	row.StartEditing = true
	row.OnSubmit = func(raw string) {
		r.secret.Set(strings.TrimSpace(raw))
		r.savePasted()
	}
	row.OnClose = r.retreat
	return row
}

templ (r *credentialRow) Render() {
	<div class="flex-col w-full" deps={r.localError}>
		@CredentialControlWithAutoFocus(r)
		if r.stage.Get() != credStageClosed {
			<div class="pl-3 flex-col w-full">
				if r.stage.Get() == credStageMenu {
					@CredentialEnvOption(r)
					@CredentialFileOption(r)
					@CredentialPasteSecretOption(r)
					if r.optional() && strings.TrimSpace(r.props.Ref) != "" { @CredentialRemoveOption(r) }
				} else if r.stage.Get() == credStageEnv { @EnvCredentialInput(r)
				} else if r.stage.Get() == credStageFile { @FileCredentialBrowser(r)
				} else if r.stage.Get() == credStagePaste { @PasteSecretInput(r) }
			</div>
			if strings.TrimSpace(r.localError.Get()) != "" { @CredentialInputError(r.localError.Get()) }
		}
	</div>
}

templ CredentialInputError(message string) {
	<div class="flex-row w-full"><span class="w-18"></span><span class="grow truncate nowrap">{message}</span></div>
}

type credentialChooserBody struct{ row *credentialRow }
func CredentialChooser(r *credentialRow) *credentialChooserBody { return &credentialChooserBody{row: r} }

// Render exposes the shared source chooser without its generic credential
// header so provider-owned fields can embed the same mechanism.
templ (b *credentialChooserBody) Render() {
	<div class="flex-col w-full" deps={b.row.localError}>
		if b.row.stage.Get() == credStageMenu {
			@CredentialEnvOption(b.row)
			@CredentialFileOption(b.row)
			@CredentialPasteSecretOption(b.row)
		} else if b.row.stage.Get() == credStageEnv { @EnvCredentialInput(b.row)
		} else if b.row.stage.Get() == credStageFile { @FileCredentialBrowser(b.row)
		} else if b.row.stage.Get() == credStagePaste { @PasteSecretInput(b.row) }
		if strings.TrimSpace(b.row.localError.Get()) != "" { @CredentialInputError(b.row.localError.Get()) }
	</div>
}

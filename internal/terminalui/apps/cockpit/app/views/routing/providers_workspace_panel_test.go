package routing

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/views"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
)

func TestIsEmptyFileCredentialRef(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ref  string
		want bool
	}{
		{name: "empty file ref", ref: "file:", want: true},
		{name: "empty file ref with spaces", ref: " file:   ", want: true},
		{name: "file path set", ref: "file:/tmp/key.txt", want: false},
		{name: "env ref", ref: "env:OPENAI_API_KEY", want: false},
		{name: "blank", ref: "", want: false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isEmptyFileCredentialRef(tt.ref); got != tt.want {
				t.Fatalf("isEmptyFileCredentialRef(%q)=%v want %v", tt.ref, got, tt.want)
			}
		})
	}
}

func TestApplyAddModelCredentialSourceChoice_FileAndEnvResetModelAndPersistRef(t *testing.T) {
	t.Parallel()

	base := state.ProviderConfigSnapshot{
		ProviderSpec:  "openrouter",
		CredentialRef: "",
		ModelID:       "some-model",
	}
	fileNext := applyAddModelCredentialSourceChoice(base, "file")
	if got := fileNext.CredentialRef; got != "file" {
		t.Fatalf("file credential ref=%q want %q", got, "file")
	}
	if got := fileNext.ModelID; got != "" {
		t.Fatalf("file model id=%q want cleared", got)
	}
	envNext := applyAddModelCredentialSourceChoice(base, "env")
	if got := envNext.CredentialRef; got == "" || got == "env" {
		t.Fatalf("env credential ref=%q want concrete env key", got)
	}
	if got := envNext.ModelID; got != "" {
		t.Fatalf("env model id=%q want cleared", got)
	}
}

func TestApplyAddModelCredentialFilePathChoice_PersistsSelectedFilePath(t *testing.T) {
	t.Parallel()

	base := state.ProviderConfigSnapshot{
		ProviderSpec:  "openrouter",
		CredentialRef: "file",
		ModelID:       "old-model",
	}
	next := applyAddModelCredentialFilePathChoice(base, "/tmp/openrouter.key")
	if want := "file:/tmp/openrouter.key"; next.CredentialRef != want {
		t.Fatalf("credential ref=%q want %q", next.CredentialRef, want)
	}
	if got := next.ModelID; got != "" {
		t.Fatalf("model id=%q want cleared", got)
	}
}

func TestDefaultAddModelCredentialUIState_InitializesClosedAndDeterministic(t *testing.T) {
	t.Parallel()

	ui := defaultAddModelCredentialUIState("/tmp/openrouter.key")
	if ui.SourcePickerOpen || ui.FilePickerOpen {
		t.Fatalf("expected picker state closed by default: %+v", ui)
	}
	if got := credentialFileBrowserPath(ui.FileBrowse.Dir); got == "" {
		t.Fatalf("expected deterministic browse path, got empty")
	}
}

func TestCloseAddModelCredentialUIState_ClosesOnlyModalFlags(t *testing.T) {
	t.Parallel()

	ui := defaultAddModelCredentialUIState("/tmp/openrouter.key")
	ui.SourcePickerOpen = true
	ui.FilePickerOpen = true
	closed := closeAddModelCredentialUIState(ui)
	if closed.SourcePickerOpen || closed.FilePickerOpen {
		t.Fatalf("expected both pickers closed: %+v", closed)
	}
	if credentialFileBrowserPath(closed.FileBrowse.Dir) != credentialFileBrowserPath(ui.FileBrowse.Dir) {
		t.Fatalf("expected browse state preserved when closing pickers")
	}
}

func TestAddModelCredentialFilePickerItems_ParentChooseUpdatesBrowseAndDoesNotResetFromStaleState(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	child := filepath.Join(tmp, "child")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatalf("mkdir child: %v", err)
	}
	ui := defaultAddModelCredentialUIState(child)
	ui.FileBrowse = credentialFileBrowseState{Dir: child}
	ui.FilePicker = views.FilterablePickerState{Query: "abc", Cursor: 2, Offset: 1}

	var updated addModelCredentialUIState
	items, err := addModelCredentialFilePickerItems(ui, func(next addModelCredentialUIState) {
		updated = next
	}, "", nil)
	if err != nil {
		t.Fatalf("addModelCredentialFilePickerItems err: %v", err)
	}
	var found bool
	var actions []any
	for _, item := range items {
		if item.Label != "../" {
			continue
		}
		found = true
		for _, act := range item.OnChoose() {
			actions = append(actions, act)
		}
		break
	}
	if !found {
		t.Fatalf("missing parent entry ../ in %#v", items)
	}
	if updated.FileBrowse.Dir != tmp {
		t.Fatalf("browse dir=%q want parent=%q", updated.FileBrowse.Dir, tmp)
	}
	if updated.FilePicker.Query != "" || updated.FilePicker.Cursor != 0 || updated.FilePicker.Offset != 0 {
		t.Fatalf("expected picker query reset on directory change, got %+v", updated.FilePicker)
	}
	if len(actions) != 1 {
		t.Fatalf("actions len=%d want 1", len(actions))
	}
	focus, ok := actions[0].(interaction.FocusKeyAction)
	if !ok {
		t.Fatalf("action[0]=%T want interaction.FocusKeyAction", actions[0])
	}
	if focus.Key != views.FilterablePickerFocusKey("credential-file-option", 0) {
		t.Fatalf("focus key=%q want %q", focus.Key, views.FilterablePickerFocusKey("credential-file-option", 0))
	}
}

func TestBuildAddModelProviderItems_SelectProviderFocusesCredentialsWhenRequired(t *testing.T) {
	t.Parallel()

	panel := addModelPanelState{
		credentialUI:          defaultAddModelCredentialUIState(""),
		setDraft:              func(state.ProviderConfigSnapshot) {},
		setProviderPickerOpen: func(bool) {},
		setModelPickerOpen:    func(bool) {},
		setCredentialUI:       func(addModelCredentialUIState) {},
	}
	items := buildAddModelProviderItems(state.Model{}, state.ProviderConfigSnapshot{Ref: "provider-a"}, panel)
	item := findProviderItemBySearch(t, items, "openrouter")
	actions := item.OnChoose()
	focus := findFocusAction(t, actions)
	if focus.Key != "add-model/credentials" {
		t.Fatalf("focus key=%q want add-model/credentials for auth-required provider", focus.Key)
	}
}

func TestBuildAddModelProviderItems_SelectProviderFocusesModelWhenCredentialsNotRequired(t *testing.T) {
	t.Parallel()

	panel := addModelPanelState{
		credentialUI:          defaultAddModelCredentialUIState(""),
		setDraft:              func(state.ProviderConfigSnapshot) {},
		setProviderPickerOpen: func(bool) {},
		setModelPickerOpen:    func(bool) {},
		setCredentialUI:       func(addModelCredentialUIState) {},
	}
	items := buildAddModelProviderItems(state.Model{}, state.ProviderConfigSnapshot{Ref: "provider-a"}, panel)
	item := findProviderItemBySearch(t, items, "ollama")
	actions := item.OnChoose()
	focus := findFocusAction(t, actions)
	if focus.Key != "add-model/model" {
		t.Fatalf("focus key=%q want add-model/model for no-auth provider", focus.Key)
	}
}

func findProviderItemBySearch(t *testing.T, items []views.FilterablePickerItem, term string) views.FilterablePickerItem {
	t.Helper()
	for _, item := range items {
		if strings.Contains(strings.ToLower(item.Search), strings.ToLower(term)) {
			return item
		}
	}
	t.Fatalf("provider item with search term %q not found", term)
	return views.FilterablePickerItem{}
}

func findFocusAction(t *testing.T, actions []update.Action) interaction.FocusKeyAction {
	t.Helper()
	for _, action := range actions {
		if focus, ok := action.(interaction.FocusKeyAction); ok {
			return focus
		}
	}
	t.Fatalf("no interaction.FocusKeyAction in %#v", actions)
	return interaction.FocusKeyAction{}
}

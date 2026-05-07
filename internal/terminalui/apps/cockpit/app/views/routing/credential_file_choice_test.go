package routing

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
)

func TestCredentialFilePickerItems_DirectoryChoiceDelegatesResetAndFocus(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	child := filepath.Join(tmp, "child")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatalf("mkdir child: %v", err)
	}

	browse := credentialFileBrowseState{Dir: tmp}
	updated := browse
	resetCalled := false

	items, err := credentialFilePickerItems(
		browse,
		func(next credentialFileBrowseState) { updated = next },
		func() []update.Action {
			resetCalled = true
			return []update.Action{interaction.FocusKeyAction{Key: "credential-file-option/0"}}
		},
		"",
		nil,
	)
	if err != nil {
		t.Fatalf("credentialFilePickerItems err: %v", err)
	}
	var dirItemFound bool
	var actions []update.Action
	for _, item := range items {
		if item.Label == "child/" {
			dirItemFound = true
			actions = item.OnChoose()
			break
		}
	}
	if !dirItemFound {
		t.Fatalf("missing directory item child/ in %#v", items)
	}
	if updated.Dir != child {
		t.Fatalf("updated dir=%q want %q", updated.Dir, child)
	}
	if !resetCalled {
		t.Fatal("expected directory choose callback to be invoked")
	}
	if len(actions) != 1 {
		t.Fatalf("actions len=%d want 1", len(actions))
	}
	if _, ok := actions[0].(interaction.FocusKeyAction); !ok {
		t.Fatalf("action[0]=%T want interaction.FocusKeyAction", actions[0])
	}
}

func TestCredentialFilePickerItems_ParentEntryNavigatesUp(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	child := filepath.Join(tmp, "child")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatalf("mkdir child: %v", err)
	}

	browse := credentialFileBrowseState{Dir: child}
	updated := browse

	items, err := credentialFilePickerItems(
		browse,
		func(next credentialFileBrowseState) { updated = next },
		nil,
		"",
		nil,
	)
	if err != nil {
		t.Fatalf("credentialFilePickerItems err: %v", err)
	}
	var parentFound bool
	for _, item := range items {
		if item.Label != "../" {
			continue
		}
		parentFound = true
		_ = item.OnChoose()
		break
	}
	if !parentFound {
		t.Fatalf("missing parent entry ../ in %#v", items)
	}
	if updated.Dir != tmp {
		t.Fatalf("updated dir=%q want parent %q", updated.Dir, tmp)
	}
}

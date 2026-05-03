package routing

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/views"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/view"
)

func credentialFileRow(value string, onActivate func() []update.Action, onCancel func() []update.Action) view.ViewSpec[state.Model] {
	summary := strings.TrimSpace(value)
	if summary == "" {
		summary = "not set"
	}
	return views.RowActionWithCancel("credential file", summary, "browse", onActivate, onCancel)
}

type credentialFileBrowseState struct {
	Dir string
}

type credentialFileEntry struct {
	Path  string
	Label string
	IsDir bool
}

func initialCredentialFileBrowseState(currentPath string) credentialFileBrowseState {
	dir := strings.TrimSpace(currentPath)
	if dir != "" {
		if info, err := os.Stat(dir); err == nil {
			if info.IsDir() {
				return credentialFileBrowseState{Dir: dir}
			}
			return credentialFileBrowseState{Dir: filepath.Dir(dir)}
		}
		candidate := filepath.Dir(dir)
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return credentialFileBrowseState{Dir: candidate}
		}
	}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		candidate := filepath.Join(home, ".config", "swobu")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return credentialFileBrowseState{Dir: candidate}
		}
		return credentialFileBrowseState{Dir: candidate}
	}
	return credentialFileBrowseState{Dir: "."}
}

func credentialFilePickerItems(
	browse credentialFileBrowseState,
	setBrowse func(credentialFileBrowseState),
	onChooseDir func() []update.Action,
	currentPath string,
	onChooseFile func(string) []update.Action,
) ([]views.FilterablePickerItem, error) {
	b := browse
	if strings.TrimSpace(b.Dir) == "" {
		b = initialCredentialFileBrowseState(currentPath)
		setBrowse(b)
	}
	entries, err := credentialFileEntries(b.Dir)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, nil
	}
	items := make([]views.FilterablePickerItem, 0, len(entries))
	for _, entry := range entries {
		path := entry.Path
		entryIsDir := entry.IsDir
		label := entry.Label
		items = append(items, views.FilterablePickerItem{
			Label: label,
			OnChoose: func() []update.Action {
				if entryIsDir {
					next := browse
					next.Dir = path
					setBrowse(next)
					if onChooseDir != nil {
						return onChooseDir()
					}
					return []update.Action{interaction.FocusKeyAction{Key: "credential-file"}}
				}
				if onChooseFile != nil {
					return onChooseFile(path)
				}
				return nil
			},
		})
	}
	return items, nil
}

func credentialFileBrowserPath(dir string) string {
	path := filepath.ToSlash(strings.TrimSpace(dir))
	if path == "" {
		path = "."
	}
	if !strings.HasSuffix(path, "/") {
		path += "/"
	}
	return path
}

func credentialFileEntries(dir string) ([]credentialFileEntry, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		dir = "."
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	items, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make([]credentialFileEntry, 0, len(items)+1)
	parent := filepath.Dir(dir)
	if parent != dir {
		out = append(out, credentialFileEntry{
			Path:  parent,
			Label: "../",
			IsDir: true,
		})
	}
	for _, item := range items {
		name := strings.TrimSpace(item.Name())
		if name == "" {
			continue
		}
		path := filepath.Join(dir, name)
		label := name
		if item.IsDir() {
			label += "/"
		}
		out = append(out, credentialFileEntry{
			Path:  path,
			Label: label,
			IsDir: item.IsDir(),
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].IsDir != out[j].IsDir {
			return out[i].IsDir
		}
		return strings.ToLower(out[i].Label) < strings.ToLower(out[j].Label)
	})
	return out, nil
}

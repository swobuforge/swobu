package workspace

import (
	"sync"

	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
)

var addRouteFocusHandoff = struct {
	mu   sync.Mutex
	seen map[readmodel.WorkspaceID]struct{}
}{
	seen: make(map[readmodel.WorkspaceID]struct{}),
}

func requestAddRouteFocusAfterSave(workspace readmodel.WorkspaceReadModel) {
	key := workspaceFocusKey(workspace)
	if key == "" {
		return
	}

	addRouteFocusHandoff.mu.Lock()
	addRouteFocusHandoff.seen[key] = struct{}{}
	addRouteFocusHandoff.mu.Unlock()
}

func consumeAddRouteFocusAfterSave(workspace readmodel.WorkspaceReadModel) bool {
	key := workspaceFocusKey(workspace)
	if key == "" {
		return false
	}

	addRouteFocusHandoff.mu.Lock()
	defer addRouteFocusHandoff.mu.Unlock()
	if _, ok := addRouteFocusHandoff.seen[key]; !ok {
		return false
	}
	delete(addRouteFocusHandoff.seen, key)
	return true
}

func workspaceFocusKey(workspace readmodel.WorkspaceReadModel) readmodel.WorkspaceID {
	if workspace.ID != "" {
		return workspace.ID
	}
	if workspace.Slug != "" {
		return readmodel.WorkspaceID(workspace.Slug)
	}
	return readmodel.WorkspaceID("+")
}

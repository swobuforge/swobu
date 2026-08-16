package workspace_overview

import (
	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/clientconnect"
	workspace_connect "github.com/swobuforge/swobu/internal/cockpit/features/workspace_connect"
	cockpitui "github.com/swobuforge/swobu/internal/cockpit/ui"
)

func endpointRowKey(s *SectionView) string {
	return "workspace-connect:" + workspaceIdentity(s) + ":" + s.Model.WorkspaceURL
}

// EndpointRowComponent projects the workspace address into the feature that
// owns Connect discovery, disclosure, copying, and automatic wiring.
func EndpointRowComponent(s *SectionView) tui.Component {
	target, err := clientconnect.NewTarget(s.Model.Slug, s.Model.WorkspaceURL)
	if err != nil {
		return cockpitui.NewSelectableRow(endpointRowKey(s)+":invalid", "endpoint", s.Model.WorkspaceURL, "", nil)
	}
	return workspace_connect.New(target, nil, s.OnNotice)
}

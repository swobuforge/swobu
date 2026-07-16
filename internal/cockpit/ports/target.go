package ports

import "github.com/swobuforge/swobu/internal/cockpit/readmodel"

// SaveTargetRequest describes an add or edit request for one route target.
type SaveTargetRequest struct {
	WorkspaceID      readmodel.WorkspaceID
	RouteID          readmodel.RouteID
	TargetID         readmodel.TargetID
	Name             string
	Provider         string
	Model            string
	ProviderProtocol string
	BaseURL          string
	CredentialRef    string
	Rank             int
	Weight           int
}

// DeleteTargetRequest names the target to remove from a route.
type DeleteTargetRequest struct {
	WorkspaceID readmodel.WorkspaceID
	RouteID     readmodel.RouteID
	TargetID    readmodel.TargetID
}

package effect

import (
	"context"
	"strings"

	stateModel "github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state/model"
)

// CheckClientAccessEffect probes the daemon's endpoint canonical.
type CheckClientAccessEffect struct {
	EndpointName   string
	ProviderConfig stateModel.ProviderConfigSnapshot
}

func (cmd CheckClientAccessEffect) Run(ctx context.Context) any {
	endpointName := strings.TrimSpace(cmd.EndpointName) // swobu:io-string source=boundary
	if endpointName == "" {
		return ClientAccessCheckFailed{Message: "workspace is not selected"}
	}
	outcome, err := operatorClient().CheckClientAccess(ctx, endpointName, cmd.ProviderConfig.ModelID)
	if err != nil {
		return ClientAccessCheckFailed{Message: "client access check could not reach the daemon"}
	}
	return ClientAccessChecked{
		Status:  outcome.Status,
		Message: outcome.Message,
	}
}

// ClientAccessCheckFailed reports that a client access check failed.
type ClientAccessCheckFailed struct{ Message string }

// ClientAccessChecked reports the result of a client access check.
type ClientAccessChecked struct {
	Status  string
	Message string
}

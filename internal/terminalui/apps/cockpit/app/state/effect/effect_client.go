package effect

import (
	"context"
	"strings"

	stateModel "github.com/metrofun/swobu/internal/terminalui/apps/cockpit/app/state/model"
	"github.com/metrofun/swobu/internal/terminalui/engine/retained/update"
)

// CheckClientAccessEffect probes the daemon's endpoint compatibility.
type CheckClientAccessEffect struct {
	EndpointName   string
	ProviderConfig stateModel.ProviderConfigSnapshot
}

func (cmd CheckClientAccessEffect) Execute(ctx context.Context) []update.Action {
	endpointName := strings.TrimSpace(cmd.EndpointName)
	if endpointName == "" {
		return []update.Action{ClientAccessCheckFailed{Message: "workspace is not selected"}}
	}
	outcome, err := operatorClient().CheckClientAccess(ctx, endpointName, cmd.ProviderConfig.ModelID)
	if err != nil {
		return []update.Action{ClientAccessCheckFailed{Message: "client access check could not reach the daemon"}}
	}
	return []update.Action{ClientAccessChecked{
		Status:  outcome.Status,
		Message: outcome.Message,
	}}
}

// ClientAccessCheckFailed reports that a client access check failed.
type ClientAccessCheckFailed struct{ Message string }

// ClientAccessChecked reports the result of a client access check.
type ClientAccessChecked struct {
	Status  string
	Message string
}

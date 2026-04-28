package effect

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	stateModel "github.com/metrofun/swobu/internal/adapters/inbound/tui/app/state/model"
	"github.com/metrofun/swobu/internal/adapters/inbound/tui/engine/update"
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
	modelID := strings.TrimSpace(cmd.ProviderConfig.ModelID)
	if modelID == "" {
		modelID = "healthcheck"
	}
	body := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"ping"}],"stream":false}`, modelID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, daemonURL()+"/c/"+endpointName+"/chat/completions", strings.NewReader(body))
	if err != nil {
		return []update.Action{ClientAccessCheckFailed{Message: "client access request could not be built"}}
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient().Do(req)
	if err != nil {
		return []update.Action{ClientAccessCheckFailed{Message: "client access check could not reach the daemon"}}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return []update.Action{ClientAccessChecked{
			Status:  "reachable",
			Message: fmt.Sprintf("compatibility request succeeded with status %d", resp.StatusCode),
		}}
	}
	raw, _ := io.ReadAll(resp.Body)
	message := strings.TrimSpace(string(raw))
	if message == "" {
		message = fmt.Sprintf("compatibility request returned status %d", resp.StatusCode)
	}
	return []update.Action{ClientAccessChecked{
		Status:  fmt.Sprintf("backend %d", resp.StatusCode),
		Message: message,
	}}
}

// ClientAccessCheckFailed reports that a client access check failed.
type ClientAccessCheckFailed struct{ Message string }

// ClientAccessChecked reports the result of a client access check.
type ClientAccessChecked struct {
	Status  string
	Message string
}

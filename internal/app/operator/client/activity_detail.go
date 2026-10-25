package operatorclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/execution"
)

type ActivityOperationDetail struct {
	Kind     string                  `json:"kind"`
	Evidence string                  `json:"evidence"`
	Exchange *ActivityExchangeDetail `json:"exchange,omitempty"`
	MCP      *ActivityMCPOperation   `json:"mcp_operation,omitempty"`
}

// ActivityUpstreamExecution only describes recorded invocations. An empty
// history is not proof of no upstream execution or no external side effects.
type ActivityUpstreamExecution struct {
	Recorded        int `json:"recorded_invocations"`
	Occurred        int `json:"occurred"`
	MayHaveOccurred int `json:"may_have_occurred"`
	DidNotOccur     int `json:"did_not_occur"`
	Unsettled       int `json:"unsettled"`
}

type ActivityExchangeDetail struct {
	Identity    string                     `json:"exchange_id"`
	Access      string                     `json:"access_id"`
	Workspace   string                     `json:"workspace"`
	Started     string                     `json:"started_at"`
	Settled     string                     `json:"settled_at,omitempty"`
	Result      string                     `json:"delivery_result,omitempty"`
	ErrorCode   string                     `json:"error_code,omitempty"`
	Upstream    *ActivityUpstreamExecution `json:"upstream_execution"`
	Status      int                        `json:"http_status,omitempty"`
	Invocations []ActivityInvocationDetail `json:"invocations"`
}

type ActivityInvocationDetail struct {
	ConnectionID       string `json:"connection_id,omitempty"`
	ConnectionRevision uint64 `json:"connection_revision,omitempty"`
	Route              string `json:"route,omitempty"`

	Identity   string               `json:"invocation_id"`
	Kind       string               `json:"kind"`
	Started    string               `json:"started_at"`
	Settled    string               `json:"settled_at,omitempty"`
	Source     string               `json:"source,omitempty"`
	Tool       string               `json:"tool,omitempty"`
	Provider   string               `json:"provider,omitempty"`
	Model      string               `json:"model,omitempty"`
	Target     string               `json:"target,omitempty"`
	Version    uint64               `json:"target_version,omitempty"`
	Purpose    string               `json:"purpose,omitempty"`
	Outcome    execution.Outcome    `json:"outcome,omitempty"`
	Occurrence execution.Occurrence `json:"execution_possibility,omitempty"`
	Failure    string               `json:"failure,omitempty"`
}

// ActivityOperation reads one exact content-free lifecycle, not an authority or
// completeness decision. Missing evidence and failed reads never trigger retry.
func (client *Client) ActivityOperation(ctx context.Context, identity string) (ActivityOperationDetail, error) {
	if strings.HasPrefix(identity, "ex_") {
		if _, err := execution.ParseExchangeID(identity); err != nil {
			return ActivityOperationDetail{}, fmt.Errorf("operator client: exact Activity identity required")
		}
	} else {
		if !strings.HasPrefix(identity, "mcp_") || len(identity) <= 4 || len(identity) > 256 {
			return ActivityOperationDetail{}, fmt.Errorf("operator client: exact Activity identity required")
		}
		for _, character := range identity {
			if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '_' || character == '-') {
				return ActivityOperationDetail{}, fmt.Errorf("operator client: exact Activity identity required")
			}
		}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, client.baseURL+"/api/v1/activity/operations/"+identity, nil)
	if err != nil {
		return ActivityOperationDetail{}, fmt.Errorf("operator client: Activity detail request unavailable")
	}
	response, err := client.http.Do(request)
	if err != nil {
		return ActivityOperationDetail{}, fmt.Errorf("operator client: Activity detail unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return ActivityOperationDetail{}, errorFromResponse(response, "operator client: Activity detail failed")
	}
	var detail ActivityOperationDetail
	decoder := json.NewDecoder(response.Body)
	if decoder.Decode(&detail) != nil || decoder.Decode(&struct{}{}) != io.EOF || (detail.Evidence != "memory" && detail.Evidence != "retained") {
		return ActivityOperationDetail{}, fmt.Errorf("operator client: invalid Activity detail acknowledgement")
	}
	if strings.HasPrefix(identity, "ex_") {
		if detail.Kind != "exchange" || detail.Exchange == nil || detail.MCP != nil || detail.Exchange.Identity != identity || detail.Exchange.Invocations == nil {
			return ActivityOperationDetail{}, fmt.Errorf("operator client: invalid Exchange detail binding")
		}
		if !validRecordedUpstreamExecution(detail.Exchange.Upstream, detail.Exchange.Invocations) {
			return ActivityOperationDetail{}, fmt.Errorf("operator client: invalid Activity execution summary")
		}
	} else if detail.Kind != "mcp_operation" || detail.Exchange != nil || detail.MCP == nil || detail.MCP.OperationID != identity {
		return ActivityOperationDetail{}, fmt.Errorf("operator client: invalid MCP detail binding")
	}
	return detail, nil
}

// Validate the server's derived counters against the original invocation facts.
// An absent or novel possibility must not be silently classified as safe to
// repeat, even if the outer client delivery has failed.
func validRecordedUpstreamExecution(upstream *ActivityUpstreamExecution, invocations []ActivityInvocationDetail) bool {
	if upstream == nil || upstream.Recorded != len(invocations) || upstream.Occurred < 0 || upstream.MayHaveOccurred < 0 || upstream.DidNotOccur < 0 || upstream.Unsettled < 0 {
		return false
	}
	counts := ActivityUpstreamExecution{Recorded: len(invocations)}
	for _, invocation := range invocations {
		switch invocation.Occurrence {
		case execution.Occurred:
			counts.Occurred++
		case execution.MayHaveOccurred:
			counts.MayHaveOccurred++
		case execution.DidNotOccur:
			counts.DidNotOccur++
		case "":
			counts.Unsettled++
		default:
			return false
		}
	}
	return counts == *upstream
}

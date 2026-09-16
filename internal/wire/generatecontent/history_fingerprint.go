package generatecontent

import (
	"encoding/json"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/historyfingerprint"
)

const fingerprintScheme historyfingerprint.Scheme = "generate-content/v1"

func fingerprintContentsRequest(contents []contentDTO) (historyfingerprint.Request, error) {
	raw, err := json.Marshal(contents)
	if err != nil {
		return historyfingerprint.Request{}, err
	}
	framed, err := historyfingerprint.FrameJSONValue(raw)
	if err != nil {
		return historyfingerprint.Request{}, err
	}
	return historyfingerprint.FingerprintRequest(fingerprintScheme, framed)
}

func fingerprintContentsResponse(content contentDTO) (historyfingerprint.Response, error) {
	raw, err := json.Marshal(content)
	if err != nil {
		return historyfingerprint.Response{}, err
	}
	framed, err := historyfingerprint.FrameJSONValue(raw)
	if err != nil {
		return historyfingerprint.Response{}, err
	}
	return historyfingerprint.FingerprintResponse(fingerprintScheme, framed)
}

type historyResult struct {
	previous *historyfingerprint.History
	request  historyfingerprint.Request
	current  []contentDTO
}

func fingerprintHistory(contents []contentDTO) (historyResult, error) {
	var previous *historyfingerprint.History
	requestStart := 0
	for index, content := range contents {
		owner, err := contentOwner(content)
		if err != nil {
			return historyResult{}, err
		}
		if owner != canonical.TurnOwnerAssistant || index == len(contents)-1 {
			continue
		}
		nextOwner, err := contentOwner(contents[index+1])
		if err != nil {
			return historyResult{}, err
		}
		if nextOwner == canonical.TurnOwnerAssistant {
			return historyResult{}, canonical.BadRequest("GenerateContent consecutive assistant contents are unsupported")
		}
		if nextOwner != canonical.TurnOwnerUser {
			continue
		}
		request, err := fingerprintContentsRequest(contents[requestStart:index])
		if err != nil {
			return historyResult{}, err
		}
		response, err := fingerprintContentsResponse(content)
		if err != nil {
			return historyResult{}, err
		}
		history, err := historyfingerprint.Advance(previous, request, response)
		if err != nil {
			return historyResult{}, err
		}
		previous = &history
		requestStart = index + 1
	}
	current := append([]contentDTO(nil), contents[requestStart:]...)
	request, err := fingerprintContentsRequest(current)
	if err != nil {
		return historyResult{}, err
	}
	return historyResult{previous: previous, request: request, current: current}, nil
}

func contentOwner(content contentDTO) (canonical.TurnOwner, error) {
	if len(content.Parts) == 0 {
		return "", canonical.BadRequest("GenerateContent content requires parts")
	}
	role := content.Role
	if role == "" {
		role = "user"
	}
	var owner canonical.TurnOwner
	for _, part := range content.Parts {
		partOwner, err := classifyPart(role, part)
		if err != nil {
			return "", err
		}
		if owner != "" && owner != partOwner {
			return "", canonical.BadRequest("GenerateContent content mixes caller and assistant ownership")
		}
		owner = partOwner
	}
	return owner, nil
}

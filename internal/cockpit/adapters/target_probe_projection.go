package adapters

import (
	"encoding/json"
	"errors"

	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
)

// decodeBedrockAuthenticationDiagnostics validates opaque transport data at
// the Cockpit adapter edge so feature components consume only typed evidence.
func decodeBedrockAuthenticationDiagnostics(raw json.RawMessage) (readmodel.BedrockAuthenticationEvidence, error) {
	var evidence readmodel.BedrockAuthenticationEvidence
	if err := json.Unmarshal(raw, &evidence); err != nil {
		return readmodel.BedrockAuthenticationEvidence{}, errors.New("invalid Bedrock authentication diagnostics: " + err.Error())
	}
	switch evidence.Authentication {
	case readmodel.BedrockAuthenticationExplicitAPIKey, readmodel.BedrockAuthenticationAWSIdentity:
		return evidence, nil
	default:
		return readmodel.BedrockAuthenticationEvidence{}, errors.New("invalid Bedrock authentication diagnostics: unknown authentication kind")
	}
}

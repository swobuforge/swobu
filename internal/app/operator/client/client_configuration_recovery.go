package operatorclient

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"

	connectapp "github.com/swobuforge/swobu/internal/app/operator/connect"
	"github.com/swobuforge/swobu/internal/clientconnect"
)

type clientConfigurationRecoveryEnvelope struct {
	Version int             `json:"version"`
	Key     string          `json:"retry_key"`
	Request json.RawMessage `json:"request"`
}

// RecoveryRecord returns detached original Connect intent, not approval or a
// receipt. It contains private before-values; adapters must publish it privately
// before sending effects and must never log it or disclose it to Admin models.
func (intent ClientConfigurationIntent) RecoveryRecord() ([]byte, error) {
	if intent.body == "" {
		return nil, errors.New("retained Connect intent required")
	}
	record, err := json.Marshal(clientConfigurationRecoveryEnvelope{Version: 1, Key: intent.key, Request: json.RawMessage(intent.body)})
	if err != nil || len(record) > 1<<20 {
		return nil, errors.New("Connect recovery record exceeds retention limit")
	}
	return record, nil
}

// RestoreClientConfigurationIntent validates a closed canonical local record
// without I/O, fresh preview or approval. Missing, null, duplicate and unknown
// members reject rather than refreshing original evidence. Apply still requires
// fresh consumer-owned confirmation and local Node admission.
func RestoreClientConfigurationIntent(record []byte) (ClientConfigurationIntent, error) {
	invalid := errors.New("invalid Connect recovery record")
	if len(record) > 1<<20 {
		return ClientConfigurationIntent{}, invalid
	}
	var envelope clientConfigurationRecoveryEnvelope
	decoder := json.NewDecoder(bytes.NewReader(record))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&envelope) != nil || decoder.Decode(&struct{}{}) != io.EOF || envelope.Version != 1 {
		return ClientConfigurationIntent{}, invalid
	}
	var request struct {
		Workspace string            `json:"workspace"`
		ModelID   string            `json:"model_id"`
		Client    string            `json:"client"`
		Principal string            `json:"principal"`
		Review    connectapp.Review `json:"review"`
	}
	decoder = json.NewDecoder(bytes.NewReader(envelope.Request))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return ClientConfigurationIntent{}, invalid
	}
	changes := make([]clientconnect.Change, 0, len(request.Review.Changes))
	for _, change := range request.Review.Changes {
		changes = append(changes, clientconnect.Change{Field: change.Field, Before: change.Before, BeforeExists: change.BeforeExists, After: change.After})
	}
	review := clientconnect.Plan{Changes: changes}
	intent, err := NewClientConfigurationIntent(connectapp.Plan{
		Revision: request.Review.Revision, Workspace: request.Workspace, ModelID: request.ModelID,
		Client: connectapp.Client{ID: request.Client}, ConfigPaths: request.Review.ConfigPaths,
		Changes: request.Review.Changes, AlreadyConfigured: review.AlreadyConfigured(),
		RequiresReplace: review.RequiresReplace(), Authentication: request.Review.Authentication,
	}, envelope.Key)
	if err != nil || intent.body != string(envelope.Request) {
		return ClientConfigurationIntent{}, invalid
	}
	canonical, err := intent.RecoveryRecord()
	if err != nil || !bytes.Equal(canonical, record) {
		return ClientConfigurationIntent{}, invalid
	}
	return intent, nil
}

// Review returns detached original evidence for trusted recovery confirmation,
// never current file readiness. Private before-values must remain local.
func (intent ClientConfigurationIntent) Review() (connectapp.Plan, error) {
	if intent.body == "" {
		return connectapp.Plan{}, errors.New("retained Connect intent required")
	}
	var request struct {
		Review connectapp.Review `json:"review"`
	}
	if json.Unmarshal([]byte(intent.body), &request) != nil {
		return connectapp.Plan{}, errors.New("invalid retained Connect intent")
	}
	changes := make([]clientconnect.Change, 0, len(request.Review.Changes))
	for _, change := range request.Review.Changes {
		changes = append(changes, clientconnect.Change{Field: change.Field, Before: change.Before, BeforeExists: change.BeforeExists, After: change.After})
	}
	review := clientconnect.Plan{Changes: changes}
	return connectapp.Plan{Revision: intent.revision, Workspace: intent.workspace, ModelID: intent.modelID, Client: connectapp.Client{ID: intent.client}, ConfigPaths: request.Review.ConfigPaths, Changes: request.Review.Changes, AlreadyConfigured: review.AlreadyConfigured(), RequiresReplace: review.RequiresReplace(), Authentication: request.Review.Authentication}, nil
}

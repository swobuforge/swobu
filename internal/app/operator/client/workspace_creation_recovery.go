package operatorclient

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"

	workspaceapi "github.com/swobuforge/swobu/internal/app/operator/workspaces"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/routing"
)

// creationRecoveryEnvelope is a closed local persistence format, not a public
// command or approval. The request preserves generated target identity and
// opaque credential locators; storage owners must keep it private and never log it.
type creationRecoveryEnvelope struct {
	Version int             `json:"version"`
	Key     string          `json:"retry_key"`
	Request json.RawMessage `json:"request"`
}

// WorkspaceSlug identifies the retained resource without exposing its payload.
func (intent WorkspaceCreationIntent) WorkspaceSlug() string { return intent.slug }

// WorkspaceCreationRecoveryIdentity exposes only original attribution needed to
// mount a recovery review. It contains no connection, credential locator or
// approval. RetryKey is attribution, never Node admission.
type WorkspaceCreationRecoveryIdentity struct {
	Slug, DisplayName, Route, TargetID, RetryKey string
}

// RecoveryIdentity projects the frozen request, never current daemon metadata.
func (intent WorkspaceCreationIntent) RecoveryIdentity() (WorkspaceCreationRecoveryIdentity, error) {
	if len(intent.body) == 0 || intent.key == "" {
		return WorkspaceCreationRecoveryIdentity{}, errors.New("retained creation intent required")
	}
	var command workspaceapi.CreateWorkspace
	if json.Unmarshal(intent.body, &command) != nil {
		return WorkspaceCreationRecoveryIdentity{}, errors.New("invalid retained creation intent")
	}
	return WorkspaceCreationRecoveryIdentity{Slug: intent.slug, DisplayName: intent.name, Route: intent.route, TargetID: command.Target.ID, RetryKey: intent.key}, nil
}

// RecoveryRecord returns a detached canonical record of the original request.
// It grants no approval or admission and must not be exposed as diagnostic text.
// The adapter owns private durable publication before any possibly effectful send.
func (intent WorkspaceCreationIntent) RecoveryRecord() ([]byte, error) {
	if len(intent.body) == 0 || intent.key == "" {
		return nil, errors.New("retained creation intent required")
	}
	return json.Marshal(creationRecoveryEnvelope{Version: 1, Key: intent.key, Request: intent.body})
}

// RestoreWorkspaceCreationIntent validates a closed canonical recovery record
// without HTTP, credential resolution or consent. Apply still needs fresh trusted
// confirmation. Noncanonical bytes reject instead of silently changing intent.
func RestoreWorkspaceCreationIntent(record []byte) (WorkspaceCreationIntent, error) {
	invalid := errors.New("invalid Workspace creation recovery record")
	var envelope creationRecoveryEnvelope
	decoder := json.NewDecoder(bytes.NewReader(record))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&envelope) != nil || decoder.Decode(&struct{}{}) != io.EOF || envelope.Version != 1 {
		return WorkspaceCreationIntent{}, invalid
	}
	var command workspaceapi.CreateWorkspace
	decoder = json.NewDecoder(bytes.NewReader(envelope.Request))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&command) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return WorkspaceCreationIntent{}, invalid
	}
	if command.InitialRoute != "" {
		// Reuse the domain's construction law: a serialized connection document
		// alone does not establish valid target identity or credential references.
		_, err := routing.FinalizeTarget(routing.TargetDraft{
			ID: command.Target.ID, Model: command.Target.Model,
			Protocol: command.Target.Protocol, ConnectionID: command.Target.ConnectionID,
		}, profile.RoutingConstructionFacts())
		if err != nil {
			return WorkspaceCreationIntent{}, invalid
		}
	}
	intent, err := NewWorkspaceCreationIntent(command, envelope.Key)
	if err != nil || !bytes.Equal(intent.body, envelope.Request) {
		return WorkspaceCreationIntent{}, invalid
	}
	canonical, err := intent.RecoveryRecord()
	if err != nil || !bytes.Equal(canonical, record) {
		// Reject duplicate members, unknown data, whitespace rewrites and omitted
		// fields even if encoding/json could decode them to the same Go values.
		return WorkspaceCreationIntent{}, invalid
	}
	return intent, nil
}

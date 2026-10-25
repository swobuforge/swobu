package operatorclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/swobuforge/swobu/internal/app/operator/authplane"
)

// AuthStartIntent freezes the original provider handoff and retry key. It grants
// no approval or Node admission. Consumers must privately retain RecoveryRecord
// before sending; uncertain recovery keeps this key and cannot start a new flow.
type AuthStartIntent struct {
	request authStartRequest
	key     string
}

type authStartRequest struct {
	ProviderSpec string `json:"provider_spec"`
	Workspace    string `json:"workspace"`
	Route        string `json:"route"`
	TargetID     string `json:"target_id"`
	DraftSubject string `json:"draft_subject"`
	AuthMode     string `json:"auth_mode"`
}

type authStartRecovery struct {
	Version int              `json:"version"`
	Key     string           `json:"retry_key"`
	Request authStartRequest `json:"request"`
}

// NewAuthStartIntent retains exact caller-authored subject/mode/key without reads,
// key generation, credential resolution or effects. Provider validation remains
// with the application owner; this boundary admits only explicit browser/device.
func NewAuthStartIntent(in authplane.StartInput, key string) (AuthStartIntent, error) {
	invalid := errors.New("exact authentication start intent and original retry key required")
	if in.ProviderSpec == "" || in.ProviderSpec != strings.ToLower(strings.TrimSpace(in.ProviderSpec)) || (in.AuthMode != "browser" && in.AuthMode != "device") || len(key) < 16 || len(key) > 256 {
		return AuthStartIntent{}, invalid
	}
	for _, b := range []byte(key) {
		if b < 33 || b > 126 {
			return AuthStartIntent{}, invalid
		}
	}
	for _, value := range []string{in.Workspace, in.Route, in.TargetID, in.DraftSubject} {
		if value != strings.TrimSpace(value) {
			return AuthStartIntent{}, invalid
		}
	}
	if in.DraftSubject == "" && (in.Workspace == "" || in.Route == "" || in.TargetID == "") {
		return AuthStartIntent{}, invalid
	}
	return AuthStartIntent{request: authStartRequest{in.ProviderSpec, in.Workspace, in.Route, in.TargetID, in.DraftSubject, in.AuthMode}, key: key}, nil
}

// Review returns detached original handoff intent for trusted confirmation.
func (intent AuthStartIntent) Review() (authplane.StartInput, string, error) {
	r := intent.request
	in := authplane.StartInput{ProviderSpec: r.ProviderSpec, Workspace: r.Workspace, Route: r.Route, TargetID: r.TargetID, DraftSubject: r.DraftSubject, AuthMode: r.AuthMode}
	if _, err := NewAuthStartIntent(in, intent.key); err != nil {
		return authplane.StartInput{}, "", err
	}
	return in, intent.key, nil
}

// RecoveryRecord contains private original subject/key, never provider codes or
// credentials. Publication belongs to the consumer and must precede dispatch.
func (intent AuthStartIntent) RecoveryRecord() ([]byte, error) {
	if _, _, err := intent.Review(); err != nil {
		return nil, err
	}
	return json.Marshal(authStartRecovery{Version: 1, Key: intent.key, Request: intent.request})
}

// RestoreAuthStartIntent admits only exact canonical original bytes; it performs
// no Node contact or consent refresh and cannot resolve an uncertain provider ID.
func RestoreAuthStartIntent(record []byte) (AuthStartIntent, error) {
	invalid := errors.New("invalid authentication start recovery record")
	var envelope authStartRecovery
	decoder := json.NewDecoder(bytes.NewReader(record))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&envelope) != nil || decoder.Decode(&struct{}{}) != io.EOF || envelope.Version != 1 {
		return AuthStartIntent{}, invalid
	}
	r := envelope.Request
	intent, err := NewAuthStartIntent(authplane.StartInput{ProviderSpec: r.ProviderSpec, Workspace: r.Workspace, Route: r.Route, TargetID: r.TargetID, DraftSubject: r.DraftSubject, AuthMode: r.AuthMode}, envelope.Key)
	if err != nil {
		return AuthStartIntent{}, invalid
	}
	canonical, err := intent.RecoveryRecord()
	if err != nil || !bytes.Equal(canonical, record) {
		return AuthStartIntent{}, invalid
	}
	return intent, nil
}

// ApplyAuthStartIntent sends the retained canonical handoff once. Failure or
// invalid acknowledgement means uncertainty; never generate another key or
// fall back to the legacy start. Node replay returns reconciliation, not success.
func (c *Client) ApplyAuthStartIntent(ctx context.Context, intent AuthStartIntent) (AuthSessionStartResult, error) {
	in, key, err := intent.Review()
	if err != nil {
		return AuthSessionStartResult{}, err
	}
	body, err := json.Marshal(intent.request)
	if err != nil {
		return AuthSessionStartResult{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/auth-sessions", bytes.NewReader(body))
	if err != nil {
		return AuthSessionStartResult{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", key)
	request.GetBody = nil
	response, err := c.http.Do(request)
	uncertain := errors.New("authentication start not acknowledged; retain original intent and reconcile without a new provider flow")
	if err != nil {
		return AuthSessionStartResult{}, uncertain
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		return AuthSessionStartResult{}, errorFromResponse(response, "authentication start requires reconciliation")
	}
	var result AuthSessionStartResult
	// Wire result names are explicit rather than inferred from Go field spelling.
	var wire struct {
		ProviderSpec string `json:"provider_spec"`
		SessionID    string `json:"session_id"`
		AuthorizeURL string `json:"authorize_url"`
		UserCode     string `json:"user_code"`
		ExpiresAt    string `json:"expires_at"`
		State        string `json:"state"`
	}
	decoder := json.NewDecoder(response.Body)
	if decoder.Decode(&wire) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return result, uncertain
	}
	expires, expiryErr := time.Parse(time.RFC3339, wire.ExpiresAt)
	authorize, authorizeErr := url.Parse(wire.AuthorizeURL)
	if wire.ProviderSpec != in.ProviderSpec || !validAuthSessionIdentifier(wire.SessionID) || wire.State != "pending" || expiryErr != nil || expires.IsZero() || authorizeErr != nil || authorize.Scheme != "https" || authorize.Host == "" || authorize.User != nil || in.AuthMode == "device" && wire.UserCode == "" || response.Header.Get("Location") != "/api/v1/auth-sessions/"+wire.SessionID {
		return result, uncertain
	}
	return AuthSessionStartResult{ProviderSpec: wire.ProviderSpec, SessionID: wire.SessionID, AuthorizeURL: wire.AuthorizeURL, UserCode: wire.UserCode, ExpiresAt: wire.ExpiresAt, State: wire.State}, nil
}

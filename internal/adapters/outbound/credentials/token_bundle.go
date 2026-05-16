package credentials

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// TokenBundle stores refresh-capable auth material for provider-backed secret
// refs. Resolvers keep backward compatibility with legacy raw-token values.
type TokenBundle struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	IDToken      string    `json:"id_token,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
	IssuedAt     time.Time `json:"issued_at,omitempty"`
}

func EncodeTokenBundle(bundle TokenBundle) (string, error) {
	bundle.AccessToken = strings.TrimSpace(bundle.AccessToken)   // trimlowerlint:allow boundary canonicalization
	bundle.RefreshToken = strings.TrimSpace(bundle.RefreshToken) // trimlowerlint:allow boundary canonicalization
	bundle.IDToken = strings.TrimSpace(bundle.IDToken)           // trimlowerlint:allow boundary canonicalization
	if bundle.AccessToken == "" {
		return "", fmt.Errorf("token bundle access token is required")
	}
	raw, err := json.Marshal(bundle)
	if err != nil {
		return "", fmt.Errorf("token bundle encode failed")
	}
	return string(raw), nil
}

func DecodeTokenBundle(raw string) (TokenBundle, bool, error) {
	trimmed := strings.TrimSpace(raw) // trimlowerlint:allow boundary canonicalization
	if trimmed == "" {
		return TokenBundle{}, false, fmt.Errorf("token payload is empty")
	}
	if !strings.HasPrefix(trimmed, "{") {
		return TokenBundle{}, false, fmt.Errorf("token payload must be a JSON object bundle")
	}
	var bundle TokenBundle
	if err := json.Unmarshal([]byte(trimmed), &bundle); err != nil {
		return TokenBundle{}, false, fmt.Errorf("token bundle decode failed")
	}
	bundle.AccessToken = strings.TrimSpace(bundle.AccessToken)   // trimlowerlint:allow boundary canonicalization
	bundle.RefreshToken = strings.TrimSpace(bundle.RefreshToken) // trimlowerlint:allow boundary canonicalization
	bundle.IDToken = strings.TrimSpace(bundle.IDToken)           // trimlowerlint:allow boundary canonicalization
	if bundle.AccessToken == "" {
		return TokenBundle{}, false, fmt.Errorf("token bundle access token is required")
	}
	return bundle, true, nil
}

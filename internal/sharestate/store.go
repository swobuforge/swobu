package sharestate

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/swobuforge/swobu/internal/platform/atomicfile"
	"github.com/swobuforge/swobu/internal/routing"
	"github.com/swobuforge/swobu/shareprotocol"
)

const SchemaVersion = 2

var ErrUnauthorized = errors.New("share bearer is invalid or expired")

type Expiry string

const (
	ExpiryOneDay     Expiry = "1d"
	ExpirySevenDays  Expiry = "7d"
	ExpiryThirtyDays Expiry = "30d"
	ExpiryNever      Expiry = "never"
)

type Grant struct {
	Workspace routing.WorkspaceSlug
	Route     routing.RouteName
	Bearer    string
	ExpiresAt time.Time
}

type TLSCredential struct {
	PrivateKey       *ecdsa.PrivateKey
	CertificateChain []byte
}

type Endpoint struct {
	IdentityPrivateKey       *ecdsa.PrivateKey
	TLSCredential            *TLSCredential
	CertificateFailureStreak uint32
	NextCertificateAttempt   time.Time
}

type State struct {
	Endpoint Endpoint
	Grants   []Grant
}

type diskState struct {
	SchemaVersion            int         `json:"schema_version"`
	IdentityPrivateKeyPEM    string      `json:"identity_private_key_pem"`
	TLSPrivateKeyPEM         string      `json:"tls_private_key_pem,omitempty"`
	CertificateChain         string      `json:"certificate_chain_pem,omitempty"`
	CertificateFailureStreak uint32      `json:"certificate_failure_streak,omitempty"`
	NextCertificateAttempt   time.Time   `json:"next_certificate_attempt_at,omitempty"`
	Grants                   []diskGrant `json:"grants,omitempty"`
}

type diskGrant struct {
	Workspace string    `json:"workspace"`
	Route     string    `json:"route"`
	Bearer    string    `json:"bearer"`
	ExpiresAt time.Time `json:"expires_at,omitempty"`
}

type Store struct {
	path  string
	mu    sync.Mutex
	state State
	now   func() time.Time
}

func Open(path string) (*Store, error) {
	store := &Store{path: path, now: time.Now}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read share state: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return nil, fmt.Errorf("secure share state: %w", err)
	}
	state, err := decode(raw)
	if err != nil {
		return nil, err
	}
	store.state = state
	return store, nil
}

func (s *Store) EnsureEndpoint() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Endpoint.IdentityPrivateKey != nil {
		return nil
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generate endpoint key: %w", err)
	}
	s.state.Endpoint.IdentityPrivateKey = key
	if err := s.persist(); err != nil {
		s.state.Endpoint.IdentityPrivateKey = nil
		return err
	}
	return nil
}

func (s *Store) Snapshot() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneState(s.state)
}

func (s *Store) ActiveGrants() []Grant {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	grants := make([]Grant, 0, len(s.state.Grants))
	for _, grant := range s.state.Grants {
		if grant.ExpiresAt.IsZero() || now.Before(grant.ExpiresAt) {
			grants = append(grants, grant)
		}
	}
	return grants
}

func (s *Store) EndpointID() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return endpointID(s.state.Endpoint.IdentityPrivateKey)
}

func (s *Store) Issue(workspace routing.WorkspaceSlug, route routing.RouteName, expiry Expiry) (Grant, error) {
	duration, never, err := expiry.duration()
	if err != nil {
		return Grant{}, err
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return Grant{}, fmt.Errorf("generate share bearer: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	grant := Grant{Workspace: workspace, Route: route, Bearer: "swsh_" + base64.RawURLEncoding.EncodeToString(secret)}
	if !never {
		grant.ExpiresAt = s.now().UTC().Add(duration)
	}
	updated := make([]Grant, 0, len(s.state.Grants)+1)
	for _, existing := range s.state.Grants {
		if existing.Workspace != workspace || existing.Route != route {
			updated = append(updated, existing)
		}
	}
	updated = append(updated, grant)
	previous := s.state.Grants
	s.state.Grants = updated
	if err := s.persist(); err != nil {
		s.state.Grants = previous
		return Grant{}, err
	}
	return grant, nil
}

func (s *Store) Revoke(workspace routing.WorkspaceSlug, route routing.RouteName) error {
	_, err := s.RevokeBindings(workspace, &route)
	return err
}

// RevokeBindings atomically removes every Grant for a workspace, or one route
// when route is non-nil. It includes expired Grants so old bearers cannot revive.
func (s *Store) RevokeBindings(workspace routing.WorkspaceSlug, route *routing.RouteName) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	updated := make([]Grant, 0, len(s.state.Grants))
	removed := false
	for _, grant := range s.state.Grants {
		match := grant.Workspace == workspace && (route == nil || grant.Route == *route)
		if match {
			removed = true
		} else {
			updated = append(updated, grant)
		}
	}
	if !removed {
		return false, nil
	}
	previous := s.state.Grants
	s.state.Grants = updated
	if err := s.persist(); err != nil {
		s.state.Grants = previous
		return false, err
	}
	return true, nil
}

func (s *Store) Authenticate(bearer string) (Grant, error) {
	candidate := []byte(strings.TrimSpace(bearer))
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for _, grant := range s.state.Grants {
		if len(candidate) == len(grant.Bearer) && subtle.ConstantTimeCompare(candidate, []byte(grant.Bearer)) == 1 {
			if !grant.ExpiresAt.IsZero() && !now.Before(grant.ExpiresAt) {
				return Grant{}, ErrUnauthorized
			}
			return grant, nil
		}
	}
	return Grant{}, ErrUnauthorized
}

func endpointID(key *ecdsa.PrivateKey) (string, error) {
	if key == nil {
		return "", errors.New("Endpoint identity key is absent")
	}
	return shareprotocol.EndpointID(&key.PublicKey)
}

func Hostname(endpointID string) string { return shareprotocol.Hostname(endpointID) }

func (e Expiry) duration() (time.Duration, bool, error) {
	switch e {
	case "", ExpirySevenDays:
		return 7 * 24 * time.Hour, false, nil
	case ExpiryOneDay:
		return 24 * time.Hour, false, nil
	case ExpiryThirtyDays:
		return 30 * 24 * time.Hour, false, nil
	case ExpiryNever:
		return 0, true, nil
	default:
		return 0, false, fmt.Errorf("unsupported share expiry %q", e)
	}
}

func (s *Store) persist() error {
	raw, err := encode(s.state)
	if err != nil {
		return err
	}
	if err := atomicfile.Replace(s.path, raw, 0o600); err != nil {
		return fmt.Errorf("persist share state: %w", err)
	}
	return nil
}

func encode(state State) ([]byte, error) {
	identityDER, err := x509.MarshalPKCS8PrivateKey(state.Endpoint.IdentityPrivateKey)
	if err != nil {
		return nil, fmt.Errorf("marshal Endpoint identity key: %w", err)
	}
	disk := diskState{SchemaVersion: SchemaVersion, IdentityPrivateKeyPEM: string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: identityDER})), CertificateFailureStreak: state.Endpoint.CertificateFailureStreak, NextCertificateAttempt: state.Endpoint.NextCertificateAttempt}
	if credential := state.Endpoint.TLSCredential; credential != nil {
		tlsDER, err := x509.MarshalPKCS8PrivateKey(credential.PrivateKey)
		if err != nil {
			return nil, fmt.Errorf("marshal Endpoint TLS key: %w", err)
		}
		disk.TLSPrivateKeyPEM = string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: tlsDER}))
		disk.CertificateChain = string(credential.CertificateChain)
	}
	for _, grant := range state.Grants {
		disk.Grants = append(disk.Grants, diskGrant{Workspace: grant.Workspace.String(), Route: grant.Route.String(), Bearer: grant.Bearer, ExpiresAt: grant.ExpiresAt})
	}
	raw, err := json.MarshalIndent(disk, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode share state: %w", err)
	}
	return append(raw, '\n'), nil
}

func decodePrivateKey(raw, noun string) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(raw))
	if block == nil {
		return nil, fmt.Errorf("decode %s: PEM is invalid", noun)
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", noun, err)
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok || key.Curve != elliptic.P256() {
		return nil, fmt.Errorf("%s must be ECDSA P-256", noun)
	}
	return key, nil
}

func decode(raw []byte) (State, error) {
	var disk diskState
	if err := json.Unmarshal(raw, &disk); err != nil {
		return State{}, fmt.Errorf("decode share state: %w", err)
	}
	if disk.SchemaVersion != SchemaVersion {
		return State{}, fmt.Errorf("unsupported share state schema version %d", disk.SchemaVersion)
	}
	identity, err := decodePrivateKey(disk.IdentityPrivateKeyPEM, "Endpoint identity key")
	if err != nil {
		return State{}, err
	}
	state := State{Endpoint: Endpoint{IdentityPrivateKey: identity, CertificateFailureStreak: disk.CertificateFailureStreak, NextCertificateAttempt: disk.NextCertificateAttempt}}
	if (disk.TLSPrivateKeyPEM == "") != (disk.CertificateChain == "") {
		return State{}, errors.New("Endpoint TLS credential must contain both private key and certificate chain")
	}
	if disk.TLSPrivateKeyPEM != "" {
		key, err := decodePrivateKey(disk.TLSPrivateKeyPEM, "Endpoint TLS key")
		if err != nil {
			return State{}, err
		}
		keyDER, _ := x509.MarshalPKCS8PrivateKey(key)
		keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
		if _, err := tls.X509KeyPair([]byte(disk.CertificateChain), keyPEM); err != nil {
			return State{}, fmt.Errorf("decode Endpoint TLS credential: %w", err)
		}
		state.Endpoint.TLSCredential = &TLSCredential{PrivateKey: key, CertificateChain: []byte(disk.CertificateChain)}
	}
	for _, persisted := range disk.Grants {
		workspace, err := routing.ParseWorkspaceSlug(persisted.Workspace)
		if err != nil {
			return State{}, fmt.Errorf("decode grant workspace: %w", err)
		}
		route, err := routing.ParseRouteName(persisted.Route)
		if err != nil {
			return State{}, fmt.Errorf("decode grant route: %w", err)
		}
		if !strings.HasPrefix(persisted.Bearer, "swsh_") {
			return State{}, errors.New("decode grant bearer: invalid format")
		}
		state.Grants = append(state.Grants, Grant{Workspace: workspace, Route: route, Bearer: persisted.Bearer, ExpiresAt: persisted.ExpiresAt})
	}
	return state, nil
}

func cloneState(state State) State {
	cloned := State{Endpoint: Endpoint{IdentityPrivateKey: state.Endpoint.IdentityPrivateKey, CertificateFailureStreak: state.Endpoint.CertificateFailureStreak, NextCertificateAttempt: state.Endpoint.NextCertificateAttempt}}
	if state.Endpoint.TLSCredential != nil {
		cloned.Endpoint.TLSCredential = &TLSCredential{PrivateKey: state.Endpoint.TLSCredential.PrivateKey, CertificateChain: append([]byte(nil), state.Endpoint.TLSCredential.CertificateChain...)}
	}
	cloned.Grants = append([]Grant(nil), state.Grants...)
	return cloned
}

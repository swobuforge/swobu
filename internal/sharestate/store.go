package sharestate

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/subtle"
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

const SchemaVersion = 1

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

type Endpoint struct {
	PrivateKey       *ecdsa.PrivateKey
	CertificateChain []byte
}

type State struct {
	Endpoint Endpoint
	Grants   []Grant
}

type diskState struct {
	SchemaVersion    int         `json:"schema_version"`
	EndpointKeyPEM   string      `json:"endpoint_private_key_pem"`
	CertificateChain string      `json:"certificate_chain_pem,omitempty"`
	Grants           []diskGrant `json:"grants,omitempty"`
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
	if s.state.Endpoint.PrivateKey != nil {
		return nil
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generate endpoint key: %w", err)
	}
	s.state.Endpoint.PrivateKey = key
	if err := s.persist(); err != nil {
		s.state.Endpoint.PrivateKey = nil
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
	return endpointID(s.state.Endpoint.PrivateKey)
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
	s.mu.Lock()
	defer s.mu.Unlock()
	updated := make([]Grant, 0, len(s.state.Grants))
	removed := false
	for _, grant := range s.state.Grants {
		if grant.Workspace != workspace || grant.Route != route {
			updated = append(updated, grant)
		} else {
			removed = true
		}
	}
	if !removed {
		return nil
	}
	previous := s.state.Grants
	s.state.Grants = updated
	if err := s.persist(); err != nil {
		s.state.Grants = previous
		return err
	}
	return nil
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

func EndpointID(publicKey any) (string, error) {
	return shareprotocol.EndpointID(publicKey)
}

func endpointID(key *ecdsa.PrivateKey) (string, error) {
	if key == nil {
		return "", errors.New("endpoint key is absent")
	}
	return EndpointID(&key.PublicKey)
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
	keyDER, err := x509.MarshalPKCS8PrivateKey(state.Endpoint.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("marshal endpoint key: %w", err)
	}
	disk := diskState{SchemaVersion: SchemaVersion, EndpointKeyPEM: string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})), CertificateChain: string(state.Endpoint.CertificateChain)}
	for _, grant := range state.Grants {
		disk.Grants = append(disk.Grants, diskGrant{Workspace: grant.Workspace.String(), Route: grant.Route.String(), Bearer: grant.Bearer, ExpiresAt: grant.ExpiresAt})
	}
	raw, err := json.MarshalIndent(disk, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode share state: %w", err)
	}
	return append(raw, '\n'), nil
}

func decode(raw []byte) (State, error) {
	var disk diskState
	if err := json.Unmarshal(raw, &disk); err != nil {
		return State{}, fmt.Errorf("decode share state: %w", err)
	}
	if disk.SchemaVersion != SchemaVersion {
		return State{}, fmt.Errorf("unsupported share state schema version %d", disk.SchemaVersion)
	}
	block, _ := pem.Decode([]byte(disk.EndpointKeyPEM))
	if block == nil {
		return State{}, errors.New("decode endpoint key: PEM is invalid")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return State{}, fmt.Errorf("parse endpoint key: %w", err)
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok || key.Curve != elliptic.P256() {
		return State{}, errors.New("endpoint key must be ECDSA P-256")
	}
	state := State{Endpoint: Endpoint{PrivateKey: key, CertificateChain: []byte(disk.CertificateChain)}}
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
	cloned := State{Endpoint: Endpoint{PrivateKey: state.Endpoint.PrivateKey, CertificateChain: append([]byte(nil), state.Endpoint.CertificateChain...)}}
	cloned.Grants = append([]Grant(nil), state.Grants...)
	return cloned
}

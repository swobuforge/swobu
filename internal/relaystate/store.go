// Package relaystate owns the relay's local transport identity and TLS
// certificate. Workspace publication and client authorization live elsewhere.
package relaystate

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/swobuforge/swobu/internal/platform/atomicfile"
	"github.com/swobuforge/swobu/relayprotocol"
)

const SchemaVersion = 1

// TLSCredential holds Node-owned application TLS material; never log its fields.
type TLSCredential struct {
	PrivateKey       *ecdsa.PrivateKey
	CertificateChain []byte
}

// Endpoint is cryptographic transport identity and certificate retry state.
// It carries no Workspace publication or user authorization.
type Endpoint struct {
	IdentityPrivateKey       *ecdsa.PrivateKey
	TLSCredential            *TLSCredential
	CertificateFailureStreak uint32
	NextCertificateAttempt   time.Time
}

// State is a private transport snapshot, independent of Node desired state.
type State struct{ Endpoint Endpoint }
type diskState struct {
	SchemaVersion            int       `json:"schema_version"`
	IdentityPrivateKeyPEM    string    `json:"identity_private_key_pem"`
	TLSPrivateKeyPEM         string    `json:"tls_private_key_pem,omitempty"`
	CertificateChain         string    `json:"certificate_chain_pem,omitempty"`
	CertificateFailureStreak uint32    `json:"certificate_failure_streak,omitempty"`
	NextCertificateAttempt   time.Time `json:"next_certificate_attempt_at,omitempty"`
}

// Store atomically persists the private relay.json journal.
type Store struct {
	path    string
	mu      sync.Mutex
	state   State
	replace func(string, []byte, os.FileMode) error
}

// Open loads the transport state without rewriting an operator-owned file.
// An absent journal stays absent until identity is first required.
func Open(path string) (*Store, error) {
	store := &Store{path: path, replace: atomicfile.Replace}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read relay state: %w", err)
	}
	store.state, err = decode(raw)
	if err != nil {
		return nil, err
	}
	return store, nil
}

// EnsureEndpoint durably creates transport identity once; a failed write leaves
// the in-memory identity absent so it cannot disagree with the journal.
func (s *Store) EnsureEndpoint() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Endpoint.IdentityPrivateKey != nil {
		return nil
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generate relay identity: %w", err)
	}
	s.state.Endpoint.IdentityPrivateKey = key
	if err = s.persist(); err != nil {
		s.state.Endpoint.IdentityPrivateKey = nil
	}
	return err
}

// Snapshot returns an independent copy of private transport state. Never log it.
func (s *Store) Snapshot() State { s.mu.Lock(); defer s.mu.Unlock(); return cloneState(s.state) }
func (s *Store) EndpointID() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return endpointID(s.state.Endpoint.IdentityPrivateKey)
}
func Hostname(id string) string { return relayprotocol.Hostname(id) }
func endpointID(key *ecdsa.PrivateKey) (string, error) {
	if key == nil {
		return "", errors.New("relay identity not initialized")
	}
	return relayprotocol.EndpointID(&key.PublicKey)
}
func (s *Store) persist() error {
	raw, err := encode(s.state)
	if err != nil {
		return err
	}
	if err = s.replace(s.path, raw, 0600); err != nil {
		return fmt.Errorf("persist relay state: %w", err)
	}
	return nil
}
func encode(state State) ([]byte, error) {
	if state.Endpoint.IdentityPrivateKey == nil {
		return nil, errors.New("relay identity missing")
	}
	identityDER, err := x509.MarshalPKCS8PrivateKey(state.Endpoint.IdentityPrivateKey)
	if err != nil {
		return nil, err
	}
	disk := diskState{SchemaVersion: SchemaVersion, IdentityPrivateKeyPEM: string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: identityDER})), CertificateFailureStreak: state.Endpoint.CertificateFailureStreak, NextCertificateAttempt: state.Endpoint.NextCertificateAttempt}
	if tlsCred := state.Endpoint.TLSCredential; tlsCred != nil {
		der, err := x509.MarshalPKCS8PrivateKey(tlsCred.PrivateKey)
		if err != nil {
			return nil, err
		}
		disk.TLSPrivateKeyPEM = string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
		disk.CertificateChain = string(tlsCred.CertificateChain)
	}
	raw, err := json.MarshalIndent(disk, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}
func decodePrivateKey(raw, noun string) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(raw))
	if block == nil {
		return nil, fmt.Errorf("%s has invalid PEM", noun)
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", noun, err)
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok || key.Curve != elliptic.P256() {
		return nil, fmt.Errorf("%s requires ECDSA P-256", noun)
	}
	return key, nil
}
func decode(raw []byte) (State, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var disk diskState
	if err := dec.Decode(&disk); err != nil {
		return State{}, fmt.Errorf("decode relay state: %w", err)
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		return State{}, errors.New("relay state must be one document")
	}
	if disk.SchemaVersion != SchemaVersion {
		return State{}, fmt.Errorf("unsupported relay schema %d", disk.SchemaVersion)
	}
	identity, err := decodePrivateKey(disk.IdentityPrivateKeyPEM, "relay identity")
	if err != nil {
		return State{}, err
	}
	state := State{Endpoint: Endpoint{IdentityPrivateKey: identity, CertificateFailureStreak: disk.CertificateFailureStreak, NextCertificateAttempt: disk.NextCertificateAttempt}}
	if (disk.TLSPrivateKeyPEM == "") != (disk.CertificateChain == "") {
		return State{}, errors.New("relay TLS key and certificate must coexist")
	}
	if disk.TLSPrivateKeyPEM != "" {
		key, err := decodePrivateKey(disk.TLSPrivateKeyPEM, "relay TLS key")
		if err != nil {
			return State{}, err
		}
		pemKey := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: mustMarshalPrivate(key)})
		if _, err = tls.X509KeyPair([]byte(disk.CertificateChain), pemKey); err != nil {
			return State{}, fmt.Errorf("decode relay certificate: %w", err)
		}
		state.Endpoint.TLSCredential = &TLSCredential{PrivateKey: key, CertificateChain: []byte(disk.CertificateChain)}
	}
	return state, nil
}
func mustMarshalPrivate(key *ecdsa.PrivateKey) []byte {
	der, _ := x509.MarshalPKCS8PrivateKey(key)
	return der
}
func cloneState(state State) State {
	clone := State{Endpoint: state.Endpoint}
	if c := state.Endpoint.TLSCredential; c != nil {
		copyCred := *c
		copyCred.CertificateChain = bytes.Clone(c.CertificateChain)
		clone.Endpoint.TLSCredential = &copyCred
	}
	return clone
}

package accessstate

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/platform/atomicfile"
	"github.com/swobuforge/swobu/internal/routing"
)

const SchemaVersion = 1

var (
	ErrNotFound     = errors.New("access identity or credential not found")
	ErrUnauthorized = errors.New("access credential is invalid or revoked")
	ErrRevoked      = errors.New("access identity is revoked")
)

type CredentialID struct{ value string }

func (id CredentialID) String() string { return id.value }

func ParseCredentialID(raw string) (CredentialID, error) { return parseCredentialID(raw) }

func parseCredentialID(raw string) (CredentialID, error) {
	if !strings.HasPrefix(raw, "acred_") {
		return CredentialID{}, errors.New("credential ID must use acred_ format")
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(raw, "acred_"))
	if err != nil || len(payload) != 16 {
		return CredentialID{}, errors.New("credential ID must contain 16 base64url-encoded random bytes")
	}
	return CredentialID{value: raw}, nil
}

type Credential struct {
	ID        CredentialID
	Digest    [32]byte
	Workspace routing.WorkspaceSlug
	CreatedAt time.Time
}

type Access struct {
	ID          accessidentity.ID
	Name        string
	CreatedAt   time.Time
	RevokedAt   *time.Time
	Credentials []Credential
}

type IssuedCredential struct {
	Credential Credential
	Token      string
}

type Resolution struct {
	AccessID     accessidentity.ID
	CredentialID CredentialID
	Workspace    routing.WorkspaceSlug
}

type Store struct {
	path         string
	mu           sync.RWMutex
	state        []Access
	byAccess     map[accessidentity.ID]int
	byCredential map[CredentialID]Resolution
	byDigest     map[[32]byte]Resolution
	now          func() time.Time
	random       io.Reader
}

func Open(path string) (*Store, error) {
	store := &Store{path: path, now: time.Now, random: rand.Reader}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		store.reindex()
		return store, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read access state: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return nil, fmt.Errorf("secure access state: %w", err)
	}
	state, err := decode(raw)
	if err != nil {
		return nil, err
	}
	store.state = state
	store.reindex()
	return store, nil
}

func (s *Store) List() []Access {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneState(s.state)
}

func (s *Store) Create(name string, workspace routing.WorkspaceSlug) (Access, IssuedCredential, error) {
	name, err := validateName(name)
	if err != nil {
		return Access{}, IssuedCredential{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	id, err := s.newAccessID()
	if err != nil {
		return Access{}, IssuedCredential{}, err
	}
	credential, err := s.newCredential(workspace)
	if err != nil {
		return Access{}, IssuedCredential{}, err
	}
	access := Access{ID: id, Name: name, CreatedAt: s.now().UTC(), Credentials: []Credential{credential.Credential}}
	candidate := append(cloneState(s.state), access)
	if err := s.commit(candidate); err != nil {
		return Access{}, IssuedCredential{}, err
	}
	return cloneAccess(access), credential, nil
}

func (s *Store) IssueCredential(id accessidentity.ID, workspace routing.WorkspaceSlug) (IssuedCredential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	index, ok := s.byAccess[id]
	if !ok {
		return IssuedCredential{}, ErrNotFound
	}
	if s.state[index].RevokedAt != nil {
		return IssuedCredential{}, ErrRevoked
	}
	issued, err := s.newCredential(workspace)
	if err != nil {
		return IssuedCredential{}, err
	}
	candidate := cloneState(s.state)
	candidate[index].Credentials = append(candidate[index].Credentials, issued.Credential)
	if err := s.commit(candidate); err != nil {
		return IssuedCredential{}, err
	}
	return issued, nil
}

func (s *Store) Authenticate(token string) (Resolution, error) {
	if _, err := parseToken(token); err != nil {
		return Resolution{}, ErrUnauthorized
	}
	digest := sha256.Sum256([]byte(token))
	s.mu.RLock()
	defer s.mu.RUnlock()
	resolution, ok := s.byDigest[digest]
	if !ok {
		return Resolution{}, ErrUnauthorized
	}
	return resolution, nil
}

func (s *Store) ResolveCredential(id CredentialID) (Resolution, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	resolution, ok := s.byCredential[id]
	if !ok {
		return Resolution{}, ErrUnauthorized
	}
	return resolution, nil
}

func (s *Store) RevokeCredential(accessID accessidentity.ID, credentialID CredentialID) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	index, ok := s.byAccess[accessID]
	if !ok {
		return false, ErrNotFound
	}
	candidate := cloneState(s.state)
	credentials := candidate[index].Credentials[:0]
	removed := false
	for _, credential := range candidate[index].Credentials {
		if credential.ID == credentialID {
			removed = true
			continue
		}
		credentials = append(credentials, credential)
	}
	if !removed {
		return false, ErrNotFound
	}
	candidate[index].Credentials = credentials
	return true, s.commit(candidate)
}

func (s *Store) RevokeAccess(id accessidentity.ID) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	index, ok := s.byAccess[id]
	if !ok {
		return false, ErrNotFound
	}
	if s.state[index].RevokedAt != nil {
		return false, nil
	}
	candidate := cloneState(s.state)
	now := s.now().UTC()
	candidate[index].RevokedAt = &now
	candidate[index].Credentials = nil
	return true, s.commit(candidate)
}

// RevokeWorkspace removes every credential rooted in a destroyed Workspace.
// The Access identity remains so historical attribution stays resolvable.
func (s *Store) RevokeWorkspace(workspace routing.WorkspaceSlug) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	candidate := cloneState(s.state)
	removed := false
	for index := range candidate {
		credentials := candidate[index].Credentials[:0]
		for _, credential := range candidate[index].Credentials {
			if credential.Workspace == workspace {
				removed = true
				continue
			}
			credentials = append(credentials, credential)
		}
		candidate[index].Credentials = credentials
	}
	if !removed {
		return false, nil
	}
	return true, s.commit(candidate)
}

func (s *Store) ReconcileWorkspaces(config routing.Config) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	candidate := cloneState(s.state)
	removed := false
	for index := range candidate {
		credentials := candidate[index].Credentials[:0]
		for _, credential := range candidate[index].Credentials {
			if _, ok := config.Workspace(credential.Workspace); !ok {
				removed = true
				continue
			}
			credentials = append(credentials, credential)
		}
		candidate[index].Credentials = credentials
	}
	if !removed {
		return false, nil
	}
	return true, s.commit(candidate)
}

func (s *Store) newAccessID() (accessidentity.ID, error) {
	for {
		var random [16]byte
		if _, err := io.ReadFull(s.random, random[:]); err != nil {
			return accessidentity.ID{}, fmt.Errorf("generate access ID: %w", err)
		}
		id := accessidentity.NewManaged(random)
		if _, exists := s.byAccess[id]; !exists {
			return id, nil
		}
	}
}

func (s *Store) newCredential(workspace routing.WorkspaceSlug) (IssuedCredential, error) {
	var idRandom [16]byte
	if _, err := io.ReadFull(s.random, idRandom[:]); err != nil {
		return IssuedCredential{}, fmt.Errorf("generate credential ID: %w", err)
	}
	id := CredentialID{value: "acred_" + base64.RawURLEncoding.EncodeToString(idRandom[:])}
	if _, exists := s.byCredential[id]; exists {
		return IssuedCredential{}, errors.New("generated duplicate credential ID")
	}
	secret := make([]byte, 32)
	if _, err := io.ReadFull(s.random, secret); err != nil {
		return IssuedCredential{}, fmt.Errorf("generate access credential: %w", err)
	}
	token := "swak_" + base64.RawURLEncoding.EncodeToString(secret)
	return IssuedCredential{Credential: Credential{ID: id, Digest: sha256.Sum256([]byte(token)), Workspace: workspace, CreatedAt: s.now().UTC()}, Token: token}, nil
}

func parseToken(token string) ([]byte, error) {
	if !strings.HasPrefix(token, "swak_") {
		return nil, ErrUnauthorized
	}
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(token, "swak_"))
	if err != nil || len(decoded) != 32 {
		return nil, ErrUnauthorized
	}
	return decoded, nil
}

func validateName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" || len(name) > 128 || !utf8.ValidString(name) {
		return "", errors.New("access name must be valid UTF-8 between 1 and 128 bytes")
	}
	for _, char := range name {
		if unicode.IsControl(char) {
			return "", errors.New("access name must not contain control characters")
		}
	}
	return name, nil
}

func (s *Store) commit(candidate []Access) error {
	raw, err := encode(candidate)
	if err != nil {
		return err
	}
	if err := atomicfile.Replace(s.path, raw, 0o600); err != nil {
		return fmt.Errorf("persist access state: %w", err)
	}
	s.state = candidate
	s.reindex()
	return nil
}

func (s *Store) reindex() {
	s.byAccess = make(map[accessidentity.ID]int, len(s.state))
	s.byCredential = make(map[CredentialID]Resolution)
	s.byDigest = make(map[[32]byte]Resolution)
	for accessIndex, access := range s.state {
		s.byAccess[access.ID] = accessIndex
		if access.RevokedAt != nil {
			continue
		}
		for _, credential := range access.Credentials {
			resolution := Resolution{AccessID: access.ID, CredentialID: credential.ID, Workspace: credential.Workspace}
			s.byCredential[credential.ID] = resolution
			s.byDigest[credential.Digest] = resolution
		}
	}
}

type diskState struct {
	SchemaVersion int          `json:"schema_version"`
	Access        []diskAccess `json:"access"`
}
type diskAccess struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	CreatedAt   time.Time        `json:"created_at"`
	RevokedAt   *time.Time       `json:"revoked_at,omitempty"`
	Credentials []diskCredential `json:"credentials"`
}
type diskCredential struct {
	ID        string    `json:"id"`
	Digest    string    `json:"digest"`
	Workspace string    `json:"workspace"`
	CreatedAt time.Time `json:"created_at"`
}

func encode(state []Access) ([]byte, error) {
	disk := diskState{SchemaVersion: SchemaVersion, Access: make([]diskAccess, 0, len(state))}
	for _, access := range state {
		entry := diskAccess{ID: access.ID.String(), Name: access.Name, CreatedAt: access.CreatedAt, RevokedAt: access.RevokedAt, Credentials: make([]diskCredential, 0, len(access.Credentials))}
		for _, credential := range access.Credentials {
			entry.Credentials = append(entry.Credentials, diskCredential{ID: credential.ID.String(), Digest: base64.RawURLEncoding.EncodeToString(credential.Digest[:]), Workspace: credential.Workspace.String(), CreatedAt: credential.CreatedAt})
		}
		disk.Access = append(disk.Access, entry)
	}
	raw, err := json.MarshalIndent(disk, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode access state: %w", err)
	}
	return append(raw, '\n'), nil
}

func decode(raw []byte) ([]Access, error) {
	var disk diskState
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&disk); err != nil {
		return nil, fmt.Errorf("decode access state: %w", err)
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return nil, errors.New("decode access state: trailing JSON data")
	}
	if disk.SchemaVersion != SchemaVersion {
		return nil, fmt.Errorf("unsupported access state schema version %d", disk.SchemaVersion)
	}
	state := make([]Access, 0, len(disk.Access))
	seenAccess := map[accessidentity.ID]struct{}{}
	seenCredential := map[CredentialID]struct{}{}
	seenDigest := map[[32]byte]struct{}{}
	for _, entry := range disk.Access {
		id, err := accessidentity.RestoreManaged(entry.ID)
		if err != nil {
			return nil, fmt.Errorf("decode access ID: %w", err)
		}
		if _, exists := seenAccess[id]; exists {
			return nil, fmt.Errorf("decode access state: duplicate access ID %q", id)
		}
		seenAccess[id] = struct{}{}
		name, err := validateName(entry.Name)
		if err != nil {
			return nil, fmt.Errorf("decode access name: %w", err)
		}
		if entry.CreatedAt.IsZero() {
			return nil, errors.New("decode access state: created_at is required")
		}
		access := Access{ID: id, Name: name, CreatedAt: entry.CreatedAt, RevokedAt: entry.RevokedAt}
		if entry.RevokedAt != nil && len(entry.Credentials) != 0 {
			return nil, errors.New("decode access state: revoked identity has credentials")
		}
		for _, persisted := range entry.Credentials {
			credentialID, err := parseCredentialID(persisted.ID)
			if err != nil {
				return nil, fmt.Errorf("decode credential ID: %w", err)
			}
			if _, exists := seenCredential[credentialID]; exists {
				return nil, fmt.Errorf("decode access state: duplicate credential ID %q", credentialID.String())
			}
			seenCredential[credentialID] = struct{}{}
			digestBytes, err := base64.RawURLEncoding.DecodeString(persisted.Digest)
			if err != nil || len(digestBytes) != 32 {
				return nil, errors.New("decode credential digest: invalid SHA-256 digest")
			}
			var digest [32]byte
			copy(digest[:], digestBytes)
			if _, exists := seenDigest[digest]; exists {
				return nil, errors.New("decode access state: duplicate credential digest")
			}
			seenDigest[digest] = struct{}{}
			workspace, err := routing.ParseWorkspaceSlug(persisted.Workspace)
			if err != nil {
				return nil, fmt.Errorf("decode credential workspace: %w", err)
			}
			if persisted.CreatedAt.IsZero() {
				return nil, errors.New("decode credential: created_at is required")
			}
			access.Credentials = append(access.Credentials, Credential{ID: credentialID, Digest: digest, Workspace: workspace, CreatedAt: persisted.CreatedAt})
		}
		state = append(state, access)
	}
	return state, nil
}

func cloneState(state []Access) []Access {
	out := make([]Access, len(state))
	for i := range state {
		out[i] = cloneAccess(state[i])
	}
	return out
}
func cloneAccess(access Access) Access {
	access.Credentials = append([]Credential(nil), access.Credentials...)
	if access.RevokedAt != nil {
		value := *access.RevokedAt
		access.RevokedAt = &value
	}
	return access
}

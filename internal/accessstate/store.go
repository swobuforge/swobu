package accessstate

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/domain/credentialaudience"
	"github.com/swobuforge/swobu/internal/routing"
)

// The current Principal document has one closed schema. No historical decoding
// or startup migration is supported before the first release.
const SchemaVersion = 3

var (
	ErrNotFound         = errors.New("access identity or credential not found")
	ErrUnauthorized     = errors.New("access credential is invalid or revoked")
	ErrRevoked          = errors.New("access identity is revoked")
	ErrRevisionConflict = errors.New("access state revision conflict")
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
	// Creation receipt is retained with this identity, including after revocation.
	// It records local caller intent, not authority or a recoverable retry key.
	CreationKeyHash    string
	CreationIntentHash string
	CreationAudit      *PrincipalCreationAudit
	LabelAudits        []PrincipalLabelAudit
	// RevocationReceipts retain hashed local request intents for safe recovery.
	// Receipt-only publication must not advance the Principal authority revision.
	RevocationReceipts map[string]string
	RevocationAudits   map[string]PrincipalRevocationAudit
	// CredentialRevocationReceipts survive removal of the selected credential.
	// They are recovery attribution, not active authority or revision truth.
	CredentialRevocationReceipts map[string]string
	CredentialRevocationAudits   map[string]CredentialRevocationAudit
	ID                           accessidentity.ID
	Name                         string
	CreatedAt                    time.Time
	RevokedAt                    *time.Time
	Credentials                  []Credential
	Permissions                  []Permission
	// AuthorityGeneration prevents a later authority lifecycle from reviving
	// consent issued before revocation. Persisted generations are positive.
	AuthorityGeneration uint64
	// PermissionGenerations retain natural-relationship history after removal.
	// Only publication may stamp these counters; detached snapshots are advisory.
	PermissionGenerations map[routing.WorkspaceSlug]uint64
}

// Permission is the natural Workspace-use relationship, independent of issued
// credentials. Zero ExpiresAt means no deadline; the exact deadline denies use.
type Permission struct {
	Workspace routing.WorkspaceSlug
	// Nil is a Workspace-wide grant; a nonempty list is a closed Service set.
	Services []routing.ServiceKind
	// RequestLimits are optional per-Service operation ceilings for this
	// Principal/Workspace relation, shared by all its credentials.
	RequestLimits []RequestRateLimit
	ExpiresAt     time.Time
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

// Audience projects the credential's established Workspace scope, never its
// Principal's other grants. Node credentials have a separate owning lifecycle.
func (r Resolution) Audience() credentialaudience.Audience {
	return credentialaudience.Workspace(r.Workspace.String())
}

// credentialAuthority is a derived immutable view of the latest committed
// grants. Publication installs it before releasing the owner lock. Effect-time
// callbacks may read it while mutating Access itself; it is never persisted or
// used to recover a failed publication.
type credentialAuthority struct {
	state        []Access
	byAccess     map[accessidentity.ID]int
	byCredential map[CredentialID]Resolution
}

type Store struct {
	authority          atomic.Pointer[credentialAuthority]
	mu                 sync.RWMutex
	state              []Access
	permissionReceipts []PermissionReceipt
	credentialReceipts []CredentialReceipt
	byAccess           map[accessidentity.ID]int
	byCredential       map[CredentialID]Resolution
	byDigest           map[[32]byte]Resolution
	now                func() time.Time
	random             io.Reader
	publish            func([]byte) error
	rateBuckets        map[requestRateKey]requestRateBucket
}

// Revision is an opaque digest for optimistic operator mutations.
func (s *Store) Revision() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.revisionLocked()
}

func (s *Store) revisionLocked() string {
	raw, err := encode(s.state)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (s *Store) CheckRevision(expected string) error {
	if expected == "" || expected == s.Revision() {
		return nil
	}
	return ErrRevisionConflict
}

// OpenInline loads the Node's Principal/Workspace-grant authority directly
// from its typed document. The caller owns the sole YAML publication lock.
// There is deliberately no access.json fallback or startup migration.
func OpenInline(raw []byte, publish func([]byte) error) (*Store, error) {
	if publish == nil {
		return nil, errors.New("Principal authority requires Node publisher")
	}
	store := &Store{now: time.Now, random: rand.Reader, publish: publish}
	if len(raw) == 0 {
		store.reindex()
		return store, nil
	}
	var disk diskState
	if err := json.Unmarshal(raw, &disk); err != nil {
		return nil, err
	}
	if disk.SchemaVersion != SchemaVersion {
		return nil, fmt.Errorf("inline principals require schema_version %d", SchemaVersion)
	}
	state, err := decode(raw)
	if err != nil {
		return nil, err
	}
	store.state = state
	store.permissionReceipts = append([]PermissionReceipt(nil), disk.PermissionReceipts...)
	store.credentialReceipts = disk.CredentialReceipts
	store.reindex()
	return store, nil
}

// ValidateDocument checks the Principal domain schema without allowing any
// mutation or creating sidecars; used by offline swobu config validate.
func ValidateDocument(raw []byte) ([]Access, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	return decode(raw)
}

func (s *Store) List() []Access {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneState(s.state)
}

// WithReviewSnapshot holds the authority revision while a caller projects its
// matching detached impact. The callback must not reenter this Store.
func (s *Store) WithReviewSnapshot(project func(string, []Access) error) error {
	if s == nil || project == nil {
		return errors.New("Access review requires a store and projection")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return project(s.revisionLocked(), cloneState(s.state))
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
	access := Access{ID: id, Name: name, CreatedAt: s.now().UTC(), Credentials: []Credential{credential.Credential}, Permissions: []Permission{{Workspace: workspace}}}
	candidate := append(cloneState(s.state), access)
	if err := s.commit(candidate); err != nil {
		return Access{}, IssuedCredential{}, err
	}
	return cloneAccess(s.state[s.byAccess[id]]), credential, nil
}

// IssueCredential issues into an existing, currently active grant.
// Credential issuance never creates or broadens Workspace permission.
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
	if !permissionAllows(s.state[index], workspace, s.now()) {
		return IssuedCredential{}, ErrUnauthorized
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
	if index, exists := s.byAccess[resolution.AccessID]; !exists || !permissionAllows(s.state[index], resolution.Workspace, s.now()) {
		return Resolution{}, ErrUnauthorized
	}
	return resolution, nil
}

// ResolveCredential reads the latest committed credential authority without
// reentering the owner's publication mutex. It grants no transport authority.
func (s *Store) ResolveCredential(id CredentialID) (Resolution, error) {
	snapshot := s.authority.Load()
	if snapshot == nil {
		return Resolution{}, ErrUnauthorized
	}
	resolution, ok := snapshot.byCredential[id]
	if !ok {
		return Resolution{}, ErrUnauthorized
	}
	index, exists := snapshot.byAccess[resolution.AccessID]
	if !exists || !permissionAllows(snapshot.state[index], resolution.Workspace, s.now()) {
		return Resolution{}, ErrUnauthorized
	}
	return resolution, nil
}

// ResolveCredentialForService checks the latest committed grant on every
// effect, including callbacks running inside an Access mutation. A successful
// self-revocation completes once; later calls observe the published revocation.
func (s *Store) ResolveCredentialForService(id CredentialID, service routing.ServiceKind) (Resolution, error) {
	if _, err := routing.ParseServiceKind(string(service)); err != nil {
		return Resolution{}, ErrUnauthorized
	}
	snapshot := s.authority.Load()
	if snapshot == nil {
		return Resolution{}, ErrUnauthorized
	}
	resolution, ok := snapshot.byCredential[id]
	if !ok {
		return Resolution{}, ErrUnauthorized
	}
	index, exists := snapshot.byAccess[resolution.AccessID]
	if !exists || snapshot.state[index].RevokedAt != nil || !permissionAllowsService(snapshot.state[index], resolution.Workspace, service, s.now()) {
		return Resolution{}, ErrUnauthorized
	}
	return resolution, nil
}

// GrantPermission changes Workspace authority without issuing a credential.
func (s *Store) GrantPermission(id accessidentity.ID, workspace routing.WorkspaceSlug) error {
	return s.GrantPermissionAtRevision(id, workspace, "")
}

// GrantPermissionAtRevision admits the reviewed state under the same lock as
// authority publication. Empty revision is reserved for internal lifecycle work;
// the operator transport requires a strong precondition before entering here.
func (s *Store) GrantPermissionAtRevision(id accessidentity.ID, workspace routing.WorkspaceSlug, expected string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if expected != "" && expected != s.revisionLocked() {
		return ErrRevisionConflict
	}
	index, ok := s.byAccess[id]
	if !ok {
		return ErrNotFound
	}
	if s.state[index].RevokedAt != nil {
		return ErrRevoked
	}
	if hasPermission(s.state[index], workspace) {
		return nil
	}
	candidate := cloneState(s.state)
	candidate[index].Permissions = append(candidate[index].Permissions, Permission{Workspace: workspace})
	return s.commit(candidate)
}

// RevokePermission removes Workspace authority but preserves credential
// metadata and plaintext irrecoverability for explicit credential lifecycle.
func (s *Store) RevokePermission(id accessidentity.ID, workspace routing.WorkspaceSlug) error {
	return s.RevokePermissionAtRevision(id, workspace, "")
}

// RevokePermissionAtRevision checks consent before any mutation while holding
// the authority store lock, including no-op and already-absent permissions.
func (s *Store) RevokePermissionAtRevision(id accessidentity.ID, workspace routing.WorkspaceSlug, expected string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if expected != "" && expected != s.revisionLocked() {
		return ErrRevisionConflict
	}
	index, ok := s.byAccess[id]
	if !ok {
		return ErrNotFound
	}
	candidate := cloneState(s.state)
	permissions := candidate[index].Permissions[:0]
	for _, current := range candidate[index].Permissions {
		if current.Workspace != workspace {
			permissions = append(permissions, current)
		}
	}
	candidate[index].Permissions = permissions
	return s.commit(candidate)
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
	candidate[index].Permissions = nil
	return true, s.commit(candidate)
}

// RevokeWorkspace removes every credential and permission rooted in a destroyed Workspace.
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
		permissions := candidate[index].Permissions[:0]
		for _, permission := range candidate[index].Permissions {
			if permission.Workspace == workspace {
				removed = true
				continue
			}
			permissions = append(permissions, permission)
		}
		candidate[index].Permissions = permissions
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
	return s.commitWithPermissionReceipts(candidate, s.permissionReceipts)
}

func (s *Store) commitWithPermissionReceipts(candidate []Access, receipts []PermissionReceipt) error {
	return s.commitWithReceipts(candidate, receipts, s.credentialReceipts)
}

func (s *Store) commitWithReceipts(candidate []Access, receipts []PermissionReceipt, credentialReceipts []CredentialReceipt) error {
	candidate = cloneState(candidate)
	for index := range candidate {
		var previous Access
		if previousIndex, exists := s.byAccess[candidate[index].ID]; exists {
			previous = s.state[previousIndex]
		}
		if err := stampPermissionGenerations(previous, &candidate[index]); err != nil {
			return err
		}
		generation := uint64(0)
		changed := true
		if previousIndex, exists := s.byAccess[candidate[index].ID]; exists {
			previous := s.state[previousIndex]
			generation = previous.AuthorityGeneration
			current := candidate[index]
			current.AuthorityGeneration = generation
			previous.CreationAudit = nil
			current.CreationAudit = nil
			previous.LabelAudits = nil
			current.LabelAudits = nil
			previous.RevocationReceipts = nil
			current.RevocationReceipts = nil
			previous.RevocationAudits = nil
			current.RevocationAudits = nil
			previous.CredentialRevocationReceipts = nil
			current.CredentialRevocationReceipts = nil
			previous.CredentialRevocationAudits = nil
			current.CredentialRevocationAudits = nil
			before, err := encode([]Access{previous})
			if err != nil {
				return err
			}
			after, err := encode([]Access{current})
			if err != nil {
				return err
			}
			changed = !bytes.Equal(before, after)
		}
		if changed {
			if generation == ^uint64(0) {
				return errors.New("access authority generation exhausted")
			}
			generation++
		}
		// Supplied snapshots cannot roll back or manufacture generation. Stamp
		// the candidate only; failed publication retains the original counter.
		candidate[index].AuthorityGeneration = generation
	}
	raw, err := encodeDocument(candidate, receipts, credentialReceipts)
	if err != nil {
		return err
	}
	if err := s.publish(raw); err != nil {
		return fmt.Errorf("publish Principal authority: %w", err)
	}
	s.state = candidate
	s.permissionReceipts = receipts
	s.credentialReceipts = credentialReceipts
	s.reindex()
	s.pruneRequestRateBuckets()
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
	s.authority.Store(&credentialAuthority{state: cloneState(s.state), byAccess: s.byAccess, byCredential: s.byCredential})
}

type diskState struct {
	CredentialReceipts []CredentialReceipt `json:"credential_receipts,omitempty"`
	SchemaVersion      int                 `json:"schema_version"`
	Access             []diskAccess        `json:"access"`
	PermissionReceipts []PermissionReceipt `json:"permission_receipts,omitempty"`
}
type diskAccess struct {
	LabelAudits                  []PrincipalLabelAudit                `json:"label_audits,omitempty"`
	CreationAudit                *PrincipalCreationAudit              `json:"creation_audit,omitempty"`
	CredentialRevocationAudits   map[string]CredentialRevocationAudit `json:"credential_revocation_audits,omitempty"`
	RevocationAudits             map[string]PrincipalRevocationAudit  `json:"revocation_audits,omitempty"`
	CredentialRevocationReceipts map[string]string                    `json:"credential_revocation_receipts,omitempty"`
	RevocationReceipts           map[string]string                    `json:"revocation_receipts,omitempty"`
	CreationKeyHash              string                               `json:"creation_key_hash,omitempty"`
	CreationIntentHash           string                               `json:"creation_intent_hash,omitempty"`
	PermissionGenerations        map[string]uint64                    `json:"permission_generations,omitempty"`
	AuthorityGeneration          uint64                               `json:"authority_generation,omitempty"`
	ID                           string                               `json:"id"`
	Name                         string                               `json:"name"`
	CreatedAt                    time.Time                            `json:"created_at"`
	RevokedAt                    *time.Time                           `json:"revoked_at,omitempty"`
	Credentials                  []diskCredential                     `json:"credentials"`
	Permissions                  []diskPermission                     `json:"permissions"`
}

type diskPermission struct {
	Workspace     string                `json:"workspace"`
	Services      []routing.ServiceKind `json:"services,omitempty"`
	RequestLimits []RequestRateLimit    `json:"request_limits,omitempty"`
	ExpiresAt     *time.Time            `json:"expires_at,omitempty"`
}
type diskCredential struct {
	ID        string    `json:"id"`
	Digest    string    `json:"digest"`
	Workspace string    `json:"workspace"`
	CreatedAt time.Time `json:"created_at"`
}

func encode(state []Access, receipts ...[]PermissionReceipt) ([]byte, error) {
	var permissionReceipts []PermissionReceipt
	if len(receipts) > 0 {
		permissionReceipts = receipts[0]
	}
	return encodeDocument(state, permissionReceipts, nil)
}

func encodeDocument(state []Access, permissions []PermissionReceipt, credentials []CredentialReceipt) ([]byte, error) {
	disk := diskState{SchemaVersion: SchemaVersion, Access: make([]diskAccess, 0, len(state)), PermissionReceipts: permissions, CredentialReceipts: credentials}
	for _, access := range state {
		entry := diskAccess{ID: access.ID.String(), Name: access.Name, CreatedAt: access.CreatedAt, RevokedAt: access.RevokedAt, Credentials: make([]diskCredential, 0, len(access.Credentials)), Permissions: make([]diskPermission, 0, len(access.Permissions))}
		entry.AuthorityGeneration = access.AuthorityGeneration
		entry.CreationKeyHash = access.CreationKeyHash
		entry.CreationIntentHash = access.CreationIntentHash
		entry.CreationAudit = access.CreationAudit
		entry.LabelAudits = access.LabelAudits
		entry.RevocationReceipts = access.RevocationReceipts
		entry.RevocationAudits = access.RevocationAudits
		entry.CredentialRevocationReceipts = access.CredentialRevocationReceipts
		entry.CredentialRevocationAudits = access.CredentialRevocationAudits
		if len(access.PermissionGenerations) != 0 {
			entry.PermissionGenerations = make(map[string]uint64, len(access.PermissionGenerations))
			for workspace, generation := range access.PermissionGenerations {
				entry.PermissionGenerations[workspace.String()] = generation
			}
		}
		for _, permission := range access.Permissions {
			persisted := diskPermission{Workspace: permission.Workspace.String(), Services: permission.Services, RequestLimits: permission.RequestLimits}
			if !permission.ExpiresAt.IsZero() {
				expires := permission.ExpiresAt.UTC()
				persisted.ExpiresAt = &expires
			}
			entry.Permissions = append(entry.Permissions, persisted)
		}
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
	if err := validatePermissionReceipts(disk.PermissionReceipts); err != nil {
		return nil, fmt.Errorf("decode access state: %w", err)
	}
	if err := validateCredentialReceipts(disk.CredentialReceipts); err != nil {
		return nil, fmt.Errorf("decode access state: %w", err)
	}
	state := make([]Access, 0, len(disk.Access))
	seenAccess := map[accessidentity.ID]struct{}{}
	seenCredential := map[CredentialID]struct{}{}
	seenDigest := map[[32]byte]struct{}{}
	seenCreationKeys := map[string]bool{}
	for _, entry := range disk.Access {
		if entry.Permissions == nil {
			return nil, errors.New("decode access state: current permissions array required")
		}
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
		if entry.CreationKeyHash != "" || entry.CreationIntentHash != "" {
			for _, value := range []string{entry.CreationKeyHash, entry.CreationIntentHash} {
				decoded, err := hex.DecodeString(value)
				if err != nil || len(decoded) != sha256.Size || value != strings.ToLower(value) {
					return nil, errors.New("decode access state: invalid Principal creation receipt")
				}
			}
			if seenCreationKeys[entry.CreationKeyHash] {
				return nil, errors.New("decode access state: duplicate Principal creation receipt")
			}
			seenCreationKeys[entry.CreationKeyHash] = true
			access.CreationKeyHash, access.CreationIntentHash = entry.CreationKeyHash, entry.CreationIntentHash
		}
		if entry.AuthorityGeneration == 0 {
			return nil, errors.New("decode access state: Principal authority generation is required")
		}
		access.AuthorityGeneration = entry.AuthorityGeneration
		if err := validatePrincipalCreationAudit(access, entry.CreationAudit); err != nil {
			return nil, err
		}
		access.CreationAudit = entry.CreationAudit
		if err := validatePrincipalLabelAudits(entry.ID, entry.AuthorityGeneration, entry.LabelAudits); err != nil {
			return nil, err
		}
		access.LabelAudits = entry.LabelAudits
		if len(entry.CredentialRevocationReceipts) != 0 {
			access.CredentialRevocationReceipts = make(map[string]string, len(entry.CredentialRevocationReceipts))
			for key, intent := range entry.CredentialRevocationReceipts {
				for _, value := range []string{key, intent} {
					decoded, err := hex.DecodeString(value)
					if err != nil || len(decoded) != sha256.Size || value != strings.ToLower(value) {
						return nil, errors.New("decode access state: invalid credential revocation receipt")
					}
				}
				access.CredentialRevocationReceipts[key] = intent
			}
		}
		if len(entry.RevocationReceipts) != 0 {
			if entry.RevokedAt == nil {
				return nil, errors.New("decode access state: revocation receipt requires revoked identity")
			}
			access.RevocationReceipts = make(map[string]string, len(entry.RevocationReceipts))
			for key, intent := range entry.RevocationReceipts {
				for _, value := range []string{key, intent} {
					decoded, err := hex.DecodeString(value)
					if err != nil || len(decoded) != sha256.Size || value != strings.ToLower(value) {
						return nil, errors.New("decode access state: invalid Principal revocation receipt")
					}
				}
				access.RevocationReceipts[key] = intent
			}
		}
		if err := validatePrincipalRevocationAudits(entry.ID, entry.RevocationReceipts, entry.RevocationAudits); err != nil {
			return nil, err
		}
		access.RevocationAudits = entry.RevocationAudits
		if err := validateCredentialRevocationAudits(entry.ID, entry.CredentialRevocationReceipts, entry.CredentialRevocationAudits); err != nil {
			return nil, err
		}
		access.CredentialRevocationAudits = entry.CredentialRevocationAudits
		if len(entry.PermissionGenerations) != 0 {
			access.PermissionGenerations = make(map[routing.WorkspaceSlug]uint64, len(entry.PermissionGenerations))
			for rawWorkspace, generation := range entry.PermissionGenerations {
				workspace, err := routing.ParseWorkspaceSlug(rawWorkspace)
				if err != nil || generation == 0 {
					return nil, errors.New("decode permission generation: canonical Workspace and positive generation required")
				}
				access.PermissionGenerations[workspace] = generation
			}
		}
		for _, persisted := range entry.Permissions {
			workspace, err := routing.ParseWorkspaceSlug(persisted.Workspace)
			if err != nil {
				return nil, fmt.Errorf("decode permission workspace: %w", err)
			}
			if hasPermission(access, workspace) {
				return nil, errors.New("decode access state: duplicate permission Workspace")
			}
			services, err := canonicalServices(persisted.Services)
			if err != nil {
				return nil, fmt.Errorf("decode permission: %w", err)
			}
			// Persisted restricted grants must remain canonical. Otherwise two
			// equivalent texts can have different reviewer intent fingerprints.
			if services != nil && !slices.Equal(services, persisted.Services) {
				return nil, errors.New("decode permission: Services must be in canonical order")
			}
			limits, err := canonicalRequestRates(persisted.RequestLimits, services)
			if err != nil || (limits != nil && !slices.Equal(limits, persisted.RequestLimits)) {
				return nil, errors.New("decode permission: invalid or noncanonical request limits")
			}
			permission := Permission{Workspace: workspace, Services: services, RequestLimits: limits}
			if persisted.ExpiresAt != nil {
				if persisted.ExpiresAt.IsZero() {
					return nil, errors.New("decode permission: expiry must not be zero")
				}
				permission.ExpiresAt = persisted.ExpiresAt.UTC()
			}
			access.Permissions = append(access.Permissions, permission)
		}
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

func clonePermission(permission Permission) Permission {
	permission.Services = append([]routing.ServiceKind(nil), permission.Services...)
	permission.RequestLimits = append([]RequestRateLimit(nil), permission.RequestLimits...)
	return permission
}

func cloneAccess(access Access) Access {
	access.LabelAudits = append([]PrincipalLabelAudit(nil), access.LabelAudits...)
	if access.CreationAudit != nil {
		audit := *access.CreationAudit
		access.CreationAudit = &audit
	}
	if access.CredentialRevocationAudits != nil {
		audits := make(map[string]CredentialRevocationAudit, len(access.CredentialRevocationAudits))
		for key, audit := range access.CredentialRevocationAudits {
			audits[key] = audit
		}
		access.CredentialRevocationAudits = audits
	}
	if access.RevocationAudits != nil {
		audits := make(map[string]PrincipalRevocationAudit, len(access.RevocationAudits))
		for key, audit := range access.RevocationAudits {
			audit.Workspaces = append([]string{}, audit.Workspaces...)
			audits[key] = audit
		}
		access.RevocationAudits = audits
	}
	if access.CredentialRevocationReceipts != nil {
		receipts := make(map[string]string, len(access.CredentialRevocationReceipts))
		for key, intent := range access.CredentialRevocationReceipts {
			receipts[key] = intent
		}
		access.CredentialRevocationReceipts = receipts
	}
	if access.RevocationReceipts != nil {
		receipts := make(map[string]string, len(access.RevocationReceipts))
		for key, intent := range access.RevocationReceipts {
			receipts[key] = intent
		}
		access.RevocationReceipts = receipts
	}
	if access.PermissionGenerations != nil {
		generations := make(map[routing.WorkspaceSlug]uint64, len(access.PermissionGenerations))
		for workspace, generation := range access.PermissionGenerations {
			generations[workspace] = generation
		}
		access.PermissionGenerations = generations
	}
	access.Credentials = append([]Credential(nil), access.Credentials...)
	access.Permissions = append([]Permission(nil), access.Permissions...)
	for i := range access.Permissions {
		access.Permissions[i] = clonePermission(access.Permissions[i])
	}
	if access.RevokedAt != nil {
		value := *access.RevokedAt
		access.RevokedAt = &value
	}
	return access
}

func hasPermission(access Access, workspace routing.WorkspaceSlug) bool {
	for _, current := range access.Permissions {
		if current.Workspace == workspace {
			return true
		}
	}
	return false
}

// permissionAllows checks time at each authorization boundary, not at startup
// or index construction. Expiry does not erase credentials or mutate revision.
func permissionAllows(access Access, workspace routing.WorkspaceSlug, now time.Time) bool {
	for _, permission := range access.Permissions {
		if permission.Workspace == workspace {
			return permission.ExpiresAt.IsZero() || now.Before(permission.ExpiresAt)
		}
	}
	return false
}

package connections

import (
	"errors"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
)

// Revision is resource-local, strictly positive and monotonic. HTTP exposes its
// decimal string as both body revision and quoted strong ETag.
type Revision struct{ value uint64 }

func ParseRevision(raw string) (Revision, error) {
	value, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || value == 0 || strconv.FormatUint(value, 10) != raw {
		return Revision{}, errors.New("invalid Connection revision")
	}
	return Revision{value: value}, nil
}
func (r Revision) String() string { return strconv.FormatUint(r.value, 10) }

// Connection has no health flag: latest checks are separate revision-bound
// observations. Fields remain private so every published value is validated.
type Connection struct {
	id        connectionidentity.ID
	label     string
	spec      Spec
	revision  Revision
	createdAt time.Time
	updatedAt time.Time
}

func New(id connectionidentity.ID, label string, spec Spec, now time.Time) (Connection, error) {
	return Restore(id, label, spec, Revision{value: 1}, now, now)
}

// Restore validates durable snapshots without inventing current timestamps or
// silently repairing a corrupt identity/revision. Storage codecs own wire shape.
func Restore(id connectionidentity.ID, label string, spec Spec, revision Revision, createdAt, updatedAt time.Time) (Connection, error) {
	if id.String() == "" || revision.value == 0 {
		return Connection{}, errors.New("Connection identity and revision are required")
	}
	if err := validateLabel(label); err != nil {
		return Connection{}, err
	}
	if err := validateSpec(spec); err != nil {
		return Connection{}, err
	}
	if createdAt.IsZero() || updatedAt.IsZero() || updatedAt.Before(createdAt) {
		return Connection{}, errors.New("invalid Connection timestamps")
	}
	return Connection{id: id, label: label, spec: spec, revision: revision, createdAt: createdAt.UTC(), updatedAt: updatedAt.UTC()}, nil
}
func validateLabel(label string) error {
	if label == "" || label != strings.TrimSpace(label) || !utf8.ValidString(label) {
		return errors.New("Connection display name is required without surrounding whitespace")
	}
	for _, c := range label {
		if unicode.IsControl(c) {
			return errors.New("Connection display name contains control characters")
		}
	}
	return nil
}
func validateSpec(spec Spec) error {
	switch spec := spec.(type) {
	case ModelSpec:
		_, err := NewModelSpec(spec.upstream)
		return err
	case MCPHTTPSpec:
		if _, err := newMCPDeclarations(spec.ExecutionIdentity(), spec.PermissionSummary()); err != nil {
			return err
		}
		_, err := NewMCPHTTPSpec(spec.endpoint, spec.credential)
		return err
	case MCPStdioSpec:
		if _, err := newMCPDeclarations(spec.ExecutionIdentity(), spec.PermissionSummary()); err != nil {
			return err
		}
		_, err := NewMCPStdioSpec(spec.executable, spec.args, spec.environment)
		return err
	case SearchSpec:
		_, err := NewSearchSpec(spec.provider, spec.endpoint, spec.credential)
		return err
	default:
		return errors.New("validated Connection spec is required")
	}
}

var ErrRevisionConflict = errors.New("Connection revision changed")
var ErrIdentityChange = errors.New("create a new Connection for a different kind or provider")

// EditSameIdentity requires explicit operator intent at the application edge.
// It cannot turn a model account into another provider or protocol kind. The
// operator assertion about account/server identity remains a command contract;
// labels or URLs cannot infer whether two external accounts are the same.
func (c Connection) EditSameIdentity(expected Revision, label string, spec Spec, now time.Time) (Connection, error) {
	if c.revision.value == 0 || expected != c.revision {
		return Connection{}, ErrRevisionConflict
	}
	if err := validateSpec(spec); err != nil {
		return Connection{}, err
	}
	if spec.Kind() != c.spec.Kind() {
		return Connection{}, ErrIdentityChange
	}
	if left, ok := c.spec.(ModelSpec); ok {
		right, ok := spec.(ModelSpec)
		if !ok || left.upstream.Provider() != right.upstream.Provider() {
			return Connection{}, ErrIdentityChange
		}
	}
	if left, ok := c.spec.(SearchSpec); ok {
		right, ok := spec.(SearchSpec)
		if !ok || left.provider != right.provider {
			return Connection{}, ErrIdentityChange
		}
	}
	if c.revision.value == math.MaxUint64 {
		return Connection{}, errors.New("Connection revision exhausted")
	}
	if now.Before(c.updatedAt) {
		return Connection{}, errors.New("Connection update time moved backwards")
	}
	return Restore(c.id, label, spec, Revision{value: c.revision.value + 1}, c.createdAt, now)
}
func (c Connection) ID() connectionidentity.ID { return c.id }
func (c Connection) DisplayName() string       { return c.label }
func (c Connection) Spec() Spec                { return c.spec }
func (c Connection) Revision() Revision        { return c.revision }
func (c Connection) CreatedAt() time.Time      { return c.createdAt }
func (c Connection) UpdatedAt() time.Time      { return c.updatedAt }

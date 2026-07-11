package canonical

import "fmt"

// AliasKey names one native identifier that points at one canonical envelope.
type AliasKey struct {
	Protocol string
	Kind     string
	NativeID string
	Index    int
}

// AliasTable records native aliases for canonical envelope IDs without ever
// renaming the canonical envelope itself.
type AliasTable struct {
	nativeToCanonical map[AliasKey]EnvelopeID
}

// NewAliasTable constructs an empty alias table.
func NewAliasTable() AliasTable {
	return AliasTable{nativeToCanonical: map[AliasKey]EnvelopeID{}}
}

// Remember stores one native alias for one canonical envelope ID.
func (t *AliasTable) Remember(key AliasKey, canonicalID EnvelopeID) error {
	if t == nil {
		return fmt.Errorf("alias table is required")
	}
	if key.Protocol == "" {
		return fmt.Errorf("alias protocol is required")
	}
	if key.Kind == "" {
		return fmt.Errorf("alias kind is required")
	}
	if key.NativeID == "" {
		return fmt.Errorf("alias native id is required")
	}
	if canonicalID == "" {
		return fmt.Errorf("canonical envelope id is required")
	}
	if t.nativeToCanonical == nil {
		t.nativeToCanonical = map[AliasKey]EnvelopeID{}
	}
	if existing, ok := t.nativeToCanonical[key]; ok && existing != canonicalID {
		return fmt.Errorf("alias %q already points at canonical envelope %q", key.NativeID, existing)
	}
	t.nativeToCanonical[key] = canonicalID
	return nil
}

// Resolve looks up the canonical envelope ID for one native alias.
func (t *AliasTable) Resolve(key AliasKey) (EnvelopeID, bool) {
	if t == nil || t.nativeToCanonical == nil {
		return "", false
	}
	id, ok := t.nativeToCanonical[key]
	return id, ok
}

// Clone copies the alias table without sharing the backing map.
func (t AliasTable) Clone() AliasTable {
	if t.nativeToCanonical == nil {
		return AliasTable{}
	}
	out := NewAliasTable()
	for key, id := range t.nativeToCanonical {
		out.nativeToCanonical[key] = id
	}
	return out
}

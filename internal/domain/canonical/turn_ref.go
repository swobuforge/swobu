package canonical

import "strings"

// PreviousResponseID is a Swobu-owned logical handle used as a turn selector.
type PreviousResponseID string

// NewPreviousResponseID normalizes one previous-response handle into canonical form.
func NewPreviousResponseID(raw string) PreviousResponseID {
	return PreviousResponseID(strings.TrimSpace(raw)) // swobu:io-string source=domain
}

func (id PreviousResponseID) IsZero() bool {
	return strings.TrimSpace(string(id)) == "" // swobu:io-string source=domain
}

func (id PreviousResponseID) String() string {
	return string(id)
}

// Clone returns a stable copy of the logical previous-response handle.
func (id PreviousResponseID) Clone() PreviousResponseID {
	return PreviousResponseID(string(id))
}

// TurnRef captures request-scoped semantic previous-response intent.
type TurnRef struct {
	Previous *PreviousResponseID
}

// NewTurnRef converts a wire-level parent selector into canonical turn intent.
func NewTurnRef(previous string) TurnRef {
	id := NewPreviousResponseID(previous)
	if id.IsZero() {
		return TurnRef{}
	}
	return TurnRef{Previous: &id}
}

func (r TurnRef) IsZero() bool {
	return r.Previous == nil || r.Previous.IsZero()
}

func (r TurnRef) PreviousID() (PreviousResponseID, bool) {
	if r.Previous == nil || r.Previous.IsZero() {
		return PreviousResponseID(""), false
	}
	return r.Previous.Clone(), true
}

func (r TurnRef) Clone() TurnRef {
	if r.Previous == nil {
		return TurnRef{}
	}
	id := r.Previous.Clone()
	return TurnRef{Previous: &id}
}

// CurrentTurnDelta returns the items that belong to the current turn only
// (from the last user item onward). This is used by wire encoders that send
// provider-native previous-response deltas: the provider already has previous
// state, so only the delta needs to be encoded.
//
// If there is no user item, a clone of the full thread is returned so the
// caller can mutate the result safely.
func CurrentTurnDelta(thread []CanonicalItem) []CanonicalItem {
	start := 0
	for i := len(thread) - 1; i >= 0; i-- {
		if thread[i].Author == ItemAuthorUser {
			start = i
			break
		}
	}
	return cloneCanonicalItems(thread[start:])
}

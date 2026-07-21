package responsesnative

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

type item struct {
	raw []byte
}

// Items is one ordered collection of complete Responses JSON objects. Input
// and output direction belongs to the field carrying Items; their validation,
// storage, cloning, and redaction invariants are identical.
type Items struct {
	items []item
}

// NewItems validates and defensively copies complete JSON objects. JSON
// property order is retained even though replay does not depend on it.
func NewItems(objects [][]byte) (Items, error) {
	items := Items{items: make([]item, len(objects))}
	for index, object := range objects {
		raw := bytes.TrimSpace(object)
		if len(raw) == 0 || raw[0] != '{' || !json.Valid(raw) {
			return Items{}, fmt.Errorf("Responses native item %d is not a complete JSON object", index)
		}
		items.items[index] = item{raw: append([]byte(nil), raw...)}
	}
	return items, nil
}

func (i Items) Len() int     { return len(i.items) }
func (i Items) IsZero() bool { return i.items == nil }

// JSONObjects returns defensive copies for the Responses wire codec.
func (i Items) JSONObjects() [][]byte {
	objects := make([][]byte, len(i.items))
	for index, value := range i.items {
		objects[index] = append([]byte(nil), value.raw...)
	}
	return objects
}

func (i Items) Clone() Items {
	if i.items == nil {
		return Items{}
	}
	cloned, _ := NewItems(i.JSONObjects())
	return cloned
}

func (i Items) String() string {
	return fmt.Sprintf("responsesnative.Items{items:%d,raw:[REDACTED]}", len(i.items))
}

func (i Items) GoString() string { return i.String() }

// Turn preserves one invocation boundary: its portable semantic input, any
// exact native input objects, and the exact native output objects they caused.
type Turn struct {
	canonicalInput canonical.CanonicalRequest
	nativeInput    Items
	output         Items
}

func NewTurn(canonicalInput canonical.CanonicalRequest, nativeInput Items, output Items) Turn {
	return Turn{canonicalInput: canonicalInput.Clone(), nativeInput: nativeInput.Clone(), output: output.Clone()}
}

func (t Turn) CanonicalInput() canonical.CanonicalRequest { return t.canonicalInput.Clone() }
func (t Turn) NativeInput() Items                         { return t.nativeInput.Clone() }
func (t Turn) Output() Items                              { return t.output.Clone() }
func (t Turn) Clone() Turn                                { return NewTurn(t.canonicalInput, t.nativeInput, t.output) }
func (t Turn) String() string {
	return fmt.Sprintf("responsesnative.Turn{output:%s}", t.output)
}
func (t Turn) GoString() string { return t.String() }

// History is the ordered Responses-only continuation transcript resolved from
// checkpoint ancestry.
type History struct {
	turns []Turn
}

func NewHistory(turns []Turn) History {
	history := History{turns: make([]Turn, len(turns))}
	for index, turn := range turns {
		history.turns[index] = turn.Clone()
	}
	return history
}

func (h History) Len() int { return len(h.turns) }
func (h History) Turns() []Turn {
	turns := make([]Turn, len(h.turns))
	for index, turn := range h.turns {
		turns[index] = turn.Clone()
	}
	return turns
}
func (h History) Clone() History { return NewHistory(h.turns) }
func (h History) String() string {
	return fmt.Sprintf("responsesnative.History{turns:%d,raw:[REDACTED]}", len(h.turns))
}
func (h History) GoString() string { return h.String() }

// RequestState is the complete independent Responses replay refinement carried
// beside one canonical provider request.
type RequestState struct {
	input   Items
	history History
}

func NewRequestState(input Items, history History) RequestState {
	return RequestState{input: input.Clone(), history: history.Clone()}
}

func (s RequestState) Input() Items        { return s.input.Clone() }
func (s RequestState) History() History    { return s.history.Clone() }
func (s RequestState) Clone() RequestState { return NewRequestState(s.input, s.history) }

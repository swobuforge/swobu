package canonical

import (
	"reflect"
	"strings"
)

type ContinuationNamespace string

// NewContinuationNamespace builds the internal replay bucket used to search for
// prior chains. It is intentionally not a public session identifier; callers
// may derive it from routing scope while actual chain identity comes from
// native previous_response_id values or canonical prefix matching inside that
// bucket.
func NewContinuationNamespace(raw string) ContinuationNamespace {
	return ContinuationNamespace(strings.TrimSpace(raw)) // swobu:io-string source=domain
}

func (s ContinuationNamespace) IsZero() bool {
	return strings.TrimSpace(string(s)) == "" // swobu:io-string source=domain
}

func (s ContinuationNamespace) String() string {
	return string(s)
}

type ContinuationPrefixMatch struct {
	Snapshot     ContinuitySnapshot
	PrefixLength int
}

// ContinuitySnapshot is the minimal replayable canonical state Swobu keeps so
// response chains can be resumed without leaking backend wire history rules.
type ContinuitySnapshot struct {
	ResponseID string
	Model      string
	Thread     []CanonicalItem
}

func NewContinuitySnapshot(responseID string, model string, thread []CanonicalItem) ContinuitySnapshot {
	return ContinuitySnapshot{
		ResponseID: responseID,
		Model:      model,
		Thread:     cloneCanonicalItems(thread),
	}
}

func (s ContinuitySnapshot) Clone() ContinuitySnapshot {
	return NewContinuitySnapshot(s.ResponseID, s.Model, s.Thread)
}

// ValidateResponseContinuationSelectors enforces the narrowed v0 responses
// contract: previous_response_id is the only supported native selector.
func ValidateResponseContinuationSelectors(request GenerationCanonicalRequest) error {
	if request.invalidContinuationPair {
		return BadRequest("responses request must not specify both previous_response_id and conversation")
	}
	if request.ConversationID() != "" {
		return BadRequest("responses conversation is not supported in swobu v0")
	}
	return nil
}

func PreviousResponseIDFromRequest(request CanonicalRequest) (string, bool, error) {
	typed, ok := request.(GenerationCanonicalRequest)
	if !ok {
		return "", false, nil
	}
	if err := ValidateResponseContinuationSelectors(typed); err != nil {
		return "", false, err
	}
	value := strings.TrimSpace(typed.PreviousResponseID()) // swobu:io-string source=domain
	if value == "" {
		return "", false, nil
	}
	return value, true, nil
}

// ContinuationConversation builds the canonical history for one request. It
// stays protocol-agnostic and therefore only understands canonical request
// semantics.
func ContinuationConversation(request CanonicalRequest) ([]CanonicalItem, bool, error) {
	switch typed := request.(type) {
	case DialogCanonicalRequest:
		items := typed.Items()
		return items, len(items) > 0, nil
	case GenerationCanonicalRequest:
		thread := typed.Thread()
		return thread, len(thread) > 0, nil
	default:
		return nil, false, nil
	}
}

// BuildContinuitySnapshot appends replayable successful output items to one
// replayable canonical thread when that thread exists for the request.
func BuildContinuitySnapshot(
	thread []CanonicalItem,
	output CanonicalOutput,
) (ContinuitySnapshot, bool, error) {
	if output == nil || output.ResultID() == "" || len(thread) == 0 {
		return ContinuitySnapshot{}, false, nil
	}
	items := output.Items()
	if len(items) == 0 {
		return ContinuitySnapshot{}, false, nil
	}
	for _, item := range items {
		switch item.Kind {
		case ItemKindText, ItemKindToolUse:
		default:
			return ContinuitySnapshot{}, false, UnsupportedOperation("canonical output item is not replayable in continuity state")
		}
	}
	return NewContinuitySnapshot(
		output.ResultID(),
		output.Model(),
		append(cloneCanonicalItems(thread), cloneCanonicalItems(items)...),
	), true, nil
}

// longestCommonPrefixLength is the semantic diff primitive for continuation
// derivation. Prefix matching is content-based on canonical items; storage
// lineage matters only insofar as it yields a usable prefix anchor.
func longestCommonPrefixLength(left []CanonicalItem, right []CanonicalItem) int {
	limit := len(left)
	if len(right) < limit {
		limit = len(right)
	}
	for i := 0; i < limit; i++ {
		if !reflect.DeepEqual(left[i], right[i]) {
			return i
		}
	}
	return limit
}

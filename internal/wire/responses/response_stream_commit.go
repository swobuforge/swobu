package responses

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// committedOutputIdentity is the immutable provider identity admitted for one
// output index. Providers may omit fields on later frames, but every repeated
// non-empty field must agree with the first observed value.
type committedOutputIdentity struct {
	kind     string
	itemID   string
	callID   string
	toolName string
}

// pendingResponseOutput is the sole lifecycle owner for one provider
// output index. Identity, progressive kind state, buffered canonical events,
// erasure evidence, and resolution cannot be looked up through any other key.
//
// State transitions:
//
//	absent → active(progressive frames) → resolved(known canonical group)
//	   │              │
//	   │              └→ deferred(partial item; terminal contract required)
//	   └────────────────→ resolved(erased; zero canonical items)
//
// Resolved states are terminal. Provider order commits only while the
// contiguous resolved frontier advances; known groups contribute their span
// and erased groups contribute zero.
type pendingResponseOutput struct {
	identity           committedOutputIdentity
	events             canonical.EventSequence
	text               *responsesTextState
	tool               *responsesToolState
	reasoning          *responsesReasoningState
	checkpointItems    []canonical.CanonicalItem
	checkpointStatus   string
	deferredItem       json.RawMessage
	base               uint32
	span               uint32
	active             bool
	resolved           bool
	erased             bool
	ignoredAtAdded     bool
	erasureRecorded    bool
	statusDropRecorded bool
}

func responsesIdentityForFrame(frameType string, frame streamFrame) committedOutputIdentity {
	identity := committedOutputIdentity{
		kind:     strings.TrimSpace(frame.Item.Type),
		itemID:   strings.TrimSpace(frame.Item.ID),
		callID:   strings.TrimSpace(frame.Item.CallID),
		toolName: strings.TrimSpace(frame.Item.Name),
	}
	if identity.itemID == "" {
		identity.itemID = strings.TrimSpace(frame.ItemID)
	}
	if identity.callID == "" {
		identity.callID = strings.TrimSpace(frame.CallID)
	}
	if identity.toolName == "" {
		identity.toolName = strings.TrimSpace(frame.Name)
	}
	if identity.kind != "" {
		return identity
	}
	switch frameType {
	case "response.output_text.delta":
		identity.kind = "message"
	case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
		identity.kind = "reasoning"
	case "response.function_call_arguments.delta", "response.function_call_arguments.done":
		identity.kind = "function_call"
	case "response.custom_tool_call_input.delta", "response.custom_tool_call_input.done":
		identity.kind = "custom_tool_call"
	case "response.mcp_call_arguments.delta", "response.mcp_call_arguments.done":
		identity.kind = "mcp_call"
	}
	return identity
}

// acceptOutputFrame admits one frame to its indexed owner. The boolean
// result reports frozen duplicate evidence that dispatch must ignore so a
// resolved output cannot emit events or change span.
func (s *responsesResponseStream) acceptOutputFrame(frameType string, frame streamFrame) (bool, error) {
	if frame.OutputIndex == nil {
		return false, canonical.NewBackendError("responses", 0, "responses output lifecycle is missing output index", "")
	}
	index := *frame.OutputIndex
	if index < 0 {
		return false, canonical.NewBackendError("responses", 0, "responses output index is negative", "")
	}
	state := s.outputAt(index)
	identity := responsesIdentityForFrame(frameType, frame)
	if state.resolved {
		if err := state.identity.validateFrozen(identity); err != nil {
			return false, err
		}
		if frameType != "response.output_item.done" {
			return false, canonical.NewBackendError("responses", 0, "responses output lifecycle continued after resolution", "")
		}
		return true, nil
	}
	if len(state.deferredItem) > 0 {
		return false, canonical.NewBackendError("responses", 0, "responses output lifecycle continued after deferred completion", "")
	}
	// An unknown announcement is deliberately open, not erased. Its unknown
	// kind cannot later become a known item after the output frontier has
	// already treated its deltas as ignored.
	if state.ignoredAtAdded && state.identity.kind == "" && identity.kind != "" {
		return false, canonical.NewBackendError("responses", 0, "responses ignored output kind changed during lifecycle", "")
	}
	if err := state.identity.merge(identity); err != nil {
		return false, err
	}
	s.publishReadyOutputs()
	return false, nil
}

func (s *responsesResponseStream) acceptTerminalOutput(index int, item responsesWireOutputItemDTO) error {
	state := s.outputAt(index)
	identity := committedOutputIdentity{
		kind:     strings.TrimSpace(item.Type),
		itemID:   strings.TrimSpace(item.ID),
		callID:   strings.TrimSpace(item.CallID),
		toolName: strings.TrimSpace(item.Name),
	}
	if state.resolved {
		return state.identity.validateFrozen(identity)
	}
	if state.ignoredAtAdded && state.identity.kind == "" && identity.kind != "" {
		return canonical.NewBackendError("responses", 0, "responses ignored output kind changed during lifecycle", "")
	}
	return state.identity.merge(identity)
}

func (i *committedOutputIdentity) merge(next committedOutputIdentity) error {
	if err := mergeResponsesIdentityField("type", &i.kind, next.kind); err != nil {
		return err
	}
	if err := mergeResponsesIdentityField("item_id", &i.itemID, next.itemID); err != nil {
		return err
	}
	if err := mergeResponsesIdentityField("call_id", &i.callID, next.callID); err != nil {
		return err
	}
	return mergeResponsesIdentityField("tool name", &i.toolName, next.toolName)
}

func (i committedOutputIdentity) validateFrozen(next committedOutputIdentity) error {
	if err := validateFrozenResponsesIdentityField("type", i.kind, next.kind); err != nil {
		return err
	}
	if err := validateFrozenResponsesIdentityField("item_id", i.itemID, next.itemID); err != nil {
		return err
	}
	if err := validateFrozenResponsesIdentityField("call_id", i.callID, next.callID); err != nil {
		return err
	}
	return validateFrozenResponsesIdentityField("tool name", i.toolName, next.toolName)
}

func validateFrozenResponsesIdentityField(field string, admitted string, next string) error {
	next = strings.TrimSpace(next)
	if next == "" {
		return nil
	}
	if admitted == "" || admitted != next {
		return canonical.NewBackendError("responses", 0, fmt.Sprintf("responses resolved output %s changed during lifecycle", field), "")
	}
	return nil
}

func mergeResponsesIdentityField(field string, admitted *string, next string) error {
	next = strings.TrimSpace(next)
	if next == "" {
		return nil
	}
	if *admitted == "" {
		*admitted = next
		return nil
	}
	if *admitted != next {
		return canonical.NewBackendError("responses", 0, fmt.Sprintf("responses output %s changed during lifecycle", field), "")
	}
	return nil
}

func (s *responsesResponseStream) outputAt(index int) *pendingResponseOutput {
	if s.providerOutputs == nil {
		s.providerOutputs = make(map[int]*pendingResponseOutput)
	}
	state := s.providerOutputs[index]
	if state == nil {
		state = &pendingResponseOutput{}
		s.providerOutputs[index] = state
	}
	return state
}

func (s *responsesResponseStream) stageOutputEvent(outputIndex *int, event canonical.Event) {
	itemEvent, itemScoped := event.Payload.(canonical.ItemEvent)
	if !itemScoped {
		s.enqueue(event)
		return
	}
	// Aggregate terminal output_text has no provider output occurrence. It is
	// the only item-producing path admitted outside the indexed stream model.
	if outputIndex == nil {
		s.enqueue(event)
		return
	}
	if *outputIndex < 0 {
		return
	}
	state := s.outputAt(*outputIndex)
	if state.resolved {
		return
	}
	if itemEvent.Position.Item+1 > state.span {
		state.span = itemEvent.Position.Item + 1
	}
	if state.active {
		s.enqueue(rebaseResponsesOutputEvent(event, state.base))
		return
	}
	state.events = append(state.events, event)
}

func rebaseResponsesOutputEvent(event canonical.Event, base uint32) canonical.Event {
	itemEvent := event.Payload.(canonical.ItemEvent)
	itemEvent.Position.Item += base
	event.Payload = itemEvent
	return event
}

func (s *responsesResponseStream) completeOutput(outputIndex *int) {
	if outputIndex == nil || *outputIndex < 0 {
		return
	}
	state := s.outputAt(*outputIndex)
	state.resolved = true
	s.publishReadyOutputs()
}

func (s *responsesResponseStream) publishReadyOutputs() {
	for {
		state := s.providerOutputs[s.outputFrontier]
		if state == nil {
			return
		}
		if !state.active {
			state.active = true
			state.base = s.nextOrdinal
			for _, event := range state.events {
				s.enqueue(rebaseResponsesOutputEvent(event, state.base))
			}
			state.events = nil
		}
		if !state.resolved {
			return
		}
		s.nextOrdinal = state.base + state.span
		s.outputFrontier++
	}
}

func (s *responsesResponseStream) isOutputComplete(index int) bool {
	state := s.providerOutputs[index]
	return state != nil && state.resolved
}

func (s *responsesResponseStream) dropOutput(outputIndex *int) {
	if outputIndex == nil || *outputIndex < 0 {
		return
	}
	state := s.outputAt(*outputIndex)
	state.erased = true
	state.span = 0
	state.events = nil
}

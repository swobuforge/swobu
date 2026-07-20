package responses

import (
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func (s *responsesResponseStream) handleReasoningDelta(frame streamFrame, kind canonical.ReasoningPartKind) error {
	s.emittedOutput = true
	itemID := fallbackItemID(frame.ItemID, "reasoning", frame.OutputIndex)
	state := s.reasoningStates[itemID]
	if state == nil {
		ordinal := s.ordinalFor(itemID, frame.OutputIndex)
		state = &responsesReasoningState{ordinal: ordinal, id: frame.ItemID}
		s.reasoningStates[itemID] = state
	}
	partIndex := len(state.parts)
	if frame.SummaryIndex != nil {
		partIndex = *frame.SummaryIndex
	} else if frame.ContentIndex != nil {
		partIndex = *frame.ContentIndex
	} else if len(state.parts) > 0 {
		partIndex = len(state.parts) - 1
	}
	if partIndex < 0 || partIndex > len(state.parts) {
		return canonical.InternalError("responses reasoning part index is non-contiguous")
	}
	if partIndex == len(state.parts) {
		state.parts = append(state.parts, &responsesReasoningStreamPartState{kind: kind})
	}
	part := state.parts[partIndex]
	if part.kind != kind {
		return canonical.InternalError("responses reasoning part kind changed during stream")
	}
	part.text.WriteString(frame.Delta)
	return nil
}

func (s *responsesResponseStream) completeReasoningState(frame streamFrame) (bool, error) {
	itemID := fallbackItemID(frame.Item.ID, "reasoning", frame.OutputIndex)
	state := s.reasoningStates[itemID]
	if state == nil {
		ordinal := s.ordinalFor(itemID, frame.OutputIndex)
		state = &responsesReasoningState{ordinal: ordinal, id: frame.Item.ID, status: frame.Item.Status}
	}
	parts := make([]canonical.ReasoningPart, 0, len(state.parts))
	for _, streamed := range state.parts {
		part, err := canonical.NewReasoningPart(streamed.kind, streamed.text.String())
		if err != nil {
			return false, canonical.InternalError("responses streamed reasoning part is invalid")
		}
		parts = append(parts, part)
	}
	if len(parts) == 0 {
		for _, summary := range frame.Item.Summary {
			if strings.TrimSpace(summary.Type) != "summary_text" { // swobu:io-string source=provider-wire
				return false, canonical.InternalError("responses streamed reasoning summary type is invalid")
			}
			part, err := canonical.NewReasoningPart(canonical.ReasoningPartSummary, summary.Text)
			if err != nil {
				return false, canonical.InternalError("responses streamed reasoning summary is invalid")
			}
			parts = append(parts, part)
		}
		content, err := decodeResponsesReasoningContent(frame.Item.Content)
		if err != nil {
			return false, err
		}
		if len(content) > 0 || frame.Item.EncryptedContent != "" {
			return false, canonical.UnsupportedOperation("responses manual reasoning state is not supported in P0")
		}
	}
	if len(parts) == 0 {
		delete(s.reasoningStates, itemID)
		return true, nil
	}
	item, err := canonical.NewReasoningItem(parts, canonical.OpaqueThinking{})
	if err != nil {
		return false, canonical.InternalError("responses streamed reasoning item is invalid")
	}
	s.enqueueItemCompleted("", state.ordinal, item)
	delete(s.reasoningStates, itemID)
	return true, nil
}

package responses

import (
	"strings"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func (s *responsesResponseStream) handleReasoningDelta(frame streamFrame, kind canonical.ReasoningPartKind) error {
	output := s.outputAt(*frame.OutputIndex)
	state := output.reasoning
	if state == nil {
		state = newResponsesReasoningState()
		output.reasoning = state
	}
	switch kind {
	case canonical.ReasoningPartSummary:
		if frame.ContentIndex != nil {
			return canonical.NewBackendError("responses", 0, "responses reasoning summary delta uses a content index", "")
		}
		return appendResponsesReasoningDelta(&state.summaryParts, frame.SummaryIndex, frame.Delta)
	case canonical.ReasoningPartTrace:
		if frame.SummaryIndex != nil {
			return canonical.NewBackendError("responses", 0, "responses reasoning trace delta uses a summary index", "")
		}
		return appendResponsesReasoningDelta(&state.traceParts, frame.ContentIndex, frame.Delta)
	default:
		return canonical.InternalError("responses reasoning delta kind is invalid")
	}
}

func appendResponsesReasoningDelta(parts *map[int]*responsesReasoningStreamPartState, wireIndex *int, delta string) error {
	index := 0
	if wireIndex != nil {
		index = *wireIndex
	} else if len(*parts) > 0 {
		for candidate := range *parts {
			if candidate > index {
				index = candidate
			}
		}
	}
	if index < 0 {
		return canonical.NewBackendError("responses", 0, "responses reasoning part index is negative", "")
	}
	part := (*parts)[index]
	if part == nil {
		part = &responsesReasoningStreamPartState{}
		(*parts)[index] = part
	}
	part.text.WriteString(delta)
	return nil
}

func (s *responsesResponseStream) completeReasoningState(frame streamFrame) (bool, error) {
	output := s.outputAt(*frame.OutputIndex)
	state := output.reasoning
	if state == nil {
		state = newResponsesReasoningState()
	}
	var opaque canonical.OpaqueThinking
	if frame.Item.EncryptedContent != "" {
		var opaqueErr error
		// RFC G2 §7.4: use the committed stream identity (merged across frames),
		// not whichever terminal frame happens to carry an id. output.identity.itemID
		// is already the sole validated id for this output index; a later frame
		// omitting it (or repeating it) leaves the committed value intact.
		opaque, opaqueErr = canonical.NewResponsesOpaqueThinking(canonical.ResponsesReasoningReplay{EncryptedContent: frame.Item.EncryptedContent, ItemID: output.identity.itemID})
		if opaqueErr != nil {
			return false, canonical.InternalError("responses streamed encrypted reasoning is invalid")
		}
	}

	parts := make([]canonical.ReasoningPart, 0, len(frame.Item.Summary)+len(state.traceParts))
	erasedChild := false
	for index, summary := range frame.Item.Summary {
		summaryType := strings.TrimSpace(summary.Type) // swobu:io-string source=provider-wire
		if err := admitResponsesProviderOutputChild(summaryType); err != nil {
			return false, err
		}
		if summaryType != "summary_text" {
			if state.summaryParts[index] != nil {
				return false, canonical.NewBackendError("responses", 0, "responses reasoning summary checkpoint changed part kind", "")
			}
			erasedChild = true
			if err := appendResponsesOccurrenceChange(s.changeLog, s.exchangeID, canonical.ResponseItemsKind, compat.Omission, responsesReasoningStreamSubject(frame, "summary", index)); err != nil {
				return false, err
			}
			continue
		}
		if progressive := state.summaryParts[index]; progressive != nil {
			if _, err := responsesTerminalSuffix(progressive.text.String(), summary.Text); err != nil {
				return false, err
			}
		}
		part, err := canonical.NewReasoningPart(canonical.ReasoningPartSummary, summary.Text)
		if err != nil {
			return false, canonical.InternalError("responses streamed reasoning summary is invalid")
		}
		parts = append(parts, part)
	}
	for index := range state.summaryParts {
		if index >= len(frame.Item.Summary) {
			return false, canonical.NewBackendError("responses", 0, "responses terminal checkpoint is missing streamed reasoning summary", "")
		}
	}

	content, err := decodeResponsesReasoningContent(frame.Item.Content)
	if err != nil {
		return false, err
	}
	for index, trace := range content {
		traceType := strings.TrimSpace(trace.Type) // swobu:io-string source=provider-wire
		if err := admitResponsesProviderOutputChild(traceType); err != nil {
			return false, err
		}
		if traceType != "reasoning_text" {
			if state.traceParts[index] != nil {
				return false, canonical.NewBackendError("responses", 0, "responses reasoning trace checkpoint changed part kind", "")
			}
			erasedChild = true
			if err := appendResponsesOccurrenceChange(s.changeLog, s.exchangeID, canonical.ResponseItemsKind, compat.Omission, responsesReasoningStreamSubject(frame, "content", index)); err != nil {
				return false, err
			}
			continue
		}
		if progressive := state.traceParts[index]; progressive != nil {
			if _, err := responsesTerminalSuffix(progressive.text.String(), trace.Text); err != nil {
				return false, err
			}
		}
		part, err := canonical.NewReasoningPart(canonical.ReasoningPartTrace, trace.Text)
		if err != nil {
			return false, canonical.InternalError("responses streamed reasoning trace is invalid")
		}
		parts = append(parts, part)
	}
	for index := range state.traceParts {
		if index >= len(content) {
			return false, canonical.NewBackendError("responses", 0, "responses terminal checkpoint is missing streamed reasoning trace", "")
		}
	}

	if len(parts) == 0 && opaque.IsZero() {
		s.omitProviderOutput(frame.OutputIndex)
		if erasedChild {
			s.erasedOutput = true
		}
		output.reasoning = nil
		return true, nil
	}
	item, err := canonical.NewReasoningItem(parts, opaque)
	if err != nil {
		return false, canonical.InternalError("responses streamed reasoning item is invalid")
	}
	ordinal := uint32(0)
	s.enqueueItemCompleted(frame.OutputIndex, ordinal, item)
	output.reasoning = nil
	return true, nil
}

func responsesReasoningStreamSubject(frame streamFrame, _ string, _ int) canonical.Occurrence {
	outputIndex := 0
	if frame.OutputIndex != nil {
		outputIndex = *frame.OutputIndex
	}
	return canonical.ResponseItemOccurrence(uint32(outputIndex))
}

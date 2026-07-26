package chatcompletions

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
	openaiwire "github.com/swobuforge/swobu/internal/wire/openai"
)

const chatToolResultImageMarkerVersion = 1

type chatToolResultImageMarker struct {
	Swobu chatToolResultImageMarkerBody `json:"_swobu"`
}

type chatToolResultImageMarkerBody struct {
	Version int    `json:"v"`
	Kind    string `json:"kind"`
	CallID  string `json:"call_id"`
	Part    int    `json:"part"`
}

type chatToolResultImageOccurrence struct {
	image  canonical.ImagePart
	callID canonical.ToolCallID
	item   int
	part   int
}

// chatActiveToolBatch is attempt-local projection state. It proves whether a
// synthetic user message can follow the current run without crossing an
// unresolved Chat tool-call boundary.
type chatActiveToolBatch struct {
	unresolved map[canonical.ToolCallID]struct{}
}

func newChatActiveToolBatch(callIDs []canonical.ToolCallID) *chatActiveToolBatch {
	batch := &chatActiveToolBatch{unresolved: make(map[canonical.ToolCallID]struct{}, len(callIDs))}
	for _, callID := range callIDs {
		batch.unresolved[callID] = struct{}{}
	}
	return batch
}

func (b *chatActiveToolBatch) clone() *chatActiveToolBatch {
	if b == nil {
		return nil
	}
	out := &chatActiveToolBatch{unresolved: make(map[canonical.ToolCallID]struct{}, len(b.unresolved))}
	for callID := range b.unresolved {
		out.unresolved[callID] = struct{}{}
	}
	return out
}

func (b *chatActiveToolBatch) resolve(callID canonical.ToolCallID) bool {
	if b == nil {
		return false
	}
	if _, ok := b.unresolved[callID]; !ok {
		return false
	}
	delete(b.unresolved, callID)
	return true
}

func (b *chatActiveToolBatch) closed() bool {
	return b != nil && len(b.unresolved) == 0
}

// encodeChatToolResultRun emits every tool message before any compatibility
// image message. Re-homing is legal only when this exact run closes the active
// assistant tool-call batch.
func encodeChatToolResultRun(
	items []canonical.CanonicalItem,
	start int,
	active *chatActiveToolBatch,
	sink compat.Sink,
	exchangeID string,
) ([]ProviderRequestMessage, int, *chatActiveToolBatch, error) {
	end := start
	for end < len(items) && items[end].Kind() == canonical.ItemKindToolResult {
		end++
	}

	nextBatch := active.clone()
	provenBatch := active != nil
	allResultsMatch := provenBatch
	occurrences := make([]chatToolResultImageOccurrence, 0)
	var contentLossErr error
	for itemIndex := start; itemIndex < end; itemIndex++ {
		result, ok := items[itemIndex].ToolResult()
		if !ok || result.CallID().IsZero() {
			return nil, start, active, canonical.InternalError("chat completions tool-result item payload is invalid")
		}
		if !nextBatch.resolve(result.CallID()) {
			allResultsMatch = false
		}
		if err := recordChatToolResultLosses(result, sink, exchangeID); err != nil && contentLossErr == nil {
			contentLossErr = err
		}
		for partIndex, part := range result.Content() {
			if image, ok := part.Image(); ok {
				occurrences = append(occurrences, chatToolResultImageOccurrence{
					image: image, callID: result.CallID(), item: itemIndex, part: partIndex,
				})
			}
		}
	}

	if len(occurrences) > 0 {
		first := occurrences[0]
		if !provenBatch || !allResultsMatch || !nextBatch.closed() {
			if err := emitChatImageDecision(sink, exchangeID, compat.RequestItemsToolResultImage, compat.Reject); err != nil {
				return nil, start, active, err
			}
			return nil, start, active, unsupportedToolResultImage(first, "active tool-call batch is not provably closed")
		}
	}
	if contentLossErr != nil {
		return nil, start, active, contentLossErr
	}

	out := make([]ProviderRequestMessage, 0, end-start+1)
	allImages := make([]chatToolResultImageOccurrence, 0, len(occurrences))
	for itemIndex := start; itemIndex < end; itemIndex++ {
		result, _ := items[itemIndex].ToolResult()
		content, images, err := encodeChatToolResultContent(result, itemIndex)
		if err != nil {
			return nil, start, active, err
		}
		allImages = append(allImages, images...)
		out = append(out, ProviderRequestMessage{
			Role:        "tool",
			Content:     content,
			ToolCallID:  result.CallID().String(),
			SourceStart: itemIndex,
			SourceEnd:   itemIndex + 1,
		})
	}

	if len(allImages) > 0 {
		if err := emitChatImageDecision(sink, exchangeID, compat.RequestItemsToolResultImage, compat.Approx); err != nil {
			return nil, start, active, err
		}
		content := make([]any, 0, len(allImages)*2)
		for _, occurrence := range allImages {
			marker, err := encodeChatToolResultImageMarker("tool_result_image", occurrence)
			if err != nil {
				return nil, start, active, err
			}
			content = append(content, map[string]any{"type": "text", "text": marker})
			image, err := encodeChatImage(
				occurrence.image,
				compat.RequestItemsToolResultImageDetail,
				sink,
				exchangeID,
			)
			if err != nil {
				return nil, start, active, err
			}
			content = append(content, image)
		}
		out = append(out, ProviderRequestMessage{
			Role: "user", Content: content, SourceStart: start, SourceEnd: end,
		})
	}

	if nextBatch != nil && nextBatch.closed() {
		nextBatch = nil
	}
	return out, end, nextBatch, nil
}

func recordChatToolResultLosses(result canonical.ToolResultItem, sink compat.Sink, exchangeID string) error {
	if result.IsError() {
		if err := emitChatImageDecision(sink, exchangeID, compat.RequestItemsToolResultIsError, compat.Approx); err != nil {
			return err
		}
	}
	if len(result.Content()) > 1 {
		if err := emitChatImageDecision(sink, exchangeID, compat.RequestItemsToolResultContentBoundaries, compat.Approx); err != nil {
			return err
		}
	}
	return nil
}

func encodeChatToolResultContent(result canonical.ToolResultItem, itemIndex int) (string, []chatToolResultImageOccurrence, error) {
	var content strings.Builder
	contentEndsWithNewline := false
	images := make([]chatToolResultImageOccurrence, 0)
	parts := result.Content()
	for partIndex, part := range parts {
		if text, ok := part.Text(); ok {
			value := text.Text()
			content.WriteString(value)
			if value != "" {
				contentEndsWithNewline = strings.HasSuffix(value, "\n")
			}
			continue
		}
		image, ok := part.Image()
		if !ok {
			return "", nil, provider.NewIncompatibleTarget("Chat Completions tool results cannot represent this canonical content kind")
		}
		occurrence := chatToolResultImageOccurrence{
			image: image, callID: result.CallID(), item: itemIndex, part: partIndex,
		}
		marker, err := encodeChatToolResultImageMarker("tool_result_image_ref", occurrence)
		if err != nil {
			return "", nil, err
		}
		if content.Len() > 0 && !contentEndsWithNewline {
			content.WriteByte('\n')
		}
		content.WriteString(marker)
		contentEndsWithNewline = false
		if partIndex+1 < len(parts) {
			content.WriteByte('\n')
			contentEndsWithNewline = true
		}
		images = append(images, occurrence)
	}
	return content.String(), images, nil
}

func encodeChatToolResultImageMarker(kind string, occurrence chatToolResultImageOccurrence) (string, error) {
	marker := chatToolResultImageMarker{Swobu: chatToolResultImageMarkerBody{
		Version: chatToolResultImageMarkerVersion,
		Kind:    kind,
		CallID:  occurrence.callID.String(),
		Part:    occurrence.part,
	}}
	raw, err := json.Marshal(marker)
	if err != nil {
		return "", canonical.InternalError("chat completions tool-result image marker could not be encoded")
	}
	return string(raw), nil
}

func encodeChatImage(
	image canonical.ImagePart,
	detailFeature compat.Feature,
	sink compat.Sink,
	exchangeID string,
) (map[string]any, error) {
	rawURL, detail, err := openaiwire.EncodeOpenAIImageURL(image)
	if err != nil {
		return nil, canonical.InternalError("canonical image source is invalid")
	}
	if detail == canonical.ImageDetailOriginal {
		if err := emitChatImageDecision(sink, exchangeID, detailFeature, compat.Approx); err != nil {
			return nil, err
		}
		detail = canonical.ImageDetailHigh
	}
	imageURL := map[string]string{"url": rawURL}
	if detail != "" {
		imageURL["detail"] = string(detail)
	}
	return map[string]any{"type": "image_url", "image_url": imageURL}, nil
}

func unsupportedToolResultImage(occurrence chatToolResultImageOccurrence, reason string) error {
	return provider.NewIncompatibleTarget(fmt.Sprintf(
		"Chat Completions cannot preserve canonical tool-result image at request item %d part %d for call %q: %s",
		occurrence.item,
		occurrence.part,
		occurrence.callID.String(),
		reason,
	))
}

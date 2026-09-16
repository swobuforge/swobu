package generatecontent

import (
	"encoding/base64"
	"encoding/json"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/wire"
)

type ResponseDocumentEncoder struct{}
type ResponseStreamEncoder struct{}

type responseDTO struct {
	Candidates    []candidateDTO `json:"candidates"`
	UsageMetadata *usageDTO      `json:"usageMetadata,omitempty"`
}
type candidateDTO struct {
	Content      responseContentDTO `json:"content"`
	FinishReason string             `json:"finishReason,omitempty"`
	Index        int                `json:"index"`
}
type responseContentDTO struct {
	Role  string            `json:"role"`
	Parts []responsePartDTO `json:"parts"`
}
type responsePartDTO struct {
	Text         *string                  `json:"text,omitempty"`
	Thought      *bool                    `json:"thought,omitempty"`
	InlineData   *inlineDataDTO           `json:"inlineData,omitempty"`
	FunctionCall *responseFunctionCallDTO `json:"functionCall,omitempty"`
}
type responseFunctionCallDTO struct {
	ID   string          `json:"id"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}
type usageDTO struct {
	PromptTokenCount     *int `json:"promptTokenCount,omitempty"`
	CandidatesTokenCount *int `json:"candidatesTokenCount,omitempty"`
	TotalTokenCount      *int `json:"totalTokenCount,omitempty"`
	ThoughtsTokenCount   *int `json:"thoughtsTokenCount,omitempty"`
}

func (ResponseDocumentEncoder) EncodeResponseDocument(request canonical.CanonicalRequest, output canonical.CanonicalResponse) (wire.ClientDocumentResult, error) {
	content, changes, err := projectResponseHistory(request, output.Items())
	if err != nil {
		return wire.ClientDocumentResult{}, err
	}
	fingerprint, err := fingerprintContentsResponse(content)
	if err != nil {
		return wire.ClientDocumentResult{}, err
	}
	parts := content.Parts
	responseParts := make([]responsePartDTO, 0, len(parts))
	for _, part := range parts {
		projected := responsePartDTO{Text: part.Text, Thought: part.Thought, InlineData: part.InlineData}
		if part.FunctionCall != nil {
			projected.FunctionCall = &responseFunctionCallDTO{ID: part.FunctionCall.ID, Name: part.FunctionCall.Name, Args: part.FunctionCall.Args}
		}
		responseParts = append(responseParts, projected)
	}
	finish, err := finishReason(output.Completion())
	if err != nil {
		return wire.ClientDocumentResult{}, err
	}
	usage, usageChanges := projectUsage(output.Usage())
	changes = appendChanges(changes, usageChanges)
	raw, err := json.Marshal(responseDTO{Candidates: []candidateDTO{{Content: responseContentDTO{Role: "model", Parts: responseParts}, FinishReason: finish, Index: 0}}, UsageMetadata: usage})
	if err != nil {
		return wire.ClientDocumentResult{}, err
	}
	return wire.ClientDocumentResult{Document: carrier.NewDocument(protocolkind.GenerateContent, "application/json", nil, raw, carrier.Meta{}), Changes: changes, ResponseFingerprint: &fingerprint}, nil
}

func projectResponseHistory(request canonical.CanonicalRequest, items []canonical.CanonicalItem) (contentDTO, []compat.Change, error) {
	parts := make([]partDTO, 0)
	var changes []compat.Change
	disclosure, disclosed := request.Reasoning().DisclosureField().Get()
	lastEmittedWasMessage := false
	for itemIndex, item := range items {
		if lastEmittedWasMessage && item.Kind() == canonical.ItemKindMessage {
			return contentDTO{}, nil, canonical.InternalError("GenerateContent cannot preserve consecutive response message boundaries")
		}
		switch item.Kind() {
		case canonical.ItemKindMessage:
			message, _ := item.Message()
			for partIndex, part := range message.Content() {
				if text, ok := part.Text(); ok {
					value := text.Text()
					parts = append(parts, partDTO{Text: &value})
					if len(part.Citations()) > 0 {
						changes = compat.AppendUnique(changes, compat.NewOmission(canonical.ResponseItemsMessageCitations, canonical.ResponsePartOccurrence(canonical.ItemPosition{Item: uint32(itemIndex), Part: uint32(partIndex)})))
					}
				} else if image, ok := part.Image(); ok {
					inline, exact := image.Source().Inline()
					if !exact {
						return contentDTO{}, nil, canonical.InternalError("GenerateContent response URL images require materialization")
					}
					parts = append(parts, partDTO{InlineData: &inlineDataDTO{MIMEType: string(inline.MediaType()), Data: base64.StdEncoding.EncodeToString(inline.Data())}})
				} else {
					return contentDTO{}, nil, canonical.InternalError("GenerateContent cannot project this response message part")
				}
			}
			lastEmittedWasMessage = true
		case canonical.ItemKindToolCall:
			call, _ := item.ToolCall()
			if call.Tool().Kind() == canonical.ToolKindWebSearch {
				changes = compat.AppendUnique(changes, compat.NewOmission(canonical.ResponseItemsKind, canonical.ResponseItemOccurrence(uint32(itemIndex))))
				continue
			}
			if call.Tool().Kind() != canonical.ToolKindFunction {
				return contentDTO{}, nil, canonical.InternalError("GenerateContent cannot project native tool calls")
			}
			object, ok := call.Input().Object()
			if !ok {
				return contentDTO{}, nil, canonical.InternalError("GenerateContent supports object function-call arguments only")
			}
			parts = append(parts, partDTO{FunctionCall: &functionCallDTO{ID: call.CallID().String(), Name: call.Tool().Name(), Args: object.Bytes()}})
			lastEmittedWasMessage = false
		case canonical.ItemKindToolResult:
			result, _ := item.ToolResult()
			if _, web := result.WebSearch(); web {
				changes = compat.AppendUnique(changes, compat.NewOmission(canonical.ResponseItemsKind, canonical.ResponseItemOccurrence(uint32(itemIndex))))
				continue
			}
			return contentDTO{}, nil, canonical.InternalError("GenerateContent cannot project canonical response item " + string(item.Kind()))
		case canonical.ItemKindReasoning:
			if !disclosed || disclosure != canonical.ReasoningDisclosureSummary {
				continue
			}
			reasoning, _ := item.Reasoning()
			summaryCount := 0
			for _, part := range reasoning.Parts() {
				if part.Kind() == canonical.ReasoningPartSummary {
					summaryCount++
				}
			}
			if summaryCount > 1 {
				return contentDTO{}, nil, canonical.InternalError("GenerateContent cannot preserve multiple summaries in one reasoning item")
			}
			for _, part := range reasoning.Parts() {
				if part.Kind() != canonical.ReasoningPartSummary {
					changes = compat.AppendUnique(changes, compat.NewOmission(canonical.ResponseItemsReasoning, canonical.ResponseItemOccurrence(uint32(itemIndex))))
					continue
				}
				text, thought := part.Text(), true
				parts = append(parts, partDTO{Text: &text, Thought: &thought})
				lastEmittedWasMessage = false
			}
		default:
			return contentDTO{}, nil, canonical.InternalError("GenerateContent cannot project canonical response item " + string(item.Kind()))
		}
	}
	return contentDTO{Role: "model", Parts: parts}, changes, nil
}

func finishReason(completion canonical.Completion) (string, error) {
	switch completion.Class() {
	case canonical.CompletionCompleted:
		return "STOP", nil
	case canonical.CompletionIncomplete:
		return "MAX_TOKENS", nil
	case canonical.CompletionDeclined:
		return "SAFETY", nil
	case canonical.CompletionFailed:
		return "", canonical.NewBackendError("", 0, "backend response failed: "+completion.Reason(), "")
	default:
		return "", canonical.InternalError("canonical completion class cannot be projected to GenerateContent")
	}
}

func projectUsage(usage canonical.TokenUsage) (*usageDTO, []compat.Change) {
	input, hasInput := usage.InputTokens()
	output, hasOutput := usage.OutputTokens()
	reasoning, hasReasoning := usage.ReasoningTokens()
	_, hasCacheRead := usage.CacheReadTokens()
	_, hasCacheWrite := usage.CacheWriteTokens()
	var changes []compat.Change
	if hasCacheRead {
		changes = compat.AppendUnique(changes, compat.NewOmission(canonical.ResponseUsageCacheReadTokens, canonical.Occurrence{}))
	}
	if hasCacheWrite {
		changes = compat.AppendUnique(changes, compat.NewOmission(canonical.ResponseUsageCacheWriteTokens, canonical.Occurrence{}))
	}
	if !hasInput && !hasOutput && !hasReasoning {
		return nil, changes
	}
	projected := &usageDTO{}
	if hasInput {
		projected.PromptTokenCount = &input
	}
	if hasInput && hasOutput {
		total := input + output
		projected.TotalTokenCount = &total
	}
	validSplit := hasOutput && hasReasoning && reasoning <= output
	if validSplit {
		candidates := output - reasoning
		projected.CandidatesTokenCount = &candidates
		projected.ThoughtsTokenCount = &reasoning
	}
	if hasOutput && !hasInput && !validSplit {
		changes = compat.AppendUnique(changes, compat.NewOmission(canonical.ResponseUsageOutputTokens, canonical.Occurrence{}))
	}
	if hasReasoning && !validSplit {
		changes = compat.AppendUnique(changes, compat.NewOmission(canonical.ResponseUsageReasoningTokens, canonical.Occurrence{}))
	}
	return projected, changes
}

func appendChanges(base, additions []compat.Change) []compat.Change {
	for _, change := range additions {
		base = compat.AppendUnique(base, change)
	}
	return base
}

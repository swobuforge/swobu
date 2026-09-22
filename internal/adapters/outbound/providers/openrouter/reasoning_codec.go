package openrouter

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
)

// ChatReplayScope owns the exact OpenRouter Chat opaque reasoning replay dialect.
const ChatReplayScope canonical.ProviderChatReplayScope = "openrouter-chat"

func applyOpenRouterReasoning(req canonical.CanonicalRequest, target protocolcodec.ReasoningTargetDialect, changeLog *[]compat.Change, exchangeID string) (map[string]any, error) {
	out := map[string]any{}
	effort, effortSet := req.Controls().Effort.Get()
	if compute, set := req.Reasoning().ComputeField().Get(); set {
		switch compute.Kind() {
		case canonical.ReasoningDisabled:
			if effortSet {
				*changeLog = compat.AppendUnique(*changeLog, compat.NewOmission(canonical.RequestControlsEffort, canonical.Occurrence{}))
				effortSet = false
			}
			if target.ProjectDisabled(changeLog) {
				out["enabled"] = false
			}
		case canonical.ReasoningAutomatic:
			out["enabled"] = true
		case canonical.ReasoningBudget:
			tokens, _ := compute.Tokens()
			out["max_tokens"] = tokens
		default:
			return nil, canonical.BadRequest("reasoning compute is invalid")
		}
	}
	if effortSet {
		out["effort"] = string(target.ProjectEffort(effort, changeLog))
	}
	if disclosure, set := req.Reasoning().DisclosureField().Get(); set {
		// Keep backend capture independent whenever this request may open a tool
		// continuation; client projection enforces disclosure again.
		canContinue, err := canOpenToolContinuation(req)
		if err != nil {
			return nil, err
		}
		if disclosure == canonical.ReasoningDisclosureNone && !canContinue {
			out["exclude"] = true
		}
	}
	if len(out) > 0 {
		return map[string]any{"reasoning": out}, nil
	}
	return nil, nil
}

func canOpenToolContinuation(req canonical.CanonicalRequest) (bool, error) {
	environment, err := canonical.EffectiveTools(req)
	if err != nil {
		return false, err
	}
	if len(environment.Declarations()) == 0 {
		return false, nil
	}
	policy, err := req.EffectiveToolPolicy()
	if err != nil {
		return false, err
	}
	return policy.Mode != canonical.ToolPolicyNone, nil
}

func decorateOpenRouterAttempt(ctx provider.AttemptContext) (protocolcodec.AttemptDecoration, error) {
	if ctx.CacheLocality.IsZero() {
		return protocolcodec.AttemptDecoration{}, nil
	}
	// OpenRouter calls this provider-side sticky-routing primitive session_id.
	// Lowering CacheLocality here does not make it a Swobu conversation/session
	// identity; history and checkpoints remain the continuity authority.
	sum := sha256.Sum256([]byte("openrouter-session:v1\x00" + ctx.CacheLocality.Key()))
	return protocolcodec.AttemptDecoration{
		Fields: map[string]any{"session_id": fmt.Sprintf("swobu_%x", sum)},
	}, nil
}

type openRouterReasoningExtractor struct {
	detailsRaw     []byte
	detailItems    []json.RawMessage
	detailsPresent bool
	detailRunOpen  bool
	flat           strings.Builder
}

func (e *openRouterReasoningExtractor) ExtractBufferedChatReasoning(message map[string]json.RawMessage) (string, error) {
	if raw, ok := message["reasoning_details"]; ok {
		if _, err := e.captureDetails(raw, false); err != nil {
			return "", err
		}
		delete(message, "reasoning_details")
	}
	var flat string
	if raw, ok := message["reasoning"]; ok {
		_ = json.Unmarshal(raw, &flat)
		delete(message, "reasoning")
	}
	return flat, nil
}

func (e *openRouterReasoningExtractor) ExtractStreamedChatReasoning(delta map[string]json.RawMessage) (protocolcodec.ChatReasoningFragment, error) {
	observed := false
	if raw, ok := delta["reasoning_details"]; ok {
		_, err := e.captureDetails(raw, true)
		if err != nil {
			return protocolcodec.ChatReasoningFragment{}, err
		}
		delete(delta, "reasoning_details")
		// Opaque continuation metadata has an independent lifecycle from
		// visible reasoning and may arrive after answer output has begun.
	} else {
		e.detailRunOpen = false
	}
	var text string
	if raw, ok := delta["reasoning"]; ok {
		_ = json.Unmarshal(raw, &text)
		delete(delta, "reasoning")
		if text != "" {
			observed = true
			e.flat.WriteString(text)
		}
	}
	return protocolcodec.ChatReasoningFragment{Text: text, Observed: observed}, nil
}

func (e *openRouterReasoningExtractor) captureDetails(raw json.RawMessage, streamed bool) (bool, error) {
	if !json.Valid(raw) {
		return false, canonical.InternalError("OpenRouter reasoning_details is invalid JSON")
	}
	if trimmed := strings.TrimSpace(string(raw)); len(trimmed) == 0 || trimmed[0] != '[' {
		return false, canonical.InternalError("OpenRouter reasoning_details must be an array")
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return false, canonical.InternalError("OpenRouter reasoning_details must be an array")
	}
	e.detailsPresent = true
	for _, item := range items {
		if !json.Valid(item) {
			return false, canonical.InternalError("OpenRouter reasoning_details contains invalid JSON")
		}
		cloned := append(json.RawMessage(nil), item...)
		if streamed && e.detailRunOpen && e.mergeDetailDelta(cloned) {
			continue
		}
		e.detailItems = append(e.detailItems, cloned)
		e.detailRunOpen = streamed
	}
	if len(e.detailsRaw) == 0 && len(e.detailItems) == len(items) {
		e.detailsRaw = append([]byte(nil), raw...)
		return true, nil
	}
	encoded, err := json.Marshal(e.detailItems)
	if err != nil {
		return false, canonical.InternalError("OpenRouter reasoning_details could not be preserved")
	}
	e.detailsRaw = encoded
	return true, nil
}

// mergeDetailDelta reconstructs the two OpenRouter detail kinds whose text is
// delivered as consecutive stream deltas. All other detail kinds remain
// occurrence-preserving opaque replay units.
func (e *openRouterReasoningExtractor) mergeDetailDelta(next json.RawMessage) bool {
	if len(e.detailItems) == 0 {
		return false
	}
	var previousFields, nextFields map[string]json.RawMessage
	if json.Unmarshal(e.detailItems[len(e.detailItems)-1], &previousFields) != nil || json.Unmarshal(next, &nextFields) != nil {
		return false
	}
	var previousType, nextType string
	_ = json.Unmarshal(previousFields["type"], &previousType)
	_ = json.Unmarshal(nextFields["type"], &nextType)
	field := ""
	switch {
	case previousType == "reasoning.summary" && nextType == previousType:
		field = "summary"
	case previousType == "reasoning.text" && nextType == previousType:
		field = "text"
	default:
		return false
	}
	var previousText, nextText string
	if json.Unmarshal(previousFields[field], &previousText) != nil || json.Unmarshal(nextFields[field], &nextText) != nil {
		return false
	}
	for key, value := range nextFields {
		if key == field || key == "type" {
			continue
		}
		if existing, ok := previousFields[key]; ok && !jsonEqual(existing, value) {
			return false
		}
		previousFields[key] = value
	}
	merged, err := json.Marshal(previousText + nextText)
	if err != nil {
		return false
	}
	previousFields[field] = merged
	encoded, err := json.Marshal(previousFields)
	if err != nil {
		return false
	}
	e.detailItems[len(e.detailItems)-1] = encoded
	return true
}

func jsonEqual(left, right json.RawMessage) bool {
	var leftValue, rightValue any
	return json.Unmarshal(left, &leftValue) == nil && json.Unmarshal(right, &rightValue) == nil && reflect.DeepEqual(leftValue, rightValue)
}

func (e *openRouterReasoningExtractor) NewChatReasoningItem(content string) (canonical.CanonicalItem, error) {
	hasDetails := e.detailsPresent
	var details json.RawMessage
	if hasDetails {
		if len(e.detailsRaw) > 0 {
			details = e.detailsRaw
		} else {
			encoded, err := json.Marshal(e.detailItems)
			if err != nil {
				return canonical.CanonicalItem{}, canonical.InternalError("OpenRouter reasoning_details could not be preserved")
			}
			details = encoded
		}
	}
	flat := content
	if flat == "" {
		flat = e.flat.String()
	}
	return newOpenRouterReasoningItem(details, hasDetails, flat)
}

func (*openRouterReasoningExtractor) NewChatVisibleReasoningItem(content string) (canonical.CanonicalItem, error) {
	return newOpenRouterReasoningItem(nil, false, content)
}

func (*openRouterReasoningExtractor) NewChatLateVisibleReasoningItem(content string) (canonical.CanonicalItem, error) {
	return newOpenRouterReasoningItem(nil, false, content)
}

func (e *openRouterReasoningExtractor) FinalizeChatContinuation() (canonical.CanonicalItem, error) {
	if !e.detailsPresent {
		return canonical.CanonicalItem{}, nil
	}
	if !json.Valid(e.detailsRaw) {
		return canonical.CanonicalItem{}, canonical.InternalError("OpenRouter reasoning_details are invalid")
	}
	opaque, err := canonical.NewProviderChatOpaqueThinking(ChatReplayScope, e.detailsRaw)
	if err != nil {
		return canonical.CanonicalItem{}, err
	}
	return canonical.NewReasoningItem(nil, opaque)
}

func newOpenRouterReasoningItem(details json.RawMessage, hasDetails bool, flat string) (canonical.CanonicalItem, error) {
	parts := make([]canonical.ReasoningPart, 0)
	var opaque canonical.OpaqueThinking
	if hasDetails {
		if !json.Valid(details) {
			return canonical.CanonicalItem{}, canonical.InternalError("OpenRouter reasoning_details are invalid")
		}
		value, err := canonical.NewProviderChatOpaqueThinking(ChatReplayScope, details)
		if err != nil {
			return canonical.CanonicalItem{}, err
		}
		opaque = value
		parts = append(parts, portableOpenRouterParts(details)...)
	}
	if flat != "" && len(parts) == 0 {
		part, err := canonical.NewReasoningPart(canonical.ReasoningPartTrace, flat)
		if err != nil {
			return canonical.CanonicalItem{}, err
		}
		parts = append(parts, part)
	}
	if len(parts) == 0 && opaque.IsZero() {
		return canonical.CanonicalItem{}, nil
	}
	return canonical.NewReasoningItem(parts, opaque)
}

func portableOpenRouterParts(details json.RawMessage) []canonical.ReasoningPart {
	var entries []struct {
		Text    string `json:"text"`
		Summary string `json:"summary"`
	}
	if json.Unmarshal(details, &entries) != nil {
		return nil
	}
	parts := make([]canonical.ReasoningPart, 0, len(entries))
	for _, entry := range entries {
		kind := canonical.ReasoningPartTrace
		text := entry.Text
		if entry.Summary != "" {
			kind, text = canonical.ReasoningPartSummary, entry.Summary
		}
		if text == "" {
			continue
		}
		part, err := canonical.NewReasoningPart(kind, text)
		if err == nil {
			parts = append(parts, part)
		}
	}
	return parts
}

var _ protocolcodec.ChatReasoningExtractor = (*openRouterReasoningExtractor)(nil)

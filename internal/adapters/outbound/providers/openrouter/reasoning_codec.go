package openrouter

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
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
	detailsRaw  []byte
	detailItems []json.RawMessage
	flat        strings.Builder
}

func (e *openRouterReasoningExtractor) ExtractBufferedChatReasoning(message map[string]json.RawMessage) (string, error) {
	if raw, ok := message["reasoning_details"]; ok {
		if err := e.captureDetails(raw); err != nil {
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
		if err := e.captureDetails(raw); err != nil {
			return protocolcodec.ChatReasoningFragment{}, err
		}
		delete(delta, "reasoning_details")
		observed = true
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

func (e *openRouterReasoningExtractor) captureDetails(raw json.RawMessage) error {
	if !json.Valid(raw) {
		return canonical.InternalError("OpenRouter reasoning_details is invalid JSON")
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return canonical.InternalError("OpenRouter reasoning_details must be an array")
	}
	for _, item := range items {
		if !json.Valid(item) {
			return canonical.InternalError("OpenRouter reasoning_details contains invalid JSON")
		}
		e.detailItems = append(e.detailItems, append(json.RawMessage(nil), item...))
	}
	if len(e.detailsRaw) == 0 && len(e.detailItems) == len(items) {
		e.detailsRaw = append([]byte(nil), raw...)
		return nil
	}
	encoded, err := json.Marshal(e.detailItems)
	if err != nil {
		return canonical.InternalError("OpenRouter reasoning_details could not be preserved")
	}
	e.detailsRaw = encoded
	return nil
}

func (e *openRouterReasoningExtractor) NewChatReasoningItem(content string) (canonical.CanonicalItem, error) {
	hasDetails := len(e.detailItems) > 0
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

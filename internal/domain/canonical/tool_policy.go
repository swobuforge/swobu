package canonical

import "strings"

type ToolPolicyMode string

// ToolPolicyMode enumerates the supported tool-intent lowerings.
//
// none is the explicit tool-forbidden mode, auto permits zero or more tool
// calls from the declared surface, required demands at least one tool call,
// and specific forces one exact tool by full ToolID.
//
// ChoiceAllowed remains a reserved canonical extension, not a current mode:
// it would represent an exact allowed ToolID subset plus requiredness, and we
// only surface it once a supported wire family can carry that selection
// constraint losslessly without widening, flattening, or renaming the tool
// surface.
const (
	ToolPolicyNone     ToolPolicyMode = "none"
	ToolPolicyAuto     ToolPolicyMode = "auto"
	ToolPolicyRequired ToolPolicyMode = "required"
	ToolPolicySpecific ToolPolicyMode = "specific"
)

// ParseToolPolicyMode parses one raw tool-policy mode without silently
// defaulting unknown values.
func ParseToolPolicyMode(raw string) (ToolPolicyMode, bool) {
	trimmed := strings.TrimSpace(raw)      // swobu:io-string source=domain
	normalized := strings.ToLower(trimmed) // swobu:io-string source=domain
	if normalized == "none" {
		return ToolPolicyNone, true
	}
	if normalized == "auto" {
		return ToolPolicyAuto, true
	}
	if normalized == "required" {
		return ToolPolicyRequired, true
	}
	if normalized == "specific" {
		return ToolPolicySpecific, true
	}
	return "", false
}

func normalizeToolPolicyMode(mode ToolPolicyMode) ToolPolicyMode {
	if parsed, ok := ParseToolPolicyMode(string(mode)); ok {
		return parsed
	}
	return ToolPolicyNone
}

// ToolPolicy describes what the caller wants the model to do with tools.
//
// Mode carries the explicit semantic choice: none forbids tools, auto allows
// optional tool use, required forces at least one tool call, and specific
// forces one exact tool.
type ToolPolicy struct {
	Mode     ToolPolicyMode
	Specific *ToolKey
}

// NewToolPolicy normalizes one semantic tool policy into canonical form.
func NewToolPolicy(mode ToolPolicyMode, specific *ToolKey) ToolPolicy {
	policy := ToolPolicy{Mode: normalizeToolPolicyMode(mode)}
	if specific != nil {
		id := specific.Clone()
		policy.Mode = ToolPolicySpecific
		policy.Specific = &id
	}
	return policy
}

func (p ToolPolicy) Clone() ToolPolicy {
	return NewToolPolicy(p.Mode, p.Specific)
}

func (p ToolPolicy) IsZero() bool {
	return p.Mode == ToolPolicyNone && p.Specific == nil
}

func (p ToolPolicy) SpecificID() (ToolKey, bool) {
	if p.Specific == nil || p.Specific.IsZero() {
		return ToolKey{}, false
	}
	return p.Specific.Clone(), true
}

func (p ToolPolicy) Validate() error {
	switch p.Mode {
	case ToolPolicyNone, ToolPolicyAuto, ToolPolicyRequired:
		if p.Specific != nil && !p.Specific.IsZero() {
			return BadRequest("tool policy specific tool requires specific mode")
		}
		return nil
	case ToolPolicySpecific:
		if p.Specific == nil || p.Specific.IsZero() {
			return BadRequest("tool policy specific mode requires a tool id")
		}
		return nil
	default:
		return BadRequest("tool policy mode is invalid")
	}
}

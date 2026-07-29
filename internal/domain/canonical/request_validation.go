package canonical

import "fmt"

// ValidateMaterializedRequest validates the complete surviving request after
// session reconstruction. Constructors and wire decoders own individual known
// values; this boundary owns only cross-item and residual invariants over the
// effective request. Native continuation deltas are intentionally not valid
// inputs until their retained history has been materialized.
func ValidateMaterializedRequest(request CanonicalRequest) error {
	items := request.Items()
	if _, _, err := SplitRequestPrelude(items); err != nil {
		return BadRequest(err.Error())
	}
	environment, err := ToolEnvironmentAt(items, len(items))
	if err != nil {
		return BadRequest(err.Error())
	}
	policy, err := request.EffectiveToolPolicy()
	if err != nil {
		return BadRequest(err.Error())
	}
	if err := policy.ValidateForTools(environment.Declarations()); err != nil {
		return BadRequest(err.Error())
	}
	if request.ToolCallBatchSpecified() {
		if err := request.ToolCallBatch().Validate(); err != nil {
			return err
		}
	}
	if request.OutputFormatSpecified() {
		if err := request.OutputFormat().Validate(); err != nil {
			return err
		}
	}

	_, hasContinuation := request.PreviousResponse()
	meaningful := hasContinuation
	var effects ToolEffectMatcher
	for index, item := range items {
		if _, err := effects.Accept(index, item); err != nil {
			return BadRequest(fmt.Sprintf("canonical request item %d has invalid tool-effect correlation: %v", index, err))
		}
		if _, ok := item.Message(); ok {
			// Scope controls placement, not whether the message carries
			// semantics. Constructors already guarantee non-empty content.
			meaningful = true
			continue
		}
		if _, ok := item.ToolCall(); ok {
			meaningful = true
			continue
		}
		if _, ok := item.ToolResult(); ok {
			meaningful = true
			continue
		}
		if _, ok := item.ToolDiscoveryResult(); ok {
			meaningful = true
			continue
		}
		if _, ok := item.Reasoning(); ok {
			meaningful = true
			continue
		}
		if _, ok := item.ToolDeclarations(); !ok {
			return BadRequest(fmt.Sprintf("canonical request item %d is invalid", index))
		}
	}
	if !meaningful {
		return BadRequest("canonical request has no content, effect, or continuation after projection")
	}
	return nil
}

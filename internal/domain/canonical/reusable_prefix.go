package canonical

// ReusablePrefixComparison is an ephemeral comparison result. At most one
// changed occurrence is populated. Occurrences are exchange-local diagnostic
// coordinates and must not be persisted into traffic evidence.
type ReusablePrefixComparison struct {
	Preserved          bool
	InstructionChanged *Occurrence
	ToolChanged        *Occurrence
	InputChanged       *Occurrence
}

type reusableInstruction struct {
	item       CanonicalItem
	occurrence Occurrence
}

type reusableTool struct {
	declaration ToolDeclaration
	scope       ContextScope
	deferred    bool
	occurrence  Occurrence
}

type reusableInput struct {
	item       CanonicalItem
	occurrence Occurrence
}

// CompareReusablePrefix compares explicit model-visible semantic owners in
// lowering order: instructions, tool declarations, then input/history items.
// Object-semantic leaves already carry canonical equality; ordered owners stay
// ordered. Protocol/request metadata is outside these owners and therefore
// cannot affect the comparison.
func CompareReusablePrefix(previous, current CanonicalRequest) ReusablePrefixComparison {
	leftInstructions, leftTools, leftInput := reusablePrefixOwners(previous)
	rightInstructions, rightTools, rightInput := reusablePrefixOwners(current)
	if occurrence, changed := firstInstructionChange(leftInstructions, rightInstructions); changed {
		return ReusablePrefixComparison{InstructionChanged: &occurrence}
	}
	// A newly inserted owner in an earlier lowering band precedes every owner
	// from later bands. It is therefore a divergence when the previous request
	// already had a later owner, even though the earlier band itself grew by an
	// append.
	if len(rightInstructions) > len(leftInstructions) && (len(leftTools) > 0 || len(leftInput) > 0) {
		occurrence := rightInstructions[len(leftInstructions)].occurrence
		return ReusablePrefixComparison{InstructionChanged: &occurrence}
	}
	if occurrence, changed := firstToolChange(leftTools, rightTools); changed {
		return ReusablePrefixComparison{ToolChanged: &occurrence}
	}
	if len(rightTools) > len(leftTools) && len(leftInput) > 0 {
		occurrence := rightTools[len(leftTools)].occurrence
		return ReusablePrefixComparison{ToolChanged: &occurrence}
	}
	if occurrence, changed := firstInputChange(leftInput, rightInput); changed {
		return ReusablePrefixComparison{InputChanged: &occurrence}
	}
	return ReusablePrefixComparison{Preserved: true}
}

func reusablePrefixOwners(request CanonicalRequest) ([]reusableInstruction, []reusableTool, []reusableInput) {
	items := request.Items()
	instructions := make([]reusableInstruction, 0)
	tools := make([]reusableTool, 0)
	input := make([]reusableInput, 0, len(items))
	toolIndex := uint32(0)
	for itemIndex, item := range items {
		occurrence := RequestItemOccurrence(uint32(itemIndex))
		if declarations, ok := item.ToolDeclarations(); ok {
			visibility := declarations.Visibility()
			for _, declaration := range declarations.Tools().Declarations() {
				tools = append(tools, reusableTool{
					declaration: declaration,
					scope:       declarations.Scope(),
					deferred:    visibility.Deferred(declaration.Key()),
					occurrence:  ToolIndexOccurrence(toolIndex),
				})
				toolIndex++
			}
			continue
		}
		if message, ok := item.Message(); ok &&
			(message.Role() == MessageRoleSystem || message.Role() == MessageRoleDeveloper) {
			instructions = append(instructions, reusableInstruction{item: item, occurrence: occurrence})
			continue
		}
		input = append(input, reusableInput{item: item, occurrence: occurrence})
	}
	return instructions, tools, input
}

func firstInstructionChange(left, right []reusableInstruction) (Occurrence, bool) {
	for index := 0; index < len(left); index++ {
		if index >= len(right) || !canonicalItemReusableEqual(left[index].item, right[index].item) {
			return left[index].occurrence, true
		}
	}
	return Occurrence{}, false
}

func firstToolChange(left, right []reusableTool) (Occurrence, bool) {
	for index := 0; index < len(left); index++ {
		if index >= len(right) || left[index].scope != right[index].scope ||
			left[index].deferred != right[index].deferred ||
			!left[index].declaration.Equivalent(right[index].declaration) {
			return left[index].occurrence, true
		}
	}
	return Occurrence{}, false
}

func firstInputChange(left, right []reusableInput) (Occurrence, bool) {
	for index := 0; index < len(left); index++ {
		if index >= len(right) || !canonicalItemReusableEqual(left[index].item, right[index].item) {
			return left[index].occurrence, true
		}
	}
	return Occurrence{}, false
}

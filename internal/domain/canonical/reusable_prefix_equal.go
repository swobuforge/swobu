package canonical

import "bytes"

// canonicalItemReusableEqual compares the explicit semantic branches used as
// model input. It intentionally names each owner instead of letting private Go
// layout, serialization, or hashing define semantic equality.
func canonicalItemReusableEqual(left, right CanonicalItem) bool {
	if left.Kind() != right.Kind() {
		return false
	}
	switch left.Kind() {
	case ItemKindMessage:
		l, _ := left.Message()
		r, _ := right.Message()
		return messageItemReusableEqual(l, r)
	case ItemKindToolCall:
		l, _ := left.ToolCall()
		r, _ := right.ToolCall()
		return toolCallReusableEqual(l, r)
	case ItemKindToolResult:
		l, _ := left.ToolResult()
		r, _ := right.ToolResult()
		return toolResultReusableEqual(l, r)
	case ItemKindToolDiscoveryResult:
		l, _ := left.ToolDiscoveryResult()
		r, _ := right.ToolDiscoveryResult()
		return toolDiscoveryResultReusableEqual(l, r)
	case ItemKindReasoning:
		l, _ := left.Reasoning()
		r, _ := right.Reasoning()
		return reasoningReusableEqual(l, r)
	case ItemKindToolDeclarations:
		return false
	default:
		return false
	}
}

func messageItemReusableEqual(left, right MessageItem) bool {
	if left.Role() != right.Role() || left.Scope() != right.Scope() || len(left.Content()) != len(right.Content()) {
		return false
	}
	leftParts, rightParts := left.Content(), right.Content()
	for index := range leftParts {
		if !messagePartReusableEqual(leftParts[index], rightParts[index]) {
			return false
		}
	}
	return true
}

func messagePartReusableEqual(left, right MessagePart) bool {
	if left.Kind() != right.Kind() {
		return false
	}
	if text, ok := left.Text(); ok {
		other, otherOK := right.Text()
		return otherOK && text.Text() == other.Text() && webCitationsReusableEqual(left.Citations(), right.Citations())
	}
	image, ok := left.Image()
	other, otherOK := right.Image()
	return ok && otherOK && imagePartReusableEqual(image, other)
}

func toolResultPartReusableEqual(left, right ToolResultPart) bool {
	if left.Kind() != right.Kind() {
		return false
	}
	if text, ok := left.Text(); ok {
		other, otherOK := right.Text()
		return otherOK && text.Text() == other.Text()
	}
	image, ok := left.Image()
	other, otherOK := right.Image()
	return ok && otherOK && imagePartReusableEqual(image, other)
}

func imagePartReusableEqual(left, right ImagePart) bool {
	leftDetail, leftDetailSet := left.Detail().Get()
	rightDetail, rightDetailSet := right.Detail().Get()
	if leftDetailSet != rightDetailSet || leftDetailSet && leftDetail != rightDetail {
		return false
	}
	leftSource, rightSource := left.Source(), right.Source()
	if leftURL, ok := leftSource.URL(); ok {
		rightURL, rightOK := rightSource.URL()
		return rightOK && leftURL.String() == rightURL.String()
	}
	leftInline, ok := leftSource.Inline()
	rightInline, rightOK := rightSource.Inline()
	return ok && rightOK && leftInline.MediaType() == rightInline.MediaType() && bytes.Equal(leftInline.Data(), rightInline.Data())
}

func toolCallReusableEqual(left, right ToolCallItem) bool {
	if left.CallID() != right.CallID() || left.Tool() != right.Tool() ||
		left.ResponsesCallIDNull() != right.ResponsesCallIDNull() ||
		!toolInputReusableEqual(left.Input(), right.Input()) {
		return false
	}
	leftExecutor, leftExecutorSet := left.DiscoveryExecutor()
	rightExecutor, rightExecutorSet := right.DiscoveryExecutor()
	if leftExecutorSet != rightExecutorSet || leftExecutorSet && leftExecutor != rightExecutor {
		return false
	}
	leftSearch, leftSearchSet := left.ResponsesWebSearch()
	rightSearch, rightSearchSet := right.ResponsesWebSearch()
	return leftSearchSet == rightSearchSet && (!leftSearchSet || leftSearch.ItemID() == rightSearch.ItemID())
}

func toolInputReusableEqual(left, right ToolInput) bool {
	if object, ok := left.Object(); ok {
		other, otherOK := right.Object()
		return otherOK && object.String() == other.String()
	}
	if text, ok := left.Text(); ok {
		other, otherOK := right.Text()
		return otherOK && text == other
	}
	search, ok := left.WebSearch()
	other, otherOK := right.WebSearch()
	return ok && otherOK && webSearchCallReusableEqual(search, other)
}

func toolResultReusableEqual(left, right ToolResultItem) bool {
	if left.CallID() != right.CallID() || left.IsError() != right.IsError() {
		return false
	}
	leftSearch, leftSearchSet := left.WebSearch()
	rightSearch, rightSearchSet := right.WebSearch()
	if leftSearchSet != rightSearchSet {
		return false
	}
	if leftSearchSet {
		return webSearchResultReusableEqual(leftSearch, rightSearch)
	}
	leftParts, rightParts := left.Content(), right.Content()
	if len(leftParts) != len(rightParts) {
		return false
	}
	for index := range leftParts {
		if !toolResultPartReusableEqual(leftParts[index], rightParts[index]) {
			return false
		}
	}
	return true
}

func toolDiscoveryResultReusableEqual(left, right ToolDiscoveryResultItem) bool {
	if left.CallID() != right.CallID() || left.Executor() != right.Executor() ||
		left.ResponsesCallIDNull() != right.ResponsesCallIDNull() ||
		!toolSetsReusableEqual(left.Tools(), right.Tools()) ||
		!toolVisibilityReusableEqual(left.Visibility(), right.Visibility()) {
		return false
	}
	leftFailure, leftFailed := left.Failure()
	rightFailure, rightFailed := right.Failure()
	if leftFailed != rightFailed {
		return false
	}
	if !leftFailed {
		return true
	}
	leftCode, leftCodeSet := leftFailure.Code().Get()
	rightCode, rightCodeSet := rightFailure.Code().Get()
	return leftCodeSet == rightCodeSet && (!leftCodeSet || leftCode == rightCode) && leftFailure.Message() == rightFailure.Message()
}

func reasoningReusableEqual(left, right ReasoningItem) bool {
	leftParts, rightParts := left.Parts(), right.Parts()
	if len(leftParts) != len(rightParts) {
		return false
	}
	for index := range leftParts {
		if leftParts[index].Kind() != rightParts[index].Kind() || leftParts[index].Text() != rightParts[index].Text() {
			return false
		}
	}
	return opaqueThinkingReusableEqual(left.Opaque(), right.Opaque())
}

func opaqueThinkingReusableEqual(left, right OpaqueThinking) bool {
	if left.kind != right.kind || !bytes.Equal(left.raw, right.raw) ||
		left.providerChatScope != right.providerChatScope || left.responsesItemID != right.responsesItemID {
		return false
	}
	if (left.origin == nil) != (right.origin == nil) {
		return false
	}
	if left.origin != nil {
		if left.origin.targetID != right.origin.targetID || left.origin.targetVersion != right.origin.targetVersion {
			return false
		}
	}
	return true
}

func toolSetsReusableEqual(left, right ToolSet) bool {
	leftTools, rightTools := left.Declarations(), right.Declarations()
	if len(leftTools) != len(rightTools) {
		return false
	}
	for index := range leftTools {
		if !leftTools[index].Equivalent(rightTools[index]) {
			return false
		}
	}
	return true
}

func toolVisibilityReusableEqual(left, right ToolVisibilityRefinements) bool {
	leftKeys, rightKeys := left.DeferredKeys(), right.DeferredKeys()
	if len(leftKeys) != len(rightKeys) {
		return false
	}
	for _, key := range leftKeys {
		if !right.Deferred(key) {
			return false
		}
	}
	return true
}

func webSearchCallReusableEqual(left, right WebSearchCall) bool {
	if left.Action != right.Action || len(left.Queries) != len(right.Queries) || !bytes.Equal(left.interactionsReplay, right.interactionsReplay) {
		return false
	}
	for index := range left.Queries {
		if left.Queries[index] != right.Queries[index] {
			return false
		}
	}
	leftURL, leftURLSet := left.URL.Get()
	rightURL, rightURLSet := right.URL.Get()
	leftMatch, leftMatchSet := left.Match.Get()
	rightMatch, rightMatchSet := right.Match.Get()
	return leftURLSet == rightURLSet && (!leftURLSet || leftURL == rightURL) &&
		leftMatchSet == rightMatchSet && (!leftMatchSet || leftMatch == rightMatch)
}

func webSearchResultReusableEqual(left, right WebSearchResult) bool {
	leftFailure, leftFailed := left.Failure()
	rightFailure, rightFailed := right.Failure()
	if leftFailed != rightFailed || leftFailed && leftFailure != rightFailure || !bytes.Equal(left.interactionsReplay, right.interactionsReplay) {
		return false
	}
	leftSources, rightSources := left.Sources(), right.Sources()
	if len(leftSources) != len(rightSources) {
		return false
	}
	for index := range leftSources {
		if !webSourceReusableEqual(leftSources[index], rightSources[index]) {
			return false
		}
	}
	return true
}

func webCitationsReusableEqual(left, right []WebCitation) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if !webSourceReusableEqual(left[index].Source, right[index].Source) ||
			!specifiedComparableEqual(left[index].Excerpt, right[index].Excerpt) ||
			!specifiedComparableEqual(left[index].Start, right[index].Start) ||
			!specifiedComparableEqual(left[index].End, right[index].End) {
			return false
		}
	}
	return true
}

func webSourceReusableEqual(left, right WebSource) bool {
	return left.URL == right.URL && specifiedComparableEqual(left.Title, right.Title) && bytes.Equal(left.messagesReplay, right.messagesReplay)
}

func specifiedComparableEqual[T comparable](left, right Specified[T]) bool {
	leftValue, leftSet := left.Get()
	rightValue, rightSet := right.Get()
	return leftSet == rightSet && (!leftSet || leftValue == rightValue)
}

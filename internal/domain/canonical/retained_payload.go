package canonical

// This allocation-free traversal exists for Continuity's native-object
// weighting boundary. If the post-M1 byte-container experiment becomes the
// selected implementation, delete it with continuity/weight.go and the heap
// calibration lane rather than preserving a generic sizing API.

import (
	"fmt"
	"math"
)

// RetainedPayloadWeight walks one canonical item without cloning it. Bytes is
// the variable-size payload retained by the item; containers counts the nested
// slices, maps, and branch records whose fixed overhead is owned by the caller's
// storage policy. It deliberately does not estimate Go object headers.
func RetainedPayloadWeight(item CanonicalItem) (bytes, containers uint64, err error) {
	weight := retainedPayloadCounter{}
	if err := weight.item(item); err != nil {
		return 0, 0, err
	}
	return weight.bytes, weight.containers, nil
}

// RetainedRequestMetadataWeight walks variable request metadata without
// cloning controls, stop sequences, tool policy, or structured-output schema.
// Ordered items are measured separately by RetainedPayloadWeight.
func RetainedRequestMetadataWeight(request CanonicalRequest) (bytes, containers uint64, err error) {
	weight := retainedPayloadCounter{}
	if model, ok := request.model.Get(); ok {
		if err := weight.addBytes(len(model)); err != nil {
			return 0, 0, err
		}
	}
	if request.previousResponse != nil {
		ref := request.previousResponse
		if err := weight.addBytes(len(ref.SwobuID)); err != nil {
			return 0, 0, err
		}
		if ref.Responses != nil {
			if err := weight.addBytes(len(ref.Responses.ProviderResponseID), len(ref.Responses.TargetID)); err != nil {
				return 0, 0, err
			}
		}
		if ref.Interactions != nil {
			if err := weight.addBytes(len(ref.Interactions.ProviderInteractionID), len(ref.Interactions.TargetID)); err != nil {
				return 0, 0, err
			}
		}
	}
	if policy, ok := request.toolPolicy.Get(); ok {
		if err := weight.addBytes(len(policy.Mode)); err != nil {
			return 0, 0, err
		}
		if policy.Specific != nil {
			if err := weight.toolKey(*policy.Specific); err != nil {
				return 0, 0, err
			}
		}
	}
	if len(request.controls.Limits.StopSequences) > 0 {
		if err := weight.addContainers(1); err != nil {
			return 0, 0, err
		}
		for _, stop := range request.controls.Limits.StopSequences {
			if err := weight.addBytes(len(stop)); err != nil {
				return 0, 0, err
			}
		}
	}
	if format, ok := request.outputFormat.Get(); ok {
		if err := weight.addBytes(len(format.Kind), len(format.Name), len(format.Description), len(format.Schema.rawObject)); err != nil {
			return 0, 0, err
		}
	}
	return weight.bytes, weight.containers, nil
}

type retainedPayloadCounter struct {
	bytes      uint64
	containers uint64
}

func (w *retainedPayloadCounter) addBytes(values ...int) error {
	for _, value := range values {
		if value < 0 || math.MaxUint64-w.bytes < uint64(value) {
			return fmt.Errorf("canonical retained payload weight overflows uint64")
		}
		w.bytes += uint64(value)
	}
	return nil
}

func (w *retainedPayloadCounter) addContainers(count uint64) error {
	if math.MaxUint64-w.containers < count {
		return fmt.Errorf("canonical retained container count overflows uint64")
	}
	w.containers += count
	return nil
}

func (w *retainedPayloadCounter) item(item CanonicalItem) error {
	switch item.Kind() {
	case ItemKindMessage:
		return w.message(*item.message)
	case ItemKindToolDeclarations:
		if err := w.addContainers(1); err != nil {
			return err
		}
		if err := w.toolSet(item.toolDeclarations.tools); err != nil {
			return err
		}
		return w.visibility(item.toolDeclarations.visibility)
	case ItemKindToolCall:
		call := item.toolCall
		if err := w.addBytes(len(call.callID.value)); err != nil {
			return err
		}
		if err := w.toolKey(call.tool); err != nil {
			return err
		}
		if err := w.toolInput(call.input); err != nil {
			return err
		}
		if call.responsesWebSearch != nil {
			return w.addBytes(len(call.responsesWebSearch.itemID))
		}
		return nil
	case ItemKindToolResult:
		result := item.toolResult
		if err := w.addBytes(len(result.callID.value)); err != nil {
			return err
		}
		if result.content != nil && result.webSearch == nil {
			if err := w.addContainers(1); err != nil {
				return err
			}
			for _, part := range result.content.parts {
				if err := w.toolResultPart(part); err != nil {
					return err
				}
			}
			return nil
		}
		if result.webSearch != nil && result.content == nil {
			return w.webSearchResult(*result.webSearch)
		}
		return fmt.Errorf("unsupported canonical tool result in retained payload weight")
	case ItemKindToolDiscoveryResult:
		result := item.toolDiscoveryResult
		if err := w.addBytes(len(result.callID.value)); err != nil {
			return err
		}
		if result.failure != nil {
			if code, ok := result.failure.code.Get(); ok {
				if err := w.addBytes(len(code)); err != nil {
					return err
				}
			}
			return w.addBytes(len(result.failure.message))
		}
		if err := w.toolSet(result.tools); err != nil {
			return err
		}
		return w.visibility(result.visibility)
	case ItemKindReasoning:
		reasoning := item.reasoning
		if err := w.addContainers(1); err != nil {
			return err
		}
		for _, part := range reasoning.parts {
			if err := w.addBytes(len(part.text)); err != nil {
				return err
			}
		}
		return w.opaque(reasoning.opaque)
	default:
		return fmt.Errorf("unsupported canonical item kind %q in retained payload weight", item.Kind())
	}
}

func (w *retainedPayloadCounter) message(message MessageItem) error {
	if err := w.addContainers(1); err != nil {
		return err
	}
	for _, part := range message.content {
		if err := w.messagePart(part); err != nil {
			return err
		}
	}
	return nil
}

func (w *retainedPayloadCounter) messagePart(part MessagePart) error {
	if err := w.addContainers(1); err != nil {
		return err
	}
	switch part.Kind() {
	case PartKindText:
		if err := w.addBytes(len(part.text.text)); err != nil {
			return err
		}
		if err := w.addContainers(1); err != nil {
			return err
		}
		for _, citation := range part.citations {
			if err := w.webSource(citation.Source); err != nil {
				return err
			}
			if excerpt, ok := citation.Excerpt.Get(); ok {
				if err := w.addBytes(len(excerpt)); err != nil {
					return err
				}
			}
		}
		return nil
	case PartKindImage:
		return w.image(*part.image)
	default:
		return fmt.Errorf("unsupported canonical message part kind %q in retained payload weight", part.Kind())
	}
}

func (w *retainedPayloadCounter) toolResultPart(part ToolResultPart) error {
	if err := w.addContainers(1); err != nil {
		return err
	}
	switch part.Kind() {
	case PartKindText:
		return w.addBytes(len(part.text.text))
	case PartKindImage:
		return w.image(*part.image)
	default:
		return fmt.Errorf("unsupported canonical tool-result part kind %q in retained payload weight", part.Kind())
	}
}

func (w *retainedPayloadCounter) image(image ImagePart) error {
	if image.source.url != nil && image.source.inline == nil {
		return w.addBytes(len(image.source.url.rawURL))
	}
	if image.source.inline != nil && image.source.url == nil {
		return w.addBytes(len(image.source.inline.mediaType), len(image.source.inline.data))
	}
	return fmt.Errorf("unsupported canonical image source in retained payload weight")
}

func (w *retainedPayloadCounter) toolInput(input ToolInput) error {
	if input.object != nil && input.text == nil && input.webSearch == nil {
		return w.addBytes(len(input.object.canonical))
	}
	if input.object == nil && input.text != nil && input.webSearch == nil {
		return w.addBytes(len(*input.text))
	}
	if input.object == nil && input.text == nil && input.webSearch != nil {
		search := input.webSearch
		if err := w.addContainers(1); err != nil {
			return err
		}
		for _, query := range search.Queries {
			if err := w.addBytes(len(query)); err != nil {
				return err
			}
		}
		if value, ok := search.URL.Get(); ok {
			if err := w.addBytes(len(value.rawURL)); err != nil {
				return err
			}
		}
		if value, ok := search.Match.Get(); ok {
			if err := w.addBytes(len(value)); err != nil {
				return err
			}
		}
		return w.addBytes(len(search.interactionsReplay))
	}
	return fmt.Errorf("unsupported canonical tool input in retained payload weight")
}

func (w *retainedPayloadCounter) toolSet(set ToolSet) error {
	if err := w.addContainers(2); err != nil {
		return err
	}
	for key := range set.byKey {
		if err := w.addBytes(len(key)); err != nil {
			return err
		}
	}
	for _, declaration := range set.ordered {
		if err := w.toolDeclaration(declaration); err != nil {
			return err
		}
	}
	return nil
}

func (w *retainedPayloadCounter) toolDeclaration(declaration ToolDeclaration) error {
	if err := w.addContainers(1); err != nil {
		return err
	}
	switch declaration.Kind() {
	case ToolKindFunction:
		tool := declaration.function
		if err := w.toolKey(tool.key); err != nil {
			return err
		}
		return w.addBytes(len(tool.description), jsonObjectLength(tool.inputSchema.object))
	case ToolKindCustom:
		tool := declaration.custom
		if err := w.toolKey(tool.key); err != nil {
			return err
		}
		return w.addBytes(len(tool.description), jsonObjectLength(tool.format.object))
	case ToolKindNamespace:
		tool := declaration.namespace
		if err := w.toolKey(tool.key); err != nil {
			return err
		}
		if err := w.addBytes(len(tool.description)); err != nil {
			return err
		}
		return w.toolDeclarations(tool.tools)
	case ToolKindDiscovery:
		tool := declaration.discovery
		return w.addBytes(len(tool.description), jsonObjectLength(tool.inputSchema.object))
	case ToolKindMCP:
		tool := declaration.mcp
		if err := w.toolKey(tool.key); err != nil {
			return err
		}
		if err := w.addBytes(len(tool.description)); err != nil {
			return err
		}
		if err := w.mcpSource(tool.source); err != nil {
			return err
		}
		return w.toolDeclarations(tool.tools)
	case ToolKindWebSearch:
		return nil
	default:
		return fmt.Errorf("unsupported canonical tool kind %q in retained payload weight", declaration.Kind())
	}
}

func (w *retainedPayloadCounter) toolDeclarations(declarations []ToolDeclaration) error {
	if err := w.addContainers(1); err != nil {
		return err
	}
	for _, declaration := range declarations {
		if err := w.toolDeclaration(declaration); err != nil {
			return err
		}
	}
	return nil
}

func (w *retainedPayloadCounter) mcpSource(source MCPSource) error {
	if err := w.addBytes(len(source.reference)); err != nil {
		return err
	}
	if err := w.specifiedStrings(source.allowedTools); err != nil {
		return err
	}
	if err := w.specifiedStrings(source.allowedCallers); err != nil {
		return err
	}
	for _, filter := range []*MCPToolFilter{source.approval.always, source.approval.never} {
		if filter != nil {
			if err := w.specifiedStrings(filter.toolNames); err != nil {
				return err
			}
		}
	}
	return nil
}

func (w *retainedPayloadCounter) specifiedStrings(values Specified[[]string]) error {
	items, ok := values.Get()
	if !ok {
		return nil
	}
	if err := w.addContainers(1); err != nil {
		return err
	}
	for _, item := range items {
		if err := w.addBytes(len(item)); err != nil {
			return err
		}
	}
	return nil
}

func (w *retainedPayloadCounter) visibility(visibility ToolVisibilityRefinements) error {
	if len(visibility.deferred) == 0 {
		return nil
	}
	if err := w.addContainers(1); err != nil {
		return err
	}
	for key := range visibility.deferred {
		if err := w.toolKey(key); err != nil {
			return err
		}
	}
	return nil
}

func (w *retainedPayloadCounter) opaque(opaque OpaqueThinking) error {
	if err := w.addBytes(len(opaque.raw), len(opaque.providerChatScope), len(opaque.responsesItemID)); err != nil {
		return err
	}
	if opaque.origin != nil {
		return w.addBytes(len(opaque.origin.targetID))
	}
	return nil
}

func (w *retainedPayloadCounter) webSearchResult(result WebSearchResult) error {
	if err := w.addContainers(1); err != nil {
		return err
	}
	if result.failure != nil {
		if err := w.addBytes(len(*result.failure)); err != nil {
			return err
		}
	}
	for _, source := range result.sources {
		if err := w.webSource(source); err != nil {
			return err
		}
	}
	return w.addBytes(len(result.interactionsReplay))
}

func (w *retainedPayloadCounter) webSource(source WebSource) error {
	if err := w.addContainers(1); err != nil {
		return err
	}
	if err := w.addBytes(len(source.URL.rawURL), len(source.messagesReplay)); err != nil {
		return err
	}
	if title, ok := source.Title.Get(); ok {
		return w.addBytes(len(title))
	}
	return nil
}

func (w *retainedPayloadCounter) toolKey(key ToolKey) error {
	return w.addBytes(len(key.namespace), len(key.kind), len(key.name))
}

func jsonObjectLength(object *JSONObject) int {
	if object == nil {
		return 0
	}
	return len(object.canonical)
}

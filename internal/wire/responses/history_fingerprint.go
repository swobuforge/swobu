package responses

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/historyfingerprint"
	"github.com/swobuforge/swobu/internal/mcp"
	"github.com/swobuforge/swobu/internal/wire"
)

const fingerprintScheme historyfingerprint.Scheme = "responses"

// responsesHistoryItemDTO is the private superset of input/output item fields
// needed to reproduce the ordered output sequence a client appends to input.
type responsesHistoryItemDTO struct {
	Type             string                         `json:"type"`
	ID               string                         `json:"id,omitempty"`
	Status           string                         `json:"status,omitempty"`
	Role             string                         `json:"role,omitempty"`
	Content          json.RawMessage                `json:"content,omitempty"`
	CallID           string                         `json:"call_id,omitempty"`
	Name             string                         `json:"name,omitempty"`
	Namespace        string                         `json:"namespace,omitempty"`
	Arguments        json.RawMessage                `json:"arguments,omitempty"`
	Input            string                         `json:"input,omitempty"`
	Output           json.RawMessage                `json:"output,omitempty"`
	ServerLabel      string                         `json:"server_label,omitempty"`
	Summary          []responsesReasoningSummaryDTO `json:"summary,omitempty"`
	EncryptedContent string                         `json:"encrypted_content,omitempty"`
	Action           json.RawMessage                `json:"action,omitempty"`
	Tools            json.RawMessage                `json:"tools,omitempty"`
	Execution        string                         `json:"execution,omitempty"`
}

type responsesHistoryResult struct {
	previous *historyfingerprint.History
	request  historyfingerprint.Request
	current  json.RawMessage
}

func fingerprintResponsesHistory(input json.RawMessage, explicitPrevious bool, retainLitePrelude bool) (responsesHistoryResult, error) {
	trimmed := bytes.TrimSpace(input)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		request, err := fingerprintResponsesRequestValue(nil)
		return responsesHistoryResult{request: request, current: json.RawMessage("null")}, err
	}
	if trimmed[0] != '[' {
		var text string
		if err := json.Unmarshal(trimmed, &text); err != nil {
			return responsesHistoryResult{}, err
		}
		content, err := json.Marshal([]struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}{{Type: "input_text", Text: text}})
		if err != nil {
			return responsesHistoryResult{}, err
		}
		request, err := fingerprintResponsesRequestValue([]responsesHistoryItemDTO{{
			Type: "message", Role: "user", Content: content,
		}})
		return responsesHistoryResult{request: request, current: append(json.RawMessage(nil), trimmed...)}, err
	}
	var items []responsesHistoryItemDTO
	if err := json.Unmarshal(trimmed, &items); err != nil {
		return responsesHistoryResult{}, err
	}
	var rawItems []json.RawMessage
	if err := json.Unmarshal(trimmed, &rawItems); err != nil || len(rawItems) != len(items) {
		return responsesHistoryResult{}, errors.New("responses history input items are invalid")
	}
	if explicitPrevious {
		request, err := fingerprintResponsesRequestValue(items)
		return responsesHistoryResult{request: request, current: append(json.RawMessage(nil), trimmed...)}, err
	}

	var previous *historyfingerprint.History
	requestStart := 0
	preambleEnd := 0
	if len(items) > 0 && strings.TrimSpace(items[0].Type) == "additional_tools" { // swobu:io-string source=boundary
		preambleEnd = 1
	}
	for index := 0; index < len(items); {
		if !isResponsesHistoryOutput(items[index]) {
			index++
			continue
		}
		responseEnd := index + 1
		for responseEnd < len(items) && isResponsesHistoryOutput(items[responseEnd]) {
			responseEnd++
		}
		// A terminal assistant/output value may be current prefill. Later request
		// input is the evidence that closes this response contribution.
		if responseEnd == len(items) {
			break
		}
		request, err := fingerprintResponsesRequestValue(items[requestStart:index])
		if err != nil {
			return responsesHistoryResult{}, err
		}
		response, err := fingerprintResponsesResponseValue(items[index:responseEnd])
		if err != nil {
			return responsesHistoryResult{}, err
		}
		history, err := historyfingerprint.Advance(previous, request, response)
		if err != nil {
			return responsesHistoryResult{}, err
		}
		previous = &history
		requestStart = responseEnd
		index = responseEnd
	}
	currentItems := items[requestStart:]
	currentRawItems := rawItems[requestStart:]
	if retainLitePrelude && preambleEnd > 0 && requestStart >= preambleEnd {
		currentItems = append(append([]responsesHistoryItemDTO(nil), items[:preambleEnd]...), currentItems...)
		currentRawItems = append(append([]json.RawMessage(nil), rawItems[:preambleEnd]...), currentRawItems...)
	}
	current, err := fingerprintResponsesRequestValue(currentItems)
	if err != nil {
		return responsesHistoryResult{}, err
	}
	// Rebase identity through normalized DTOs, but retain current native input
	// from the original objects so unknown fields, nulls, and large numbers are
	// not destroyed by fingerprint morphology.
	currentRaw, err := json.Marshal(currentRawItems)
	if err != nil {
		return responsesHistoryResult{}, err
	}
	return responsesHistoryResult{previous: previous, request: current, current: currentRaw}, nil
}

func isResponsesHistoryOutput(item responsesHistoryItemDTO) bool {
	typeName := strings.TrimSpace(item.Type) // swobu:io-string source=boundary
	if typeName == "message" {
		return strings.TrimSpace(item.Role) == "assistant" // swobu:io-string source=boundary
	}
	if typeName == "tool_search_output" {
		return strings.TrimSpace(item.Execution) == "server"
	}
	// swobu:lint ignore string-switch because=protocol boundary partitions Responses history item variants.
	switch typeName {
	case "function_call", "custom_tool_call", "tool_search_call", "web_search_call", "reasoning", "program", "program_output", "compaction":
		return true
	default:
		return false
	}
}

func fingerprintResponsesRequestValue(items []responsesHistoryItemDTO) (historyfingerprint.Request, error) {
	normalized, err := normalizeResponsesHistoryItems(items)
	if err != nil {
		return historyfingerprint.Request{}, err
	}
	raw, err := json.Marshal(normalized)
	if err != nil {
		return historyfingerprint.Request{}, err
	}
	return historyfingerprint.FingerprintRequest(fingerprintScheme, raw)
}

func fingerprintResponsesResponseValue(items []responsesHistoryItemDTO) (historyfingerprint.Response, error) {
	normalized, err := normalizeResponsesHistoryItems(items)
	if err != nil {
		return historyfingerprint.Response{}, err
	}
	raw, err := json.Marshal(normalized)
	if err != nil {
		return historyfingerprint.Response{}, err
	}
	return historyfingerprint.FingerprintResponse(fingerprintScheme, raw)
}

func normalizeResponsesHistoryItems(items []responsesHistoryItemDTO) ([]responsesHistoryItemDTO, error) {
	normalized := make([]responsesHistoryItemDTO, 0, len(items))
	for index := range items {
		if !admittedResponsesHistoryItem(items[index]) {
			continue
		}
		item := items[index]
		if strings.TrimSpace(item.Type) != "web_search_call" { // swobu:io-string source=boundary
			// Presentation IDs and generic wire statuses have no history
			// consumer. Web search is the exception because its ID is the
			// semantic call/result correlation and status selects lifecycle.
			item.ID = ""
			item.Status = ""
		} else if strings.TrimSpace(item.Status) == "searching" { // swobu:io-string source=boundary
			item.Status = "in_progress"
		}
		var err error
		item.Content, err = normalizeResponsesRawJSON(items[index].Content)
		if err != nil {
			return nil, err
		}
		if item.Type == "function_call" && len(items[index].Arguments) > 0 {
			arguments, err := decodeResponsesFunctionCallArguments(items[index].Arguments)
			if err != nil {
				return nil, err
			}
			item.Arguments, err = json.Marshal(arguments.String())
		} else {
			item.Arguments, err = normalizeResponsesRawJSON(items[index].Arguments)
		}
		if err != nil {
			return nil, err
		}
		item.Output, err = normalizeResponsesRawJSON(items[index].Output)
		if err != nil {
			return nil, err
		}
		item.Action, err = normalizeResponsesRawJSON(items[index].Action)
		if err != nil {
			return nil, err
		}
		item.Tools, err = normalizeResponsesHistoryTools(items[index].Tools)
		if err != nil {
			return nil, err
		}
		normalized = append(normalized, item)
	}
	return normalized, nil
}

func normalizeResponsesHistoryTools(source json.RawMessage) (json.RawMessage, error) {
	if len(bytes.TrimSpace(source)) == 0 {
		return nil, nil
	}
	var tools []responsesToolDefinitionDTO
	if err := json.Unmarshal(source, &tools); err != nil {
		return normalizeResponsesRawJSON(source)
	}
	normalized := make([]responsesToolDefinitionDTO, 0, len(tools))
	for index := range tools {
		if !strings.EqualFold(strings.TrimSpace(tools[index].Type), "mcp") {
			normalized = append(normalized, tools[index])
			continue
		}
		if _, err := decodeResponsesMCPNamespace(tools[index], mcp.Access{}); err != nil {
			return nil, err
		}
		tools[index].Headers = nil
		tools[index].Authorization = nil
		normalized = append(normalized, tools[index])
	}
	sanitized, err := json.Marshal(normalized)
	if err != nil {
		return nil, err
	}
	return normalizeResponsesRawJSON(sanitized)
}

func admittedResponsesHistoryItem(item responsesHistoryItemDTO) bool {
	switch strings.TrimSpace(item.Type) { // swobu:io-string source=boundary
	case "message", "additional_tools", "function_call", "custom_tool_call", "function_call_output", "custom_tool_call_output", "tool_search_call", "tool_search_output", "reasoning":
		return true
	case "web_search_call":
		action := bytes.TrimSpace(item.Action)
		return len(action) > 0 && !bytes.Equal(action, []byte("null"))
	default:
		return false
	}
}

func normalizeResponsesRawJSON(source json.RawMessage) (json.RawMessage, error) {
	if len(source) == 0 {
		return nil, nil
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(source))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

func fingerprintResponsesResponse(request canonical.CanonicalRequest, output canonical.CanonicalResponse) (historyfingerprint.Response, error) {
	state := responsesResponseHistoryState{completion: output.Completion()}
	for ordinal, item := range output.Items() {
		if err := state.appendItem(request, ordinal, item); err != nil {
			return historyfingerprint.Response{}, err
		}
	}
	return state.fingerprint()
}

// responsesResponseHistoryState owns only the appendable private output-item
// representation. It deliberately models the currently supported output
// subset; future reasoning items require an explicit grammar extension.
type responsesResponseHistoryState struct {
	items      []responsesHistoryItemDTO
	completion canonical.Completion
}

func (s *responsesResponseHistoryState) appendItem(request canonical.CanonicalRequest, ordinal int, item canonical.CanonicalItem) error {
	switch item.Kind() {
	case canonical.ItemKindMessage:
		message, _ := item.Message()
		if message.Role() != canonical.MessageRoleAssistant {
			return canonical.InternalError("canonical response contains a non-assistant Responses output message")
		}
		content := make([]responsesOutputTextItemDTO, 0, len(message.Content()))
		for _, part := range message.Content() {
			text, ok := part.Text()
			if !ok {
				return canonical.NewBackendError(
					"responses",
					0,
					"Responses client output cannot represent the backend image response",
					"",
				)
			}
			annotations, err := encodeResponsesAnnotations(text.Text(), part.Citations())
			if err != nil {
				return err
			}
			content = append(content, responsesOutputTextItemDTO{Type: "output_text", Text: text.Text(), Annotations: annotations})
		}
		raw, err := json.Marshal(content)
		if err != nil {
			return err
		}
		history := responsesHistoryItemDTO{Type: "message", Role: "assistant", Content: raw}
		s.items = append(s.items, history)
		return nil
	case canonical.ItemKindToolCall:
		call, _ := item.ToolCall()
		tool := call.Tool()
		history := responsesHistoryItemDTO{
			ID: call.CallID().String(), CallID: call.CallID().String(), Name: tool.Name(),
			Namespace: responsesClientNamespace(tool),
		}
		switch tool.Kind() {
		case canonical.ToolKindFunction:
			object, ok := call.Input().Object()
			if !ok {
				return canonical.InternalError("responses output function call requires object input")
			}
			history.Type = "function_call"
			arguments, err := json.Marshal(object.String())
			if err != nil {
				return err
			}
			history.Arguments = arguments
		case canonical.ToolKindCustom:
			input, ok := call.Input().Text()
			if !ok {
				return canonical.InternalError("responses output custom call requires text input")
			}
			history.Type = "custom_tool_call"
			history.Input = input
		case canonical.ToolKindWebSearch:
			input, ok := call.Input().WebSearch()
			if !ok {
				return canonical.InternalError("responses output web-search call requires typed input")
			}
			history.Type = "web_search_call"
			history.CallID = ""
			history.Name = ""
			action, err := encodeResponsesWebSearchAction(input)
			if err != nil {
				return err
			}
			history.Action = action
		case canonical.ToolKindDiscovery:
			object, ok := call.Input().Object()
			if !ok {
				return canonical.InternalError("responses output discovery call requires object input")
			}
			executor, ok := call.DiscoveryExecutor()
			if !ok {
				return canonical.InternalError("responses output discovery call lost execution ownership")
			}
			history.Type = "tool_search_call"
			history.Name = ""
			history.Execution = "client"
			if executor == canonical.DiscoveryExecutorProvider {
				history.Execution = "server"
			}
			history.Arguments = json.RawMessage(object.String())
		default:
			return canonical.InternalError("Responses history received an unknown canonical tool-call kind")
		}
		s.items = append(s.items, history)
		return nil
	case canonical.ItemKindToolResult:
		result, _ := item.ToolResult()
		search, ok := result.WebSearch()
		if !ok {
			return canonical.InternalError("canonical Responses output contains a request-only content tool result")
		}
		for index := len(s.items) - 1; index >= 0; index-- {
			if s.items[index].Type != "web_search_call" || s.items[index].ID != result.CallID().String() {
				continue
			}
			action, err := encodeResponsesWebSearchSources(search, s.items[index].Action)
			if err != nil {
				return err
			}
			s.items[index].Action = action
			return nil
		}
		return canonical.InternalError("responses web-search result has no prior call")
	case canonical.ItemKindToolDiscoveryResult:
		result, _ := item.ToolDiscoveryResult()
		wireTools, err := encodeResponsesTools(result.Tools().Declarations(), nil, mcp.Access{}, nil, "")
		if err != nil {
			return err
		}
		rawTools, err := json.Marshal(wireTools)
		if err != nil {
			return err
		}
		execution := "client"
		if result.Executor() == canonical.DiscoveryExecutorProvider {
			execution = "server"
		}
		history := responsesHistoryItemDTO{
			Type: "tool_search_output", CallID: result.CallID().String(),
			Execution: execution, Tools: rawTools,
		}
		if result.ResponsesCallIDNull() {
			history.CallID = ""
		}
		s.items = append(s.items, history)
		return nil
	case canonical.ItemKindReasoning:
		reasoning, _ := item.Reasoning()
		disclosure, disclosureSet := request.Reasoning().DisclosureField().Get()
		history := responsesHistoryItemDTO{Type: "reasoning"}
		// This presentation identity is client-local and never enters provider
		// replay because P0 supports only native ResponseRef continuation.
		history.ID = fmt.Sprintf("rs_swobu_%d", ordinal)
		history.Status = "completed"
		if replay, ok := reasoning.Opaque().Responses(); ok {
			history.EncryptedContent = replay.EncryptedContent
		}
		for _, part := range reasoning.Parts() {
			if disclosureSet && disclosure == canonical.ReasoningDisclosureNone {
				continue
			}
			if part.Kind() != canonical.ReasoningPartSummary {
				continue
			}
			history.Summary = append(history.Summary, responsesReasoningSummaryDTO{Type: "summary_text", Text: part.Text()})
		}
		s.items = append(s.items, history)
		return nil
	default:
		return canonical.InternalError("Responses history received a request-only canonical item kind")
	}
}

func (s *responsesResponseHistoryState) fingerprint() (historyfingerprint.Response, error) {
	status, _ := responsesWireStatusForCompletion(s.completion)
	items := append([]responsesHistoryItemDTO(nil), s.items...)
	for index := range items {
		if items[index].Status == "" {
			items[index].Status = status
		}
	}
	return fingerprintResponsesResponseValue(items)
}

// responsesFingerprintingEncoder marks completion in the same Encode call
// that returns response.completed. Byte and message delivery wrappers gate
// that exact terminal output on checkpoint commit.
func responsesFingerprintingEncoder(request canonical.CanonicalRequest, encode wire.ResponseEventEncoder, complete func(*historyfingerprint.Response), fail func(error)) wire.ResponseEventEncoder {
	state := &responsesResponseHistoryState{}
	return func(event canonical.Event) ([][]byte, error) {
		encoded, err := encode(event)
		if err != nil {
			fail(err)
			return nil, err
		}
		switch event.Kind {
		case canonical.EventItemCompleted:
			itemEvent, ok := event.Payload.(canonical.ItemEvent)
			completed, valid := itemEvent.Payload.(canonical.ItemCompletedPayload)
			if !ok || !valid {
				err := errors.New("responses item completion payload is invalid")
				fail(err)
				return nil, err
			}
			if err := state.appendItem(request, int(itemEvent.Position.Item), completed.Item); err != nil {
				fail(err)
				return nil, err
			}
		case canonical.EventFinish:
			if payload, ok := event.Payload.(canonical.FinishPayload); ok {
				state.completion = payload.Completion
			}
		}
		status, terminal := responsesFingerprintTerminalStatus(event)
		if !terminal {
			return encoded, nil
		}
		if status != canonical.EnvelopeStatusCompleted {
			fail(errors.New("responses response did not complete successfully"))
			return encoded, nil
		}
		fingerprint, err := state.fingerprint()
		if err != nil {
			fail(err)
			return encoded, nil
		}
		complete(&fingerprint)
		return encoded, nil
	}
}

func responsesFingerprintTerminalStatus(event canonical.Event) (canonical.EnvelopeStatus, bool) {
	if event.Kind != canonical.EventEnvelopeEnd {
		return "", false
	}
	payload, ok := event.Payload.(canonical.EnvelopeEndPayload)
	if !ok || payload.Kind != canonical.EnvResponse {
		return "", false
	}
	return payload.Status, true
}

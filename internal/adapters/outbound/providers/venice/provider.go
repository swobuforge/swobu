package venice

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"

	modelcatalogopenai "github.com/swobuforge/swobu/internal/adapters/outbound/modelcatalog/openai"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/openaifamily"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
	core "github.com/swobuforge/swobu/internal/wire/primitives"
)

type modelSupportStore struct {
	mu          sync.RWMutex
	byModel     map[string]provider.TargetSupport
	searchModel map[string]provider.Support
}

func NewRuntime(client *http.Client, credentials providersruntime.CredentialProvider) providersruntime.ProviderRuntimeBundle {
	store := &modelSupportStore{byModel: make(map[string]provider.TargetSupport), searchModel: make(map[string]provider.Support)}
	policy := openaifamily.StandardBearerPolicy(profile.ProviderSpecVenice).
		WithModelCatalogQuery(func(values url.Values) { values.Set("type", "text") }).
		WithModelCatalogProject(store.projectModel)
	bundle := openaifamily.NewRuntime(client, credentials, policy)
	bundle.BackendResolver = backendResolver{standard: bundle.BackendResolver, support: store}
	bundle.TargetSupport = store
	return bundle
}

func (s *modelSupportStore) projectModel(providerID profile.ProviderID, row modelcatalogopenai.ModelRow) (profile.ModelAuthoringOption, bool, error) {
	var model struct {
		Type      string `json:"type"`
		OwnedBy   string `json:"owned_by"`
		ModelSpec struct {
			Name         string `json:"name"`
			Capabilities struct {
				FunctionCalling *bool `json:"supportsFunctionCalling"`
				WebSearch       *bool `json:"supportsWebSearch"`
				Reasoning       *bool `json:"supportsReasoning"`
				ReasoningEffort *bool `json:"supportsReasoningEffort"`
				ResponseSchema  *bool `json:"supportsResponseSchema"`
				Vision          *bool `json:"supportsVision"`
			} `json:"capabilities"`
		} `json:"model_spec"`
	}
	if err := json.Unmarshal(row.RawJSON(), &model); err != nil {
		return profile.ModelAuthoringOption{}, false, err
	}
	if model.Type != "" && !strings.EqualFold(strings.TrimSpace(model.Type), "text") {
		return profile.ModelAuthoringOption{}, false, nil
	}
	values := make(map[canonical.CapabilityPath]provider.Support)
	addSupportValue(values, canonical.RequestTools, combinedToolSupport(model.ModelSpec.Capabilities.FunctionCalling, model.ModelSpec.Capabilities.WebSearch))
	addSupport(values, canonical.RequestReasoning, model.ModelSpec.Capabilities.Reasoning)
	addSupport(values, canonical.RequestControlsEffort, model.ModelSpec.Capabilities.ReasoningEffort)
	addSupport(values, canonical.RequestOutputFormatSchema, model.ModelSpec.Capabilities.ResponseSchema)
	addSupport(values, canonical.RequestItemsMessageImage, model.ModelSpec.Capabilities.Vision)
	support := provider.NewTargetSupport(values)
	s.mu.Lock()
	if s.byModel == nil {
		s.byModel = make(map[string]provider.TargetSupport)
	}
	if s.searchModel == nil {
		s.searchModel = make(map[string]provider.Support)
	}
	s.byModel[row.ID()] = support
	s.searchModel[row.ID()] = boolSupport(model.ModelSpec.Capabilities.WebSearch)
	s.mu.Unlock()
	name := strings.TrimSpace(model.ModelSpec.Name)
	if name == "" {
		name = row.ID()
	}
	publisher := strings.TrimSpace(model.OwnedBy)
	if publisher == "" {
		publisher = string(providerID)
	}
	return profile.NewModelAuthoringOption(row.ID(), name, publisher, "", "", []string{"chat_completions", "chat_completions_stream"}, "chat_completions"), true, nil
}

func combinedToolSupport(functionCalling, webSearch *bool) provider.Support {
	functionSupport := boolSupport(functionCalling)
	searchSupport := boolSupport(webSearch)
	if functionSupport == provider.SupportSupported || searchSupport == provider.SupportSupported {
		return provider.SupportSupported
	}
	if functionSupport == provider.SupportUnsupported && searchSupport == provider.SupportUnsupported {
		return provider.SupportUnsupported
	}
	return provider.SupportUnknown
}

func boolSupport(value *bool) provider.Support {
	if value == nil {
		return provider.SupportUnknown
	}
	if *value {
		return provider.SupportSupported
	}
	return provider.SupportUnsupported
}

func addSupportValue(values map[canonical.CapabilityPath]provider.Support, capability canonical.CapabilityPath, support provider.Support) {
	if support != provider.SupportUnknown {
		values[capability] = support
	}
}

func addSupport(values map[canonical.CapabilityPath]provider.Support, capability canonical.CapabilityPath, value *bool) {
	if value == nil {
		return
	}
	if *value {
		values[capability] = provider.SupportSupported
		return
	}
	values[capability] = provider.SupportUnsupported
}

func (s *modelSupportStore) ResolveTargetSupport(target provider.TargetSnapshot) provider.TargetSupport {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.byModel[target.Model]
}

func (s *modelSupportStore) resolveWebSearch(model string) provider.Support {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.searchModel[model]
}

type backendResolver struct {
	standard provider.BackendResolver
	support  *modelSupportStore
}

func (r backendResolver) ResolveBackend(target provider.TargetSnapshot) (provider.Backend, error) {
	backend, err := r.standard.ResolveBackend(target)
	if err != nil {
		return provider.Backend{}, err
	}
	if target.ProtocolKind != protocolkind.ChatCompletions {
		return provider.Backend{}, fmt.Errorf("Venice backend protocol %q is not Chat Completions", target.ProtocolKind)
	}
	standard, ok := backend.Codec.(protocolcodec.Codec)
	if !ok {
		return provider.Backend{}, fmt.Errorf("Venice Chat backend has codec %T, want protocolcodec.Codec", backend.Codec)
	}
	standard.ChatDialect.LowerTool = protocolcodec.ChatOutOfBandHostedSearch()
	standard.ChatDialect.LowerToolPolicy = protocolcodec.ChatOutOfBandHostedSearchPolicy()
	backend.Codec = codec{standard: standard, webSearchSupport: r.support.resolveWebSearch(target.Model)}
	return backend, backend.Validate()
}

type codec struct {
	standard         protocolcodec.Codec
	webSearchSupport provider.Support
}

func (c codec) Encode(req provider.Request) (carrier.Document, []compat.Change, error) {
	searchMode, searchEnabled, err := webSearchMode(req.Canonical)
	if err != nil {
		return carrier.Document{}, nil, err
	}
	if searchEnabled && c.webSearchSupport == provider.SupportUnsupported {
		return carrier.Document{}, nil, provider.IncompatibleCapability(canonical.RequestToolsKind, canonical.ToolOccurrence(canonical.NewWebSearchDeclaration().Key()), "Venice model does not support native web search")
	}
	document, changes, err := c.standard.Encode(req)
	if err != nil || !searchEnabled {
		return document, changes, err
	}
	var payload map[string]any
	if err := json.Unmarshal(document.RawBytes(), &payload); err != nil {
		return carrier.Document{}, changes, canonical.InternalError("Venice Chat request is invalid JSON")
	}
	payload["venice_parameters"] = map[string]any{
		"enable_web_search":    searchMode,
		"enable_web_citations": true,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return carrier.Document{}, changes, canonical.InternalError("Venice Chat request could not be encoded")
	}
	return carrier.NewDocument(document.Family, document.Media, document.Header, raw, document.Meta), changes, nil
}

func webSearchMode(request canonical.CanonicalRequest) (string, bool, error) {
	tools, err := canonical.EffectiveTools(request)
	if err != nil {
		return "", false, err
	}
	declarations := tools.Declarations()
	searchCount := 0
	for _, declaration := range declarations {
		if declaration.Kind() == canonical.ToolKindWebSearch {
			searchCount++
		}
	}
	if searchCount == 0 {
		return "", false, nil
	}
	policy, err := request.EffectiveToolPolicy()
	if err != nil {
		return "", false, err
	}
	switch policy.Mode {
	case canonical.ToolPolicyNone:
		return "", false, nil
	case canonical.ToolPolicyAuto:
		return "auto", true, nil
	case canonical.ToolPolicyRequired:
		if len(declarations) == searchCount {
			return "on", true, nil
		}
		return "", false, provider.IncompatibleCapability(canonical.RequestToolPolicy, canonical.ToolOccurrence(canonical.NewWebSearchDeclaration().Key()), "Venice cannot require one tool across mixed native-search and client-executed declarations")
	case canonical.ToolPolicySpecific:
		key, ok := policy.SpecificID()
		if ok && key.Kind() == canonical.ToolKindWebSearch {
			if len(declarations) != searchCount {
				return "", false, provider.IncompatibleCapability(canonical.RequestToolPolicy, canonical.ToolOccurrence(key), "Venice cannot force native web search while client-executed declarations remain available")
			}
			return "on", true, nil
		}
		return "", false, nil
	default:
		return "", false, canonical.BadRequest("web-search tool policy is invalid")
	}
}

func (c codec) Decode(ctx context.Context, req provider.Request, ingress provider.Ingress) (provider.DecodedResponse, error) {
	searchMode, searchEnabled, err := webSearchMode(req.Canonical)
	if err != nil {
		return provider.DecodedResponse{}, err
	}
	if !searchEnabled {
		return protocolcodec.DecodeChatWithReasoningCarrier(ctx, c.standard, req, ingress, protocolcodec.ReasoningContentExtractor{})
	}
	state := newCitationState()
	cleaned, err := cleanCitationIngress(ctx, ingress, state)
	if err != nil {
		return provider.DecodedResponse{}, err
	}
	decoded, err := protocolcodec.DecodeChatWithReasoningCarrier(ctx, c.standard, req, cleaned, protocolcodec.ReasoningContentExtractor{})
	if err != nil {
		return decoded, err
	}
	decoded.Stream = &veniceResponseStream{upstream: decoded.Stream, citations: state, searchRequired: searchEnabled && searchMode == "on", exchangeID: req.ExchangeID}
	return decoded, nil
}

type citation struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

type citationState struct {
	mu         sync.Mutex
	citations  []citation
	references map[int]struct{}
}

func newCitationState() *citationState {
	return &citationState{references: make(map[int]struct{})}
}

func (s *citationState) observe(citations []citation, references []int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(citations) > 0 {
		s.citations = append([]citation(nil), citations...)
	}
	for _, reference := range references {
		s.references[reference] = struct{}{}
	}
}

func (s *citationState) snapshot() ([]citation, map[int]struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	citations := append([]citation(nil), s.citations...)
	references := make(map[int]struct{}, len(s.references))
	for reference := range s.references {
		references[reference] = struct{}{}
	}
	return citations, references
}

func cleanCitationIngress(ctx context.Context, ingress provider.Ingress, state *citationState) (provider.Ingress, error) {
	switch value := ingress.(type) {
	case provider.DocumentIngress:
		cleaned, citations, references, err := cleanCitationJSON(value.Document.RawBytes(), stripCitationMarkers)
		if err != nil {
			return nil, err
		}
		state.observe(citations, references)
		value.Document = carrier.NewDocument(value.Document.Family, value.Document.Media, value.Document.Header, cleaned, value.Document.Meta)
		return value, nil
	case provider.StreamIngress:
		value.Stream.Body = newVeniceSSEBody(ctx, value.Stream.Body, state)
		return value, nil
	default:
		return nil, canonical.InternalError("Venice provider ingress is unsupported")
	}
}

var (
	citationMarker       = regexp.MustCompile(`\[REF\](\d+)\[/REF\]`)
	legacyCitationMarker = regexp.MustCompile(`\^([1-9]\d*(?:,[1-9]\d*)*)\^`)
)

func cleanCitationJSON(raw []byte, stripMarkers func(string) (string, []int)) ([]byte, []citation, []int, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, nil, nil, canonical.InternalError("Venice Chat response is invalid JSON")
	}
	var citations []citation
	var references []int
	citations = append(citations, removeCitationField(root)...)
	if rawParameters, ok := root["venice_parameters"]; ok {
		var parameters map[string]json.RawMessage
		if json.Unmarshal(rawParameters, &parameters) == nil {
			citations = append(citations, removeCitationField(parameters)...)
			root["venice_parameters"], _ = json.Marshal(parameters)
		}
	}
	if rawChoices, ok := root["choices"]; ok {
		var choices []map[string]json.RawMessage
		if json.Unmarshal(rawChoices, &choices) == nil {
			for _, choice := range choices {
				for _, field := range []string{"message", "delta"} {
					var object map[string]json.RawMessage
					if json.Unmarshal(choice[field], &object) == nil {
						citations = append(citations, removeCitationField(object)...)
						if rawContent, ok := object["content"]; ok {
							var content string
							if json.Unmarshal(rawContent, &content) == nil {
								cleaned, found := stripMarkers(content)
								references = append(references, found...)
								object["content"], _ = json.Marshal(cleaned)
							}
						}
						choice[field], _ = json.Marshal(object)
					}
				}
			}
			root["choices"], _ = json.Marshal(choices)
		}
	}
	cleaned, err := json.Marshal(root)
	if err != nil {
		return nil, nil, nil, canonical.InternalError("Venice Chat response could not be normalized")
	}
	return cleaned, citations, references, nil
}

func stripCitationMarkers(text string) (string, []int) {
	var references []int
	cleaned := citationMarker.ReplaceAllStringFunc(text, func(marker string) string {
		match := citationMarker.FindStringSubmatch(marker)
		reference, err := strconv.Atoi(match[1])
		if err == nil {
			references = append(references, reference)
		}
		return ""
	})
	cleaned = legacyCitationMarker.ReplaceAllStringFunc(cleaned, func(marker string) string {
		match := legacyCitationMarker.FindStringSubmatch(marker)
		for _, ordinal := range strings.Split(match[1], ",") {
			reference, err := strconv.Atoi(ordinal)
			if err == nil {
				references = append(references, reference-1)
			}
		}
		return ""
	})
	return cleaned, references
}

func removeCitationField(object map[string]json.RawMessage) []citation {
	raw, ok := object["web_search_citations"]
	if !ok {
		return nil
	}
	delete(object, "web_search_citations")
	var values []citation
	_ = json.Unmarshal(raw, &values)
	return values
}

type veniceSSEBody struct {
	ctx        context.Context
	reader     *core.SSEReaderCloser
	state      *citationState
	buffer     strings.Reader
	markerTail string
}

func newVeniceSSEBody(ctx context.Context, body io.ReadCloser, state *citationState) *veniceSSEBody {
	return &veniceSSEBody{ctx: ctx, reader: core.NewSSEReader(body), state: state}
}

func (b *veniceSSEBody) Read(output []byte) (int, error) {
	for b.buffer.Len() == 0 {
		event, err := b.reader.Next(b.ctx)
		if err != nil {
			return 0, err
		}
		data := event.Data
		if data != "[DONE]" {
			cleaned, citations, references, cleanErr := cleanCitationJSON([]byte(data), b.stripStreamMarkers)
			if cleanErr != nil {
				return 0, cleanErr
			}
			b.state.observe(citations, references)
			data = string(cleaned)
		}
		var framed strings.Builder
		if event.Event != "" {
			fmt.Fprintf(&framed, "event: %s\n", event.Event)
		}
		fmt.Fprintf(&framed, "data: %s\n\n", data)
		b.buffer.Reset(framed.String())
	}
	return b.buffer.Read(output)
}

func (b *veniceSSEBody) stripStreamMarkers(text string) (string, []int) {
	combined := b.markerTail + text
	b.markerTail = ""
	cleaned, references := stripCitationMarkers(combined)
	cut := incompleteCitationMarkerStart(cleaned)
	if cut >= 0 {
		b.markerTail = cleaned[cut:]
		cleaned = cleaned[:cut]
	}
	return cleaned, references
}

func incompleteCitationMarkerStart(text string) int {
	for index := strings.LastIndex(text, "["); index >= 0; index = strings.LastIndex(text[:index], "[") {
		suffix := text[index:]
		if incompleteCitationMarker(suffix) {
			return index
		}
	}
	for index := strings.LastIndex(text, "^"); index >= 0; index = strings.LastIndex(text[:index], "^") {
		if incompleteLegacyCitationMarker(text[index:]) {
			return index
		}
	}
	return -1
}

func incompleteCitationMarker(value string) bool {
	if strings.HasPrefix("[REF]", value) {
		return true
	}
	if !strings.HasPrefix(value, "[REF]") {
		return false
	}
	remainder := strings.TrimPrefix(value, "[REF]")
	digitCount := 0
	for digitCount < len(remainder) && remainder[digitCount] >= '0' && remainder[digitCount] <= '9' {
		digitCount++
	}
	if digitCount == 0 {
		return remainder == ""
	}
	closing := remainder[digitCount:]
	return closing == "" || strings.HasPrefix("[/REF]", closing)
}

func incompleteLegacyCitationMarker(value string) bool {
	if value == "^" {
		return true
	}
	if !strings.HasPrefix(value, "^") {
		return false
	}
	remainder := strings.TrimPrefix(value, "^")
	expectDigit := true
	for _, character := range remainder {
		switch {
		case character >= '1' && character <= '9':
			expectDigit = false
		case character == '0' && !expectDigit:
		case character == ',' && !expectDigit:
			expectDigit = true
		default:
			return false
		}
	}
	return true
}

func (b *veniceSSEBody) Close() error { return b.reader.Close() }

func citationItems(exchangeID string, citations []citation) ([]canonical.CanonicalItem, error) {
	callID, err := canonical.NewToolCallID("venice_search_" + exchangeID)
	if err != nil {
		callID, err = canonical.NewToolCallID("venice_search")
		if err != nil {
			return nil, err
		}
	}
	input, err := canonical.NewWebSearchToolInput(canonical.WebSearchCall{Action: canonical.WebSearchActionSearch})
	if err != nil {
		return nil, err
	}
	call, err := canonical.NewToolCallItem(callID, canonical.NewWebSearchDeclaration().Key(), input)
	if err != nil {
		return nil, err
	}
	sources := citationSources(citations, nil)
	result, err := canonical.NewWebSearchResult(sources)
	if err != nil {
		return nil, err
	}
	resultItem, err := canonical.NewWebSearchResultItem(callID, result)
	if err != nil {
		return nil, err
	}
	return []canonical.CanonicalItem{call, resultItem}, nil
}

func citationSources(citations []citation, references map[int]struct{}) []canonical.WebSource {
	sources := make([]canonical.WebSource, 0, len(citations))
	for index, value := range citations {
		if references != nil {
			if _, ok := references[index]; !ok {
				continue
			}
		}
		webURL, err := canonical.NewWebURL(strings.TrimSpace(value.URL))
		if err != nil {
			continue
		}
		title := canonical.Unspecified[string]()
		if strings.TrimSpace(value.Title) != "" {
			title = canonical.Specify(strings.TrimSpace(value.Title))
		}
		source, err := canonical.NewWebSource(webURL, title)
		if err == nil {
			sources = append(sources, source)
		}
	}
	return sources
}

type veniceResponseStream struct {
	upstream       canonical.ResponseStream
	citations      *citationState
	searchRequired bool
	pending        []canonical.Event
	exchangeID     string
	offset         uint32
	seq            int64
}

func (s *veniceResponseStream) Next(ctx context.Context) (canonical.Event, error) {
	if len(s.pending) > 0 {
		event := s.pending[0]
		s.pending = s.pending[1:]
		s.seq++
		event.Seq = s.seq
		return event, nil
	}
	event, err := s.upstream.Next(ctx)
	if err != nil {
		return event, err
	}
	if event.Kind == canonical.EventResponseIdentity && s.offset == 0 {
		citations, _ := s.citations.snapshot()
		if !s.searchRequired && len(citations) == 0 {
			s.seq++
			event.Seq = s.seq
			return event, nil
		}
		items, itemErr := citationItems(s.exchangeID, citations)
		if itemErr != nil {
			return canonical.Event{}, itemErr
		}
		s.offset = uint32(len(items))
		generated := canonical.SynthesizeResponseEnvelopeEvents(s.exchangeID, canonical.ResponseRef{}, "", items, canonical.Completion{}, canonical.NewUnknownTokenUsage())
		for _, candidate := range generated {
			switch candidate.Kind {
			case canonical.EventItemStart, canonical.EventContentStart, canonical.EventTextDelta, canonical.EventArgsDelta, canonical.EventItemCompleted:
				s.pending = append(s.pending, candidate)
			}
		}
	}
	if itemEvent, ok := event.Payload.(canonical.ItemEvent); ok {
		if completed, ok := itemEvent.Payload.(canonical.ItemCompletedPayload); ok {
			item, rewriteErr := s.citeMessage(completed.Item)
			if rewriteErr != nil {
				return canonical.Event{}, rewriteErr
			}
			itemEvent.Payload = canonical.ItemCompletedPayload{Item: item}
		}
		if s.offset > 0 {
			itemEvent.Position.Item += s.offset
		}
		event.Payload = itemEvent
	}
	s.seq++
	event.Seq = s.seq
	return event, nil
}

func (s *veniceResponseStream) citeMessage(item canonical.CanonicalItem) (canonical.CanonicalItem, error) {
	message, ok := item.Message()
	if !ok || message.Role() != canonical.MessageRoleAssistant {
		return item, nil
	}
	citations, references := s.citations.snapshot()
	sources := citationSources(citations, references)
	if len(sources) == 0 {
		return item, nil
	}
	webCitations := make([]canonical.WebCitation, len(sources))
	for index, source := range sources {
		webCitations[index] = canonical.WebCitation{Source: source}
	}
	parts := message.Content()
	for index, part := range parts {
		text, ok := part.Text()
		if !ok {
			continue
		}
		cited, err := canonical.NewCitedTextMessagePart(text.Text(), webCitations)
		if err != nil {
			return canonical.CanonicalItem{}, err
		}
		parts[index] = cited
		break
	}
	return canonical.NewScopedMessageItem(message.Role(), parts, message.Scope())
}

func (s *veniceResponseStream) Close(ctx context.Context) error { return s.upstream.Close(ctx) }

var _ provider.Codec = codec{}
var _ provider.TargetSupportResolver = (*modelSupportStore)(nil)

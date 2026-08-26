package chatgpt

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/session"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestBackendCodecPreservesRawJSONIntegers(t *testing.T) {
	request := canonicaltest.LargeIntegerRequest(t, "gpt-5.4-mini")
	names, _, err := provider.BuildAttemptToolNames(request)
	if err != nil {
		t.Fatal(err)
	}
	document, _, err := newBackendCodec("chatgpt").Encode(provider.Request{
		Canonical: request,
		ToolNames: names,
		Delivery:  delivery.StreamingDelivery(delivery.FramingSSE),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := bytes.Count(document.RawBytes(), []byte("9007199254740993")); got != 3 {
		t.Fatalf("large integer occurrences = %d, want 3: %s", got, document.RawBytes())
	}
}

func TestBackendCodecNormalizesCodexPayload(t *testing.T) {
	t.Parallel()

	doc, _, err := newBackendCodec("chatgpt").Encode(provider.Request{
		Canonical: canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: canonical.Specify("gpt-5.4-mini"),
			Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hello")},
		}),
		Delivery: delivery.StreamingDelivery(delivery.FramingSSE),
	})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(doc.RawBytes(), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if _, ok := payload["instructions"]; ok {
		t.Fatalf("instructions overlay must be absent, got %#v", payload["instructions"])
	}
	if store, ok := payload["store"].(bool); !ok || store {
		t.Fatalf("store=%#v, want false", payload["store"])
	}
	if stream, ok := payload["stream"].(bool); !ok || !stream {
		t.Fatalf("stream=%#v, want true", payload["stream"])
	}
	input, ok := payload["input"].([]any)
	if !ok || len(input) == 0 {
		t.Fatalf("input=%#v, want non-empty list", payload["input"])
	}
	first, ok := input[0].(map[string]any)
	if !ok || first["type"] != "message" || first["role"] != "user" || first["content"] != "hello" {
		t.Fatalf("input[0]=%#v, want canonical user message", input[0])
	}
}

func TestBackendCodecRejectsBufferedProviderDelivery(t *testing.T) {
	_, _, err := newBackendCodec("chatgpt").Encode(provider.Request{
		Canonical: canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: canonical.Specify("gpt-5.4-mini"),
			Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hello")},
		}),
		Delivery: delivery.BufferedDelivery(),
	})
	if err == nil {
		t.Fatal("buffered provider delivery must be rejected")
	}
}

func TestBackendCodecUsesSharedOfficialResponsesToolLowering(t *testing.T) {
	childKey, _ := canonical.NewToolKey("workspace", canonical.ToolKindFunction, "read_file")
	child := canonicaltest.MustFunctionTool(childKey, "Read", canonicaltest.Schema(t, `{"type":"object"}`), canonical.Unspecified[bool]())
	namespaceKey, _ := canonical.NewRequestToolKey(canonical.ToolKindNamespace, "workspace")
	namespace, _ := canonical.NewToolNamespace(namespaceKey, "Workspace", []canonical.ToolDeclaration{child})
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("model"),
		Items: []canonical.CanonicalItem{canonicaltest.ToolDeclarations(t, namespace), canonicaltest.Message(t, canonical.MessageRoleUser, "read")},
	})
	names, _, err := provider.BuildAttemptToolNames(request)
	if err != nil {
		t.Fatal(err)
	}
	document, _, err := newBackendCodec("chatgpt").Encode(provider.Request{Canonical: request, ToolNames: names, Delivery: delivery.StreamingDelivery(delivery.FramingSSE)})
	if err != nil {
		t.Fatal(err)
	}
	wireName, _ := names.WireName(childKey)
	if bytes.Contains(document.RawBytes(), []byte(`"type":"namespace"`)) || !bytes.Contains(document.RawBytes(), []byte(`"name":"`+wireName+`"`)) {
		t.Fatalf("ChatGPT Responses document = %s", document.RawBytes())
	}
}

// TestChatGPTResponseIDDoesNotBecomeNativeContinuation proves the Codex
// regression fix: ChatGPT/Codex is stateless under store:false and rejects
// previous_response_id, so a ChatGPT provider response ID must decode as
// identity-only (ResponseRef.Responses == nil). It must never become a reusable
// ResponsesContinuation, which would make session routing select the Delta
// representation and emit previous_response_id on the next turn.
func TestChatGPTResponseIDDoesNotBecomeNativeContinuation(t *testing.T) {
	t.Parallel()

	raw := "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"provider_resp_789\",\"model\":\"gpt-5.4-mini\",\"status\":\"in_progress\",\"output\":[]}}\n\n" +
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"provider_resp_789\",\"status\":\"completed\",\"output\":[]}}\n\n"
	decoded, err := newBackendCodec("chatgpt").Decode(context.Background(), provider.Request{ExchangeID: "ex_store_false"}, provider.StreamIngress{Stream: carrier.ByteStream{
		MediaType: "text/event-stream",
		Body:      io.NopCloser(strings.NewReader(raw)),
	}})
	if err != nil {
		t.Fatal(err)
	}
	target := provider.NewTargetSnapshot("chatgpt-target", "chatgpt", "https://chatgpt.test", "", protocolkind.Responses, "responses", delivery.BufferedDelivery())
	target.Model = "gpt-5.4-mini"
	closed, err := canonical.ReadClosedEnvelope(context.Background(), canonical.NewBoundResponseIdentityStream(decoded.Stream, canonical.ResponseBinding{SwobuID: "resp_test", TargetID: target.TargetID, TargetVersion: target.TargetVersion}), canonical.EnvResponse)
	if err != nil {
		t.Fatal(err)
	}
	output, err := closed.ProjectResponse()
	if err != nil {
		t.Fatal(err)
	}
	if responsesRef := output.Response().Responses; responsesRef != nil {
		t.Fatalf("ChatGPT response ID must stay identity-only, got native continuation %#v", responsesRef)
	}
}

// TestChatGPTWebSearchReplayOmitsSyntheticItemID proves the second release
// blocker is fixed: a replayed idless web_search_call (Codex durable rollout
// under store:false) must round-trip through canonical and re-encode with no
// item.id at all. The canonical call correlation token (a synthetic
// "toolu_swobu_*" minted only for call↔result pairing) must never become a
// Responses item.id. Provider-owned item identity and canonical correlation are
// distinct identities; only an exact preserved refinement may be emitted as id.
func TestChatGPTWebSearchReplayOmitsSyntheticItemID(t *testing.T) {
	t.Parallel()

	// An idless Codex replay decodes to a web-search call carrying a synthetic
	// request-local correlation id and no Responses refinement, plus its result.
	searchInput, err := canonical.NewWebSearchToolInput(canonical.WebSearchCall{
		Action:  canonical.WebSearchActionSearch,
		Queries: []string{"site:openai.com documentation search"},
	})
	if err != nil {
		t.Fatal(err)
	}
	correlation, err := canonical.NewToolCallID("toolu_swobu_66_0")
	if err != nil {
		t.Fatal(err)
	}
	// No refinement: this is the idless case. Passing nil (not the correlation)
	// is the only faithful representation of an omitted provider id.
	call, err := canonical.NewToolCallItemWithResponsesWebSearch(correlation, canonical.WebSearchToolKey(), searchInput, nil)
	if err != nil {
		t.Fatal(err)
	}
	searchResult, _ := canonical.NewWebSearchResult(nil)
	result, err := canonical.NewWebSearchResultItem(correlation, searchResult)
	if err != nil {
		t.Fatal(err)
	}

	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("gpt-5.4-mini"),
		Items: []canonical.CanonicalItem{
			canonicaltest.Message(t, canonical.MessageRoleUser, "find the deadline"),
			call,
			result,
			canonicaltest.Message(t, canonical.MessageRoleAssistant, "July 21"),
			canonicaltest.Message(t, canonical.MessageRoleUser, "verify it"),
		},
	})

	document, _, err := newBackendCodec("chatgpt").Encode(provider.Request{
		Canonical: request,
		Delivery:  delivery.StreamingDelivery(delivery.FramingSSE),
	})
	if err != nil {
		t.Fatal(err)
	}
	wire := string(document.RawBytes())

	// The web_search_call item must survive replay intact.
	if !strings.Contains(wire, `"type":"web_search_call"`) {
		t.Fatalf("web_search_call dropped from replayed history: %s", wire)
	}
	// The synthetic canonical correlation id must never leak onto the wire as a
	// Responses item.id (or anywhere else).
	if strings.Contains(wire, "toolu_swobu_") {
		t.Fatalf("synthetic canonical correlation leaked into ChatGPT wire: %s", wire)
	}

	// The replayed web_search_call must carry no id field at all.
	var payload struct {
		Input []json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(document.RawBytes(), &payload); err != nil {
		t.Fatal(err)
	}
	for _, raw := range payload.Input {
		if !strings.Contains(string(raw), `"type":"web_search_call"`) {
			continue
		}
		var item struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(raw, &item); err != nil {
			t.Fatal(err)
		}
		if item.ID != "" {
			t.Fatalf("idless web_search_call replay gained an item id %q: %s", item.ID, raw)
		}
	}

	// store:false remains the ChatGPT/Codex provider contract.
	var fullPayload map[string]any
	if err := json.Unmarshal(document.RawBytes(), &fullPayload); err != nil {
		t.Fatal(err)
	}
	if store, ok := fullPayload["store"].(bool); !ok || store {
		t.Fatalf("store=%#v, want false (ChatGPT/Codex requires store:false)", fullPayload["store"])
	}
}

// TestChatGPTWebSearchReplayOmitsActionSources proves the third release blocker
// in this provider contract is fixed: ChatGPT/Codex request input rejects
// web_search_call action.sources ("Unknown parameter: input[N].action.sources").
// A completed web-search call from turn one replays into the turn-two request as
// call state only — action type + query preserved, status completed, the exact
// provider ws id retained — but action.sources omitted. The discovered sources
// stay canonical-complete for client responses and citations; they only leave
// the Codex request grammar.
func TestChatGPTWebSearchReplayOmitsActionSources(t *testing.T) {
	t.Parallel()

	// Turn-one provider output: a completed web search with a real provider item
	// id and discovered sources.
	searchInput, err := canonical.NewWebSearchToolInput(canonical.WebSearchCall{
		Action:  canonical.WebSearchActionSearch,
		Queries: []string{"deadline"},
	})
	if err != nil {
		t.Fatal(err)
	}
	correlation, err := canonical.NewToolCallID("ws_123")
	if err != nil {
		t.Fatal(err)
	}
	refinement, err := canonical.NewResponsesWebSearchRefinement(canonical.ResponsesItemID("ws_123"))
	if err != nil {
		t.Fatal(err)
	}
	call, err := canonical.NewToolCallItemWithResponsesWebSearch(correlation, canonical.WebSearchToolKey(), searchInput, &refinement)
	if err != nil {
		t.Fatal(err)
	}
	sourceURL, err := canonical.NewWebURL("https://example.test/rules")
	if err != nil {
		t.Fatal(err)
	}
	source, err := canonical.NewWebSource(sourceURL, canonical.Specify("Rules"))
	if err != nil {
		t.Fatal(err)
	}
	searchResult, err := canonical.NewWebSearchResult([]canonical.WebSource{source})
	if err != nil {
		t.Fatal(err)
	}
	result, err := canonical.NewWebSearchResultItem(correlation, searchResult)
	if err != nil {
		t.Fatal(err)
	}

	// Turn two: full materialized history (store:false ⇒ no continuation).
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("gpt-5.4-mini"),
		Items: []canonical.CanonicalItem{
			canonicaltest.Message(t, canonical.MessageRoleUser, "what is the deadline"),
			call,
			result,
			canonicaltest.Message(t, canonical.MessageRoleAssistant, "July 21"),
			canonicaltest.Message(t, canonical.MessageRoleUser, "verify it"),
		},
	})

	document, _, err := newBackendCodec("chatgpt").Encode(provider.Request{
		Canonical: request,
		Delivery:  delivery.StreamingDelivery(delivery.FramingSSE),
	})
	if err != nil {
		t.Fatal(err)
	}
	wire := string(document.RawBytes())

	var payload struct {
		Input []json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(document.RawBytes(), &payload); err != nil {
		t.Fatal(err)
	}

	// Locate the one web_search_call in the replayed input and assert its shape.
	var found bool
	for _, raw := range payload.Input {
		if !strings.Contains(string(raw), `"type":"web_search_call"`) {
			continue
		}
		found = true
		var item struct {
			ID     string          `json:"id"`
			Status string          `json:"status"`
			Action json.RawMessage `json:"action"`
		}
		if err := json.Unmarshal(raw, &item); err != nil {
			t.Fatal(err)
		}
		if item.ID != "ws_123" {
			t.Fatalf("web_search_call id = %q, want exact provider id ws_123: %s", item.ID, raw)
		}
		if item.Status != "completed" {
			t.Fatalf("web_search_call status = %q, want completed: %s", item.Status, raw)
		}
		// The replayable call state — action type + query — is preserved.
		var action struct {
			Type    string `json:"type"`
			Query   string `json:"query"`
			Sources json.RawMessage
		}
		if err := json.Unmarshal(item.Action, &action); err != nil {
			t.Fatal(err)
		}
		if action.Type != "search" || action.Query != "deadline" {
			t.Fatalf("replayable action state lost: %#v", action)
		}
		// The Codex-rejected field must be entirely absent, not just empty.
		if strings.Contains(string(item.Action), `"sources"`) {
			t.Fatalf("action.sources leaked into ChatGPT request input: %s", item.Action)
		}
	}
	if !found {
		t.Fatalf("web_search_call dropped from replayed history: %s", wire)
	}

	// The discovered source URL must NOT cross the Codex request boundary. (It
	// still exists in the canonical result for client responses and citations.)
	if strings.Contains(wire, "https://example.test/rules") {
		t.Fatalf("web-search source URL leaked into ChatGPT request input: %s", wire)
	}

	// The remaining ChatGPT/Codex provider invariants hold alongside this one.
	if strings.Contains(wire, "previous_response_id") {
		t.Fatalf("ChatGPT wire must omit previous_response_id: %s", wire)
	}
	if strings.Contains(wire, "toolu_swobu_") {
		t.Fatalf("synthetic canonical correlation leaked into ChatGPT wire: %s", wire)
	}
	var fullPayload map[string]any
	if err := json.Unmarshal(document.RawBytes(), &fullPayload); err != nil {
		t.Fatal(err)
	}
	if store, ok := fullPayload["store"].(bool); !ok || store {
		t.Fatalf("store=%#v, want false (ChatGPT/Codex requires store:false)", fullPayload["store"])
	}
}

// regression fix at the session + codec seam (no live backend required). Because
// the ChatGPT decoder produces an identity-only ResponseRef (Responses == nil),
// session routing must select the fully materialized request for the second
// turn, never the Delta. The resulting wire request then carries the complete
// turn history and no previous_response_id — the exact shape ChatGPT/Codex
// (stateless under store:false) accepts.
func TestChatGPTTwoTurnReplayOmitsPreviousResponseID(t *testing.T) {
	t.Parallel()

	target := provider.NewTargetSnapshot("chatgpt-target", "chatgpt", "https://chatgpt.test", "", protocolkind.Responses, "responses", delivery.BufferedDelivery())
	target.Model = "gpt-5.4-mini"

	turnOne := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("gpt-5.4-mini"),
		Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "turn one")},
	})
	rawTurnOne := "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"provider_resp_previous\",\"model\":\"gpt-5.4-mini\",\"status\":\"in_progress\",\"store\":false}}\n\n" +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"message\",\"id\":\"msg_1\",\"status\":\"completed\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"answer one\"}]}}\n\n" +
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"provider_resp_previous\",\"model\":\"gpt-5.4-mini\",\"status\":\"completed\",\"store\":false,\"output\":[]}}\n\n"
	decoded, err := newBackendCodec("chatgpt").Decode(context.Background(), provider.Request{ExchangeID: "ex_turn_one", Canonical: turnOne}, provider.StreamIngress{Stream: carrier.ByteStream{
		MediaType: "text/event-stream",
		Body:      io.NopCloser(strings.NewReader(rawTurnOne)),
	}})
	if err != nil {
		t.Fatal(err)
	}
	turnOneResponse, err := canonical.ProjectStream(context.Background(), canonical.NewBoundResponseIdentityStream(decoded.Stream, canonical.ResponseBinding{
		SwobuID:       "resp_previous",
		TargetID:      target.TargetID,
		TargetVersion: target.TargetVersion,
	}), canonical.ResponseBinding{SwobuID: "resp_previous"})
	if err != nil {
		t.Fatal(err)
	}
	if turnOneResponse.Response().Responses != nil {
		t.Fatal("ChatGPT decoder minted native continuation during turn-one checkpoint commit")
	}
	store := session.NewMemoryStore()
	if _, err := store.StartSession(context.Background(), "dev", session.Checkpoint{HistoryScheme: "responses/v1", Request: turnOne, Response: *turnOneResponse}); err != nil {
		t.Fatal(err)
	}
	checkpoint, found, err := store.Get(context.Background(), "dev", "resp_previous")
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("turn-one checkpoint was not available for client continuation")
	}
	turnTwo := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("gpt-5.4-mini"),
		Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "turn two")},
		PreviousResponse: &canonical.ResponseRef{
			SwobuID: "resp_previous",
		},
	})
	prepared, err := session.Resume(turnTwo, checkpoint)
	if err != nil {
		t.Fatal(err)
	}

	// Hydration stores one complete request. The preferred attempt must keep
	// complete history because ChatGPT responses never mint native continuation.
	selected := prepared.Request()
	if _, ok := prepared.PreviousHistory(target.TargetID, target.TargetVersion); ok {
		t.Fatal("ChatGPT identity-only handle exposed Responses continuation")
	}

	document, _, err := newBackendCodec("chatgpt").Encode(provider.Request{Canonical: selected, Delivery: delivery.StreamingDelivery(delivery.FramingSSE)})
	if err != nil {
		t.Fatal(err)
	}
	wire := string(document.RawBytes())
	if strings.Contains(wire, "previous_response_id") {
		t.Fatalf("ChatGPT wire must omit previous_response_id: %s", wire)
	}
	if !strings.Contains(wire, "turn one") || !strings.Contains(wire, "answer one") || !strings.Contains(wire, "turn two") {
		t.Fatalf("ChatGPT wire must carry the full materialized history: %s", wire)
	}
	var payload map[string]any
	if err := json.Unmarshal(document.RawBytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if store, ok := payload["store"].(bool); !ok || store {
		t.Fatalf("store=%#v, want false (ChatGPT is stateless for replay)", payload["store"])
	}
}

func TestChatGPTLowersWebSearchDeclarationAndPolicy(t *testing.T) {
	t.Parallel()
	set, err := canonical.NewToolSet([]canonical.ToolDeclaration{canonical.NewWebSearchDeclaration()})
	if err != nil {
		t.Fatal(err)
	}

	// Case 1: Active tool declaration without specific policy is safely omitted for ChatGPT Codex compatibility.
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("gpt-5.4-mini"),
		Items: []canonical.CanonicalItem{
			canonicaltest.ToolDeclarations(t, set.Declarations()...),
			canonicaltest.Message(t, canonical.MessageRoleUser, "search the web"),
		},
	})
	names, _, err := provider.BuildAttemptToolNames(request)
	if err != nil {
		t.Fatal(err)
	}
	doc, _, err := newBackendCodec("chatgpt").Encode(provider.Request{
		Canonical: request,
		ToolNames: names,
		Delivery:  delivery.StreamingDelivery(delivery.FramingSSE),
	})
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(doc.RawBytes(), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if tools, ok := payload["tools"].([]any); ok && len(tools) != 0 {
		t.Fatalf("payload tools = %#v, want 0 tools (web_search omitted on ChatGPT)", payload["tools"])
	}

	// Case 2: Specific tool policy requiring web search returns IncompatibleCapability.
	webSearchKey := canonical.WebSearchToolKey()
	reqSpecific := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("gpt-5.4-mini"),
		Items: []canonical.CanonicalItem{
			canonicaltest.ToolDeclarations(t, set.Declarations()...),
			canonicaltest.Message(t, canonical.MessageRoleUser, "search the web"),
		},
		ToolPolicy: canonical.Specify(canonical.NewToolPolicy(canonical.ToolPolicySpecific, &webSearchKey)),
	})
	namesSpecific, _, err := provider.BuildAttemptToolNames(reqSpecific)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = newBackendCodec("chatgpt").Encode(provider.Request{
		Canonical: reqSpecific,
		ToolNames: namesSpecific,
		Delivery:  delivery.StreamingDelivery(delivery.FramingSSE),
	})
	if err == nil {
		t.Fatal("expected error for specific web search policy on ChatGPT, got nil")
	}
}

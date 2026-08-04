package responses

import (
	"reflect"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/wire"
)

type responsesHistoryMutationCase struct {
	name    string
	base    string
	mutated string
}

func TestHistoryFingerprintSemanticRefinement(t *testing.T) {
	core := `{"model":"m","input":[
		{"type":"message","role":"user","content":[{"type":"input_text","text":"U1"},{"type":"input_image","image_url":"https://example.test/one.png","detail":"low"}]},
		{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"plan A"}],"encrypted_content":"opaque-A"},
		{"type":"function_call","id":"fc_1","call_id":"call_1","name":"lookup","arguments":"{\"q\":\"one\"}"},
		{"type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"A1"}]},
		{"type":"function_call_output","call_id":"call_1","output":[{"type":"input_text","text":"result one"}]},
		{"type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"A2"}]},
		{"type":"message","role":"user","content":[{"type":"input_text","text":"U3"}]}
	]}`
	webSearch := `{"model":"m","input":[
		{"type":"message","role":"user","content":[{"type":"input_text","text":"find it"}]},
		{"type":"web_search_call","id":"ws_1","status":"completed","action":{"type":"search","queries":["one"],"sources":[{"type":"url","url":"https://a.test","title":"A"}]}},
		{"type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"answer","annotations":[{"type":"url_citation","url":"https://a.test","title":"A","start_index":0,"end_index":6}]}]},
		{"type":"message","role":"user","content":[{"type":"input_text","text":"next"}]}
	]}`
	discovery := `{"model":"m","input":[
		{"type":"message","role":"user","content":[{"type":"input_text","text":"find tools"}]},
		{"type":"tool_search_call","call_id":"search_1","execution":"server","arguments":{"query":"files"}},
		{"type":"tool_search_output","call_id":"search_1","status":"completed","execution":"server","tools":[{"type":"function","name":"read_file","description":"read one","parameters":{"type":"object","properties":{"path":{"type":"string"}}}}]},
		{"type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"ready"}]},
		{"type":"message","role":"user","content":[{"type":"input_text","text":"next"}]}
	]}`
	cases := []responsesHistoryMutationCase{
		{name: "user text", base: core, mutated: strings.Replace(core, "U1", "U1 changed", 1)},
		{name: "image URL", base: core, mutated: strings.Replace(core, "one.png", "two.png", 1)},
		{name: "image detail", base: core, mutated: strings.Replace(core, `"detail":"low"`, `"detail":"high"`, 1)},
		{name: "reasoning summary", base: core, mutated: strings.Replace(core, "plan A", "plan B", 1)},
		{name: "encrypted reasoning", base: core, mutated: strings.Replace(core, "opaque-A", "opaque-B", 1)},
		{name: "tool call ID", base: core, mutated: strings.ReplaceAll(core, "call_1", "call_2")},
		{name: "tool name", base: core, mutated: strings.Replace(core, "lookup", "search", 1)},
		{name: "tool arguments", base: core, mutated: strings.Replace(core, `\"q\":\"one\"`, `\"q\":\"two\"`, 1)},
		{name: "tool result content", base: core, mutated: strings.Replace(core, "result one", "result two", 1)},
		{name: "output item ordering", base: core, mutated: strings.Replace(core,
			`{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"plan A"}],"encrypted_content":"opaque-A"},
		{"type":"function_call"`,
			`{"type":"function_call"`, 1)},
		{name: "historical item deletion", base: core, mutated: strings.Replace(core, `{"type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"A2"}]},`, "", 1)},
		{name: "web search query", base: webSearch, mutated: strings.Replace(webSearch, `"one"`, `"two"`, 1)},
		{name: "web search source URL", base: webSearch, mutated: strings.ReplaceAll(webSearch, "https://a.test", "https://b.test")},
		{name: "web search source title", base: webSearch, mutated: strings.ReplaceAll(webSearch, `"title":"A"`, `"title":"B"`)},
		{name: "discovery call ID", base: discovery, mutated: strings.ReplaceAll(discovery, "search_1", "search_2")},
		{name: "discovery arguments", base: discovery, mutated: strings.Replace(discovery, `"query":"files"`, `"query":"docs"`, 1)},
		{name: "discovery tool name", base: discovery, mutated: strings.Replace(discovery, "read_file", "read_doc", 1)},
		{name: "discovery tool schema", base: discovery, mutated: strings.Replace(discovery, `"path"`, `"file"`, 1)},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			base := decodeResponsesFingerprintRequest(t, test.base)
			mutated := decodeResponsesFingerprintRequest(t, test.mutated)
			basePrevious := responsesPreviousCanonicalHistory(t, base)
			mutatedPrevious := responsesPreviousCanonicalHistory(t, mutated)
			if reflect.DeepEqual(basePrevious, mutatedPrevious) {
				t.Fatal("mutation did not change normal decoded previous semantics")
			}
			if base.RebasedRequest.Previous == mutated.RebasedRequest.Previous {
				t.Fatal("different previous canonical semantics retained predecessor fingerprint")
			}
		})
	}
}

func responsesPreviousCanonicalHistory(t *testing.T, decoded wire.ClientRequestResult) []canonical.CanonicalItem {
	t.Helper()
	if decoded.RebasedRequest == nil {
		t.Fatal("request did not reconstruct previous history")
	}
	_, full, err := canonical.SplitRequestPrelude(decoded.Request.Items())
	if err != nil {
		t.Fatal(err)
	}
	_, current, err := canonical.SplitRequestPrelude(decoded.RebasedRequest.Request.Items())
	if err != nil {
		t.Fatal(err)
	}
	if len(current) > len(full) || !reflect.DeepEqual(full[len(full)-len(current):], current) {
		t.Fatalf("rebased current history is not a suffix of full canonical history\nfull=%#v\ncurrent=%#v", full, current)
	}
	return full[:len(full)-len(current)]
}

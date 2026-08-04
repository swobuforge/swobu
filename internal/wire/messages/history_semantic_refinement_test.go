package messages

import (
	"reflect"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/wire"
)

type messagesHistoryMutationCase struct {
	name    string
	base    string
	mutated string
}

func TestHistoryFingerprintSemanticRefinement(t *testing.T) {
	base := `{"model":"m","system":"current system","messages":[
		{"role":"user","content":[{"type":"text","text":"U1"},{"type":"image","source":{"type":"url","url":"https://example.test/one.png"}}]},
		{"role":"assistant","content":[
			{"type":"thinking","thinking":"plan A","signature":"sig-A"},
			{"type":"text","text":"answer","citations":[{"type":"web_search_result_location","url":"https://a.test","title":"A","cited_text":"answer","start_char_index":0,"end_char_index":6}]},
			{"type":"tool_use","id":"call_1","name":"lookup","input":{"q":"one"}}
		]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_1","is_error":false,"content":"result one"}]},
		{"role":"assistant","content":[{"type":"redacted_thinking","data":"opaque-A"},{"type":"text","text":"A2"}]},
		{"role":"user","content":"U3"}
	]}`
	cases := []messagesHistoryMutationCase{
		{name: "user text", base: base, mutated: strings.Replace(base, "U1", "U1 changed", 1)},
		{name: "image URL", base: base, mutated: strings.Replace(base, "one.png", "two.png", 1)},
		{name: "thinking text", base: base, mutated: strings.Replace(base, "plan A", "plan B", 1)},
		{name: "thinking signature", base: base, mutated: strings.Replace(base, "sig-A", "sig-B", 1)},
		{name: "citation URL", base: base, mutated: strings.Replace(base, "https://a.test", "https://b.test", 1)},
		{name: "citation title", base: base, mutated: strings.Replace(base, `"title":"A"`, `"title":"B"`, 1)},
		{name: "citation text", base: base, mutated: strings.Replace(base, `"cited_text":"answer"`, `"cited_text":"nswer"`, 1)},
		{name: "citation range", base: base, mutated: strings.Replace(base, `"end_char_index":6`, `"end_char_index":5`, 1)},
		{name: "tool call ID", base: base, mutated: strings.ReplaceAll(base, "call_1", "call_2")},
		{name: "tool name", base: base, mutated: strings.Replace(base, "lookup", "search", 1)},
		{name: "tool input", base: base, mutated: strings.Replace(base, `"q":"one"`, `"q":"two"`, 1)},
		{name: "tool result content", base: base, mutated: strings.Replace(base, "result one", "result two", 1)},
		{name: "tool result error", base: base, mutated: strings.Replace(base, `"is_error":false`, `"is_error":true`, 1)},
		{name: "redacted data", base: base, mutated: strings.Replace(base, "opaque-A", "opaque-B", 1)},
		{name: "block ordering", base: base, mutated: strings.Replace(base, `{"type":"thinking","thinking":"plan A","signature":"sig-A"},
			{"type":"text"`, `{"type":"text"`, 1)},
		{name: "block deletion", base: base, mutated: strings.Replace(base, `{"type":"redacted_thinking","data":"opaque-A"},`, "", 1)},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			left := decodeMessagesFingerprintRequest(t, test.base)
			right := decodeMessagesFingerprintRequest(t, test.mutated)
			leftSemantics := messagesPreviousCanonicalHistory(t, left)
			rightSemantics := messagesPreviousCanonicalHistory(t, right)
			if reflect.DeepEqual(leftSemantics, rightSemantics) {
				t.Fatal("mutation did not change normal decoded previous semantics")
			}
			if left.RebasedRequest.Previous == right.RebasedRequest.Previous {
				t.Fatal("different previous canonical semantics retained predecessor fingerprint")
			}
		})
	}
}

func messagesPreviousCanonicalHistory(t *testing.T, decoded wire.ClientRequestResult) []canonical.CanonicalItem {
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
		t.Fatal("rebased current history is not a suffix of full canonical history")
	}
	return full[:len(full)-len(current)]
}

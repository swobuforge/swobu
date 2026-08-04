package chatcompletions

import (
	"reflect"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/wire"
)

type chatHistoryMutationCase struct {
	name           string
	base           string
	mutated        string
	semanticChange bool
}

func TestHistoryFingerprintSemanticRefinement(t *testing.T) {
	conversation := `{
		"model":"m",
		"messages":[
			{"role":"system","content":"leading system A"},
			{"role":"developer","content":"leading developer A"},
			{"role":"user","content":[{"type":"text","text":"U1"},{"type":"image_url","image_url":{"url":"https://example.test/one.png","detail":"low"}}]},
			{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"q\":\"one\"}"}}]},
			{"role":"tool","tool_call_id":"call_1","content":"result one"},
			{"role":"developer","content":"mid developer A"},
			{"role":"user","content":"U2"},
			{"role":"assistant","content":"A2"},
			{"role":"user","content":"U3"}
		]}`
	cases := []chatHistoryMutationCase{
		{name: "leading system neutral", base: conversation, mutated: strings.Replace(conversation, "leading system A", "leading system B", 1)},
		{name: "leading developer neutral", base: conversation, mutated: strings.Replace(conversation, "leading developer A", "leading developer B", 1)},
		{name: "historical user text", base: conversation, mutated: strings.Replace(conversation, "U1", "U1 changed", 1), semanticChange: true},
		{name: "historical image URL", base: conversation, mutated: strings.Replace(conversation, "one.png", "two.png", 1), semanticChange: true},
		{name: "historical image detail", base: conversation, mutated: strings.Replace(conversation, `"detail":"low"`, `"detail":"high"`, 1), semanticChange: true},
		{name: "tool call ID", base: conversation, mutated: strings.ReplaceAll(conversation, "call_1", "call_2"), semanticChange: true},
		{name: "tool name", base: conversation, mutated: strings.Replace(conversation, "lookup", "search", 1), semanticChange: true},
		{name: "tool arguments", base: conversation, mutated: strings.Replace(conversation, `\"q\":\"one\"`, `\"q\":\"two\"`, 1), semanticChange: true},
		{name: "tool result content", base: conversation, mutated: strings.Replace(conversation, "result one", "result two", 1), semanticChange: true},
		{name: "mid developer", base: conversation, mutated: strings.Replace(conversation, "mid developer A", "mid developer B", 1), semanticChange: true},
		{name: "content ordering", base: conversation, mutated: strings.Replace(conversation, `{"type":"text","text":"U1"},{"type":"image_url"`, `{"type":"image_url"`, 1), semanticChange: true},
		{name: "message deletion", base: conversation, mutated: strings.Replace(conversation, `{"role":"developer","content":"mid developer A"},`, "", 1), semanticChange: true},
	}
	assertChatHistoryMutationCases(t, cases)
}

func assertChatHistoryMutationCases(t *testing.T, cases []chatHistoryMutationCase) {
	t.Helper()
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			base := decodeChatFingerprintRequest(t, test.base)
			mutated := decodeChatFingerprintRequest(t, test.mutated)
			basePrevious := chatPreviousCanonicalHistory(t, base)
			mutatedPrevious := chatPreviousCanonicalHistory(t, mutated)
			semanticChanged := !reflect.DeepEqual(basePrevious, mutatedPrevious)
			if semanticChanged != test.semanticChange {
				t.Fatalf("semantic oracle changed=%t, want %t\nbase=%#v\nmutated=%#v", semanticChanged, test.semanticChange, basePrevious, mutatedPrevious)
			}
			fingerprintChanged := base.RebasedRequest.Previous != mutated.RebasedRequest.Previous
			if test.semanticChange && !fingerprintChanged {
				t.Fatal("different previous canonical semantics retained predecessor fingerprint")
			}
			if !test.semanticChange && fingerprintChanged {
				t.Fatal("request-scoped context changed predecessor fingerprint")
			}
		})
	}
}

func chatPreviousCanonicalHistory(t *testing.T, decoded wire.ClientRequestResult) []canonical.CanonicalItem {
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

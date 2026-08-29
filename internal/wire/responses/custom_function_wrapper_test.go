package responses

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestDecodeCustomFunctionWrapperUsesAttemptProvenance(t *testing.T) {
	key := canonicaltest.MustRequestToolKey(canonical.ToolKindCustom, "shell")
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("model"),
		Items: []canonical.CanonicalItem{canonicaltest.ToolDeclarations(t,
			canonicaltest.MustCustomTool(key, "", canonical.EmptyToolFormat()),
		)},
	})
	items, err := decodeCompletedResponsesItemSet(
		context.Background(), request, testAttemptToolNames(request),
		[]json.RawMessage{json.RawMessage(`{"type":"function_call","call_id":"call_1","name":"shell","arguments":"{\"input\":\"echo exact\"}"}`)},
		"", "", nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("decoded items = %#v", items)
	}
	call, ok := items[0].ToolCall()
	if !ok || call.Tool() != key {
		t.Fatalf("decoded call = %#v, want canonical Custom key", call)
	}
	input, ok := call.Input().Text()
	if !ok || input != "echo exact" {
		t.Fatalf("decoded input = %q/%t", input, ok)
	}
}

func TestDecodeCustomFunctionWrapperRejectsInvalidArguments(t *testing.T) {
	key := canonicaltest.MustRequestToolKey(canonical.ToolKindCustom, "shell")
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("model"),
		Items: []canonical.CanonicalItem{canonicaltest.ToolDeclarations(t,
			canonicaltest.MustCustomTool(key, "", canonical.EmptyToolFormat()),
		)},
	})
	for _, arguments := range []string{
		`{}`,
		`{"input":7}`,
		`{"input":"ok","extra":true}`,
		`[]`,
	} {
		t.Run(arguments, func(t *testing.T) {
			encodedArguments, _ := json.Marshal(arguments)
			item := json.RawMessage(`{"type":"function_call","call_id":"call_1","name":"shell","arguments":` + string(encodedArguments) + `}`)
			_, err := decodeCompletedResponsesItemSet(
				context.Background(), request, testAttemptToolNames(request),
				[]json.RawMessage{item}, "", "", nil,
			)
			var backendErr canonical.BackendError
			if !errors.As(err, &backendErr) {
				t.Fatalf("error = %v, want backend protocol error", err)
			}
		})
	}
}

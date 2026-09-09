package messages

import (
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestMessagesToolConformanceWireStates(t *testing.T) {
	yes, no := true, false
	cases := []struct {
		name string
		wire *bool
		want canonical.SchemaConformance
	}{
		{"default", nil, canonical.SchemaConformanceDefault},
		{"relaxed", &no, canonical.SchemaConformanceRelaxed},
		{"enforced", &yes, canonical.SchemaConformanceEnforced},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := messagesToolConformance(tc.wire); got != tc.want {
				t.Fatalf("conformance=%v want=%v", got, tc.want)
			}
			object, _ := canonical.ParseJSONObject([]byte(`{"type":"object"}`))
			key, _ := canonical.NewRequestToolKey(canonical.ToolKindFunction, "lookup")
			declaration, err := canonical.NewFunctionTool(key, "", canonical.NewToolSchemaObject(object), canonical.SchemaContract{Profile: canonical.SchemaProfileAnthropic, Conformance: tc.want})
			if err != nil {
				t.Fatal(err)
			}
			request := canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{canonicaltest.ToolDeclarations(t, declaration)}})
			projection, _, err := DefaultToolLowering().Function(ToolLoweringContext{Names: testAttemptToolNames(request)}, declaration)
			if err != nil {
				t.Fatal(err)
			}
			encoded := projection.Fragments[0]
			if (encoded.Strict == nil) != (tc.wire == nil) {
				t.Fatalf("strict presence changed: %#v", encoded.Strict)
			}
			if tc.wire != nil && *encoded.Strict != *tc.wire {
				t.Fatalf("strict=%v want=%v", *encoded.Strict, *tc.wire)
			}
		})
	}
}

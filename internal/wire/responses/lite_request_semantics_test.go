package responses

import (
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/historyfingerprint"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

// Codex rust-v0.162.1 client.rs builds each Lite prefix from optional tools
// and optional base instructions. compact.rs supplies no tools.
func TestResponsesLiteOptionalPrefixConvergence(t *testing.T) {
	for _, marker := range []string{"http", "websocket"} {
		for _, tc := range []struct{ name, standard, lite string }{
			{"instructions", `{"model":"m","instructions":"base","input":[{"type":"message","role":"user","content":"hi"}]}`, `{"model":"m","instructions":"","input":[{"type":"message","role":"developer","content":"base"},{"type":"message","role":"user","content":"hi"}]}`},
			{"no_prefix", `{"model":"m","input":[{"type":"message","role":"user","content":"hi"}]}`, `{"model":"m","input":[{"type":"message","role":"user","content":"hi"}]}`},
			{"string_input", `{"model":"m","input":"hi"}`, `{"model":"m","input":"hi"}`},
			{"tools_only", `{"model":"m","tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}],"input":[{"type":"message","role":"user","content":"hi"}]}`, `{"model":"m","input":[{"type":"additional_tools","role":"developer","tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}]},{"type":"message","role":"user","content":"hi"}]}`},
			{"empty_tools_and_instructions", `{"model":"m","instructions":"base","input":[{"type":"message","role":"user","content":"hi"}]}`, `{"model":"m","input":[{"type":"additional_tools","role":"developer","tools":[]},{"type":"message","role":"developer","content":"base"},{"type":"message","role":"user","content":"hi"}]}`},
			{"later_developer", `{"model":"m","instructions":"base","input":[{"type":"message","role":"user","content":"hi"},{"type":"message","role":"developer","content":"history"}]}`, `{"model":"m","input":[{"type":"message","role":"developer","content":"base"},{"type":"message","role":"user","content":"hi"},{"type":"message","role":"developer","content":"history"}]}`},
		} {
			t.Run(marker+"/"+tc.name, func(t *testing.T) {
				standard := decodeResponsesConvergenceRequest(t, tc.standard, nil)
				raw, headers := testLiteMarker(t, tc.lite, marker)
				lite := decodeResponsesConvergenceRequest(t, raw, headers)
				if standard.RequestFingerprint != lite.RequestFingerprint {
					t.Fatal("equivalent requests changed history identity")
				}
				encode := func(request canonical.CanonicalRequest) any {
					doc, err := EncodeCarrierWithChanges(EncodeInput{Request: request, ToolNames: testAttemptToolNames(request)}, delivery.BufferedDelivery(), nil, "test", EncodeOptions{})
					if err != nil {
						t.Fatal(err)
					}
					var value any
					if err := json.Unmarshal(doc.RawBytes(), &value); err != nil {
						t.Fatal(err)
					}
					return value
				}
				if !reflect.DeepEqual(encode(standard.Request), encode(lite.Request)) {
					t.Fatal("equivalent requests changed provider document")
				}
				if tc.name == "later_developer" {
					items := lite.Request.Items()
					message, ok := items[len(items)-1].Message()
					if !ok || message.Scope() != canonical.ContextScopeHistory || message.Role() != canonical.MessageRoleDeveloper {
						t.Fatal("later developer message lost history scope")
					}
				}
			})
		}
	}
}

func TestResponsesLiteOptionalPrefixRebase(t *testing.T) {
	for _, marker := range []string{"http", "websocket"} {
		t.Run(marker, func(t *testing.T) {
			decode := func(raw string) carrier.Document {
				body, headers := testLiteMarker(t, raw, marker)
				return carrier.NewDocument(protocolkind.Responses, "application/json", headers, []byte(body), carrier.Meta{})
			}
			first, err := (ClientRequestDecoder{}).DecodeClientRequest(decode(`{"model":"m","input":[{"type":"message","role":"developer","content":"base"},{"type":"message","role":"user","content":"one"}]}`))
			if err != nil {
				t.Fatal(err)
			}
			response := canonicaltest.Response(t, "swobu_lite_no_tools", "m", []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleAssistant, "answer")}, canonical.Completed("completed"))
			encoded, err := (ResponseDocumentEncoder{}).EncodeResponseDocument(canonical.CanonicalRequest{}, response)
			if err != nil {
				t.Fatal(err)
			}
			want, err := historyfingerprint.Advance(nil, first.Request.RequestFingerprint, *encoded.ResponseFingerprint)
			if err != nil {
				t.Fatal(err)
			}
			second, err := (ClientRequestDecoder{}).DecodeClientRequest(decode(`{"model":"m","input":[{"type":"message","role":"developer","content":"base"},{"type":"message","role":"user","content":"one"},{"type":"message","role":"assistant","content":[{"type":"output_text","text":"answer"}]},{"type":"message","role":"user","content":"two"}]}`))
			if err != nil {
				t.Fatal(err)
			}
			rebased := second.Request.RebasedRequest
			if rebased == nil || rebased.Previous != want {
				t.Fatal("tool-less Lite continuation lost predecessor identity")
			}
			items := rebased.Request.Items()
			if len(items) != 2 {
				t.Fatalf("rebased item count=%d, want directive and current message", len(items))
			}
			base, ok := items[0].Message()
			if !ok || base.Scope() != canonical.ContextScopeRequest || base.Role() != canonical.MessageRoleSystem || canonicaltest.DirectiveText(items) != "base" {
				t.Fatal("rebasing lost request-scoped base instructions")
			}
		})
	}
}

func TestResponsesLiteRejectsContradictoryAndMalformedFields(t *testing.T) {
	for _, marker := range []string{"http", "websocket"} {
		for _, tc := range []struct{ name, raw string }{
			{"instructions", `{"model":"m","instructions":"KEEP_THIS_DIRECTIVE","input":"hi"}`},
			{"null_instructions", `{"model":"m","instructions":null,"input":"hi"}`},
			{"invalid_instructions", `{"model":"m","instructions":{},"input":"hi"}`},
			{"whitespace_instructions", `{"model":"m","instructions":" ","input":"hi"}`},
			{"tools", `{"model":"m","tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}],"input":[{"type":"message","role":"user","content":"hi"}]}`},
			{"empty_top_level_tools", `{"model":"m","tools":[],"input":"hi"}`},
			{"null_top_level_tools", `{"model":"m","tools":null,"input":"hi"}`},
			{"malformed_input", `{"model":"m","input":{"role":"user"}}`},
			{"missing_input", `{"model":"m"}`},
			{"null_input", `{"model":"m","input":null}`},
			{"invalid_prefix_tools", `{"model":"m","input":[{"type":"additional_tools","role":"developer","tools":{}}]}`},
		} {
			t.Run(marker+"/"+tc.name, func(t *testing.T) {
				raw, headers := testLiteMarker(t, tc.raw, marker)
				_, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.NewDocument(protocolkind.Responses, "application/json", headers, []byte(raw), carrier.Meta{}))
				if err == nil {
					t.Fatal("invalid marked request was accepted")
				}
				var clientError canonical.Error
				if !errors.As(err, &clientError) || clientError.Code != canonical.ErrorCodeBadRequest {
					t.Fatalf("error=%v, want BAD_REQUEST", err)
				}
			})
		}
	}
}

func testLiteMarker(t *testing.T, raw, marker string) (string, http.Header) {
	t.Helper()
	if marker == "http" {
		return raw, http.Header{"X-Openai-Internal-Codex-Responses-Lite": {"true"}}
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		t.Fatal(err)
	}
	body["client_metadata"] = json.RawMessage(`{"ws_request_header_x_openai_internal_codex_responses_lite":"true"}`)
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded), nil
}

func TestResponsesLiteOptionalPrefixRequiresMarkerForRequestScope(t *testing.T) {
	result := decodeResponsesConvergenceRequest(t, `{"model":"m","input":[{"type":"message","role":"developer","content":"history"},{"type":"message","role":"user","content":"hi"}]}`, nil)
	message, ok := result.Request.Items()[0].Message()
	if !ok || message.Scope() != canonical.ContextScopeHistory || message.Role() != canonical.MessageRoleDeveloper {
		t.Fatal("unmarked developer input became request-owned")
	}
}

func TestResponsesLiteOptionalPrefixExplicitContinuation(t *testing.T) {
	for _, marker := range []string{"http", "websocket"} {
		t.Run(marker, func(t *testing.T) {
			raw, headers := testLiteMarker(t, `{"model":"m","previous_response_id":"swobu_previous","input":null}`, marker)
			result := decodeResponsesConvergenceRequest(t, raw, headers)
			_, hasPrevious := result.Request.PreviousResponse()
			if result.RebasedRequest != nil || len(result.Request.Items()) != 0 || !hasPrevious {
				t.Fatal("empty explicit continuation changed history or invented prefix items")
			}
		})
	}
}

package generatecontent

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/historyfingerprint"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	shared "github.com/swobuforge/swobu/internal/wire/shared"
)

func decodeRequest(t *testing.T, raw string, streaming bool) canonical.CanonicalRequest {
	t.Helper()
	result, err := (ClientRequestDecoder{}).DecodeClientRequest(
		carrier.Document{Family: protocolkind.GenerateContent, Raw: []byte(raw)},
		canonical.ClientOperation{Family: canonical.ClientFamilyGenerateContent, Model: canonical.Specify("gemini-3"), Streaming: canonical.Specify(streaming)},
	)
	if err != nil {
		t.Fatalf("DecodeClientRequest: %v", err)
	}
	return result.Request.Request
}

func TestDecodeClientRequestSupportsPortableInlineImage(t *testing.T) {
	raw := `{"contents":[{"parts":[{"inlineData":{"mimeType":"image/png","data":"` + base64.StdEncoding.EncodeToString([]byte("png")) + `"}}]}]}`
	decoder := ClientRequestDecoder{ImageLimits: shared.ImageDecodeLimitPolicy{MaxInlineBytes: 16, MaxImages: 1, MaxTotalImageBytes: 16}}
	result, err := decoder.DecodeClientRequest(carrier.Document{Raw: []byte(raw)}, canonical.ClientOperation{Family: canonical.ClientFamilyGenerateContent, Model: canonical.Specify("m"), Streaming: canonical.Specify(false)})
	if err != nil {
		t.Fatal(err)
	}
	message, ok := result.Request.Request.Items()[0].Message()
	if !ok {
		t.Fatal("image was not a message")
	}
	if _, ok := message.Content()[0].Image(); !ok {
		t.Fatal("image part was not preserved")
	}
}

func TestDecodeClientRequestAcceptsSemanticallyEmptyGoogleSearchObject(t *testing.T) {
	request := decodeRequest(t, `{"contents":[{"parts":[{"text":"search"}]}],"tools":[{"googleSearch":{ }}]}`, false)
	environment, err := canonical.ToolEnvironmentAt(request.Items(), len(request.Items()))
	if err != nil {
		t.Fatal(err)
	}
	declarations := environment.Declarations()
	if len(declarations) != 1 || declarations[0].Kind() != canonical.ToolKindWebSearch {
		t.Fatalf("declarations=%#v", declarations)
	}
}

func TestDecodeClientRequestRejectsGoogleSearchOptions(t *testing.T) {
	_, err := (ClientRequestDecoder{}).DecodeClientRequest(
		carrier.Document{Raw: []byte(`{"contents":[{"parts":[{"text":"search"}]}],"tools":[{"googleSearch":{"mode":"dynamic"}}]}`)},
		canonical.ClientOperation{Family: canonical.ClientFamilyGenerateContent, Model: canonical.Specify("m"), Streaming: canonical.Specify(false)},
	)
	if err == nil {
		t.Fatal("DecodeClientRequest accepted googleSearch options")
	}
}

func TestDecodeClientRequestPresenceSemantics(t *testing.T) {
	for _, raw := range []string{
		`{"contents":[{"parts":[{"text":"x"}]}],"cachedContent":null}`,
		`{"contents":[{"parts":[{"text":"x"}]}],"serviceTier":null}`,
		`{"contents":[{"parts":[{"text":"x"}]}],"store":null}`,
		`{"contents":[{"parts":[{"text":"x"}]}],"safetySettings":[{}]}`,
	} {
		if _, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.Document{Raw: []byte(raw)}, canonical.ClientOperation{Family: canonical.ClientFamilyGenerateContent, Model: canonical.Specify("m"), Streaming: canonical.Specify(false)}); err == nil {
			t.Fatalf("DecodeClientRequest accepted %s", raw)
		}
	}
	for _, raw := range []string{
		`{"contents":[{"parts":[{"text":"x"}]}],"safetySettings":[]}`,
		`{"contents":[{"parts":[{"text":"x"}]}],"safetySettings":null}`,
	} {
		if _, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.Document{Raw: []byte(raw)}, canonical.ClientOperation{Family: canonical.ClientFamilyGenerateContent, Model: canonical.Specify("m"), Streaming: canonical.Specify(false)}); err != nil {
			t.Fatalf("DecodeClientRequest rejected %s: %v", raw, err)
		}
	}
}

func TestDecodeClientRequestRequiresOneResponsePerOutstandingCallInStep(t *testing.T) {
	raw := `{"contents":[{"role":"model","parts":[{"functionCall":{"id":"a","name":"lookup","args":{}}},{"functionCall":{"id":"b","name":"lookup","args":{}}}]},{"role":"user","parts":[{"functionResponse":{"id":"a","name":"lookup","response":{"output":"A"}}}]}]}`
	if _, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.Document{Raw: []byte(raw)}, canonical.ClientOperation{Family: canonical.ClientFamilyGenerateContent, Model: canonical.Specify("m"), Streaming: canonical.Specify(false)}); err == nil {
		t.Fatal("DecodeClientRequest accepted incomplete functionResponse step")
	}
}

func TestDecodeClientRequestApproximatesArbitraryFunctionResponseObjectAndPreservesMedia(t *testing.T) {
	raw := `{"contents":[{"role":"model","parts":[{"functionCall":{"id":"a","name":"lookup","args":{}}}]},{"role":"user","parts":[{"functionResponse":{"id":"a","name":"lookup","response":{"z":2,"a":1},"parts":[{"inlineData":{"mimeType":"image/png","data":"cG5n"}}]}}]}]}`
	decoded, err := (ClientRequestDecoder{ImageLimits: shared.ImageDecodeLimitPolicy{MaxInlineBytes: 16, MaxImages: 1, MaxTotalImageBytes: 16}}).DecodeClientRequest(carrier.Document{Raw: []byte(raw)}, canonical.ClientOperation{Family: canonical.ClientFamilyGenerateContent, Model: canonical.Specify("m"), Streaming: canonical.Specify(false)})
	if err != nil {
		t.Fatal(err)
	}
	items := decoded.Request.Request.Items()
	result, ok := items[len(items)-1].ToolResult()
	if !ok || len(result.Content()) != 2 {
		t.Fatalf("result=%#v", items[len(items)-1])
	}
	text, _ := result.Content()[0].Text()
	if text.Text() != `{"a":1,"z":2}` {
		t.Fatalf("text=%q", text.Text())
	}
	if _, ok := result.Content()[1].Image(); !ok {
		t.Fatal("functionResponse image missing")
	}
	want := compat.NewApproximation(canonical.RequestItemsToolResultContent, canonical.RequestItemOccurrence(uint32(len(items)-1)))
	if len(decoded.Changes) != 1 || decoded.Changes[0] != want {
		t.Fatalf("changes=%#v want [%#v]", decoded.Changes, want)
	}
}

func TestDecodeClientRequestPreservesWholeFunctionResponseWhenScalarHasSiblings(t *testing.T) {
	for _, response := range []string{
		`{"output":"ok","metadata":{"x":1}}`,
		`{"error":"boom","output":"partial"}`,
	} {
		raw := `{"contents":[{"role":"model","parts":[{"functionCall":{"id":"a","name":"lookup","args":{}}}]},{"role":"user","parts":[{"functionResponse":{"id":"a","name":"lookup","response":` + response + `}}]}]}`
		decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.Document{Raw: []byte(raw)}, canonical.ClientOperation{Family: canonical.ClientFamilyGenerateContent, Model: canonical.Specify("m"), Streaming: canonical.Specify(false)})
		if err != nil {
			t.Fatal(err)
		}
		items := decoded.Request.Request.Items()
		result, ok := items[len(items)-1].ToolResult()
		if !ok {
			t.Fatalf("last item = %#v", items[len(items)-1])
		}
		text, _ := result.Content()[0].Text()
		object, _ := canonical.ParseJSONObject([]byte(response))
		if text.Text() != object.String() {
			t.Fatalf("text=%q want %q", text.Text(), object.String())
		}
		want := compat.NewApproximation(canonical.RequestItemsToolResultContent, canonical.RequestItemOccurrence(uint32(len(items)-1)))
		if len(decoded.Changes) != 1 || decoded.Changes[0] != want {
			t.Fatalf("changes=%#v want [%#v]", decoded.Changes, want)
		}
	}
}

func TestDecodeClientRequestPreservesReadableThoughtAndAccountsSignatureOmission(t *testing.T) {
	raw := `{"contents":[{"role":"model","parts":[{"text":"summary","thought":true,"thoughtSignature":"opaque"}]}]}`
	result, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.Document{Raw: []byte(raw)}, canonical.ClientOperation{Family: canonical.ClientFamilyGenerateContent, Model: canonical.Specify("m"), Streaming: canonical.Specify(false)})
	if err != nil {
		t.Fatal(err)
	}
	items := result.Request.Request.Items()
	if len(items) != 1 || items[0].Kind() != canonical.ItemKindReasoning {
		t.Fatalf("items = %+v", items)
	}
	if len(result.Changes) != 1 || result.Changes[0].Kind != compat.Omission {
		t.Fatalf("changes = %+v", result.Changes)
	}
}

func TestDecodeClientRequestRejectsUnsupportedMedia(t *testing.T) {
	for _, raw := range []string{
		`{"contents":[{"parts":[{"inlineData":{"mimeType":"audio/wav","data":"AA=="}}]}]}`,
		`{"contents":[{"parts":[{"fileData":{"mimeType":"image/png","fileUri":"x"}}]}]}`,
	} {
		_, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.Document{Raw: []byte(raw)}, canonical.ClientOperation{Family: canonical.ClientFamilyGenerateContent, Model: canonical.Specify("m"), Streaming: canonical.Specify(false)})
		if err == nil {
			t.Fatalf("DecodeClientRequest(%s) succeeded", raw)
		}
	}
}

func TestDecodeClientRequestPreservesOrderedToolCorrelation(t *testing.T) {
	request := decodeRequest(t, `{
      "contents":[
        {"role":"user","parts":[{"text":"look up both"}]},
        {"role":"model","parts":[
          {"text":"checking"},
          {"functionCall":{"id":"a","name":"lookup","args":{"q":"a"}}},
          {"functionCall":{"id":"b","name":"lookup","args":{"q":"b"}}}
        ]},
        {"role":"user","parts":[
          {"functionResponse":{"id":"b","name":"lookup","response":{"output":"B"}}},
          {"functionResponse":{"id":"a","name":"lookup","response":{"result":"A"}}}
        ]}
      ]
    }`, false)
	if request.Model() != "gemini-3" {
		t.Fatalf("model = %q", request.Model())
	}
	items := request.Items()
	if len(items) != 6 {
		t.Fatalf("items = %d, want 6", len(items))
	}
	for index, want := range []canonical.ItemKind{canonical.ItemKindMessage, canonical.ItemKindMessage, canonical.ItemKindToolCall, canonical.ItemKindToolCall, canonical.ItemKindToolResult, canonical.ItemKindToolResult} {
		if items[index].Kind() != want {
			t.Fatalf("item[%d] = %q, want %q", index, items[index].Kind(), want)
		}
	}
}

func TestDecodeClientRequestRejectsCorrelationMismatch(t *testing.T) {
	raw := `{"contents":[{"role":"model","parts":[{"functionCall":{"id":"a","name":"lookup","args":{}}}]},{"role":"user","parts":[{"functionResponse":{"id":"a","name":"other","response":{"output":"x"}}}]}]}`
	_, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.Document{Raw: []byte(raw)}, canonical.ClientOperation{Family: canonical.ClientFamilyGenerateContent, Model: canonical.Specify("m"), Streaming: canonical.Specify(false)})
	if err == nil {
		t.Fatal("DecodeClientRequest succeeded")
	}
}

func TestDecodeClientRequestRejectsMixedOwnerContent(t *testing.T) {
	raw := `{"contents":[{"role":"model","parts":[{"text":"x"},{"functionResponse":{"id":"a","name":"lookup","response":{}}}]}]}`
	_, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.Document{Raw: []byte(raw)}, canonical.ClientOperation{Family: canonical.ClientFamilyGenerateContent, Model: canonical.Specify("m"), Streaming: canonical.Specify(false)})
	if err == nil {
		t.Fatal("DecodeClientRequest succeeded")
	}
}

func TestDecodeClientRequestRejectsThoughtMetadataOnNonTextParts(t *testing.T) {
	for _, part := range []string{
		`{"functionCall":{"id":"a","name":"lookup","args":{}},"thought":true}`,
		`{"functionResponse":{"id":"a","name":"lookup","response":{"output":"x"}},"thought":true}`,
		`{"inlineData":{"mimeType":"image/png","data":"cG5n"},"thought":true}`,
	} {
		raw := `{"contents":[{"role":"model","parts":[` + part + `]}]}`
		if _, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.Document{Raw: []byte(raw)}, canonical.ClientOperation{Family: canonical.ClientFamilyGenerateContent, Model: canonical.Specify("m"), Streaming: canonical.Specify(false)}); err == nil {
			t.Fatalf("DecodeClientRequest accepted thought metadata on %s", part)
		}
	}
}

func TestDecodeClientRequestRejectsConsecutiveAssistantContents(t *testing.T) {
	raw := `{"contents":[{"role":"user","parts":[{"text":"question"}]},{"role":"model","parts":[{"text":"first"}]},{"role":"model","parts":[{"text":"second"}]},{"role":"user","parts":[{"text":"continue"}]}]}`
	if _, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.Document{Raw: []byte(raw)}, canonical.ClientOperation{Family: canonical.ClientFamilyGenerateContent, Model: canonical.Specify("m"), Streaming: canonical.Specify(false)}); err == nil {
		t.Fatal("DecodeClientRequest accepted consecutive assistant contents")
	}
}

func TestDecodeClientRequestRejectsUnknownAndUnsupportedFields(t *testing.T) {
	for _, raw := range []string{
		`{"contents":[{"parts":[{"text":"x"}]}],"newSemantic":true}`,
		`{"contents":[{"parts":[{"text":"x"}]}],"generationConfig":{"candidateCount":2}}`,
		`{"contents":[{"parts":[{"text":"x"}]}],"store":false}`,
	} {
		_, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.Document{Raw: []byte(raw)}, canonical.ClientOperation{Family: canonical.ClientFamilyGenerateContent, Model: canonical.Specify("m"), Streaming: canonical.Specify(false)})
		if err == nil {
			t.Fatalf("DecodeClientRequest(%s) succeeded", raw)
		}
	}
}

func TestHistoryFingerprintPreservesCallIDsAndThoughtSignatures(t *testing.T) {
	decode := func(raw string) historyfingerprint.Request {
		result, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.Document{Raw: []byte(raw)}, canonical.ClientOperation{Family: canonical.ClientFamilyGenerateContent, Model: canonical.Specify("m"), Streaming: canonical.Specify(false)})
		if err == nil {
			return result.Request.RequestFingerprint
		}
		// Opaque signatures are currently rejected from portable canonical, but
		// fingerprinting itself must still distinguish them once admission widens.
		var dto requestDTO
		if json.Unmarshal([]byte(raw), &dto) != nil {
			t.Fatal(err)
		}
		fp, fpErr := fingerprintContentsRequest(dto.Contents)
		if fpErr != nil {
			t.Fatal(fpErr)
		}
		return fp
	}
	base := `{"contents":[{"role":"model","parts":[{"functionCall":{"id":"a","name":"lookup","args":{}},"thoughtSignature":"sig-a"}]}]}`
	changedID := strings.Replace(base, `"id":"a"`, `"id":"b"`, 1)
	changedSignature := strings.Replace(base, "sig-a", "sig-b", 1)
	if decode(base) == decode(changedID) {
		t.Fatal("call ID did not affect fingerprint")
	}
	if decode(base) == decode(changedSignature) {
		t.Fatal("thought signature did not affect fingerprint")
	}
}

func TestDecodeClientRequestMapsToolChoiceAndOutputFormat(t *testing.T) {
	cases := []struct {
		config string
		mode   canonical.ToolPolicyMode
	}{
		{`{"functionCallingConfig":{"mode":"NONE"}}`, canonical.ToolPolicyNone},
		{`{"functionCallingConfig":{"mode":"AUTO"}}`, canonical.ToolPolicyAuto},
		{`{"functionCallingConfig":{"mode":"ANY"}}`, canonical.ToolPolicyRequired},
		{`{"functionCallingConfig":{"mode":"ANY","allowedFunctionNames":["lookup"]}}`, canonical.ToolPolicySpecific},
	}
	for _, tc := range cases {
		request := decodeRequest(t, `{"contents":[{"parts":[{"text":"x"}]}],"toolConfig":`+tc.config+`}`, false)
		if request.ToolPolicy().Mode != tc.mode {
			t.Fatalf("config=%s mode=%q", tc.config, request.ToolPolicy().Mode)
		}
	}
	object := decodeRequest(t, `{"contents":[{"parts":[{"text":"x"}]}],"generationConfig":{"responseMimeType":"application/json"}}`, false)
	if object.OutputFormat().Kind != canonical.OutputFormatJSONObject {
		t.Fatalf("format=%q", object.OutputFormat().Kind)
	}
	schema := decodeRequest(t, `{"contents":[{"parts":[{"text":"x"}]}],"generationConfig":{"responseMimeType":"application/json","responseJsonSchema":{"type":"object"}}}`, false)
	if schema.OutputFormat().Kind != canonical.OutputFormatJSONSchema || schema.OutputFormat().Schema.RawObject() == "" {
		t.Fatalf("format=%+v", schema.OutputFormat())
	}
}

func TestDecodeClientRequestMapsThinkingControls(t *testing.T) {
	request := decodeRequest(t, `{"contents":[{"parts":[{"text":"x"}]}],"generationConfig":{"thinkingConfig":{"thinkingLevel":"high","includeThoughts":true}}}`, false)
	effort, ok := request.Controls().Effort.Get()
	if !ok || effort != canonical.InferenceEffortHigh {
		t.Fatalf("effort=%q %v", effort, ok)
	}
	disclosure, ok := request.Reasoning().DisclosureField().Get()
	if !ok || disclosure != canonical.ReasoningDisclosureSummary {
		t.Fatalf("disclosure=%q %v", disclosure, ok)
	}
	budget := decodeRequest(t, `{"contents":[{"parts":[{"text":"x"}]}],"generationConfig":{"thinkingConfig":{"thinkingBudget":256,"includeThoughts":false}}}`, false)
	compute, ok := budget.Reasoning().ComputeField().Get()
	tokens, tokenOK := compute.Tokens()
	if !ok || !tokenOK || tokens != 256 {
		t.Fatalf("compute=%+v", compute)
	}
}

func TestDecodeClientRequestRejectsUnrepresentableRequestBands(t *testing.T) {
	configs := []string{
		`"toolConfig":{"functionCallingConfig":{"mode":"VALIDATED"}}`,
		`"toolConfig":{"functionCallingConfig":{"mode":"ANY","allowedFunctionNames":["a","b"]}}`,
		`"generationConfig":{"thinkingConfig":{"thinkingLevel":"high","thinkingBudget":12}}`,
		`"generationConfig":{"thinkingConfig":{"thinkingLevel":"extreme"}}`,
		`"generationConfig":{"responseSchema":{"type":"object"}}`,
		`"generationConfig":{"responseMimeType":"audio/wav"}`,
	}
	for _, config := range configs {
		raw := `{"contents":[{"parts":[{"text":"x"}]}],` + config + `}`
		_, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.Document{Raw: []byte(raw)}, canonical.ClientOperation{Family: canonical.ClientFamilyGenerateContent, Model: canonical.Specify("m"), Streaming: canonical.Specify(false)})
		if err == nil {
			t.Fatalf("DecodeClientRequest accepted %s", config)
		}
	}
}

func TestRestartReplayReconstructsPredecessorAndExactToolResultID(t *testing.T) {
	raw := `{
      "contents":[
        {"role":"user","parts":[{"text":"lookup"}]},
        {"role":"model","parts":[{"functionCall":{"id":"A","name":"lookup","args":{"q":"x"}}}]},
        {"role":"user","parts":[{"functionResponse":{"id":"A","name":"lookup","response":{"output":"done"}}}]}
      ]
    }`
	operation := canonical.ClientOperation{Family: canonical.ClientFamilyGenerateContent, Model: canonical.Specify("m"), Streaming: canonical.Specify(false)}
	first, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.Document{Raw: []byte(raw)}, operation)
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.Document{Raw: []byte(raw)}, operation)
	if err != nil {
		t.Fatal(err)
	}
	if first.Request.RebasedRequest == nil || restarted.Request.RebasedRequest == nil {
		t.Fatal("replay did not reconstruct predecessor")
	}
	if first.Request.RebasedRequest.Previous != restarted.Request.RebasedRequest.Previous {
		t.Fatal("predecessor depends on decoder process state")
	}
	items := restarted.Request.RebasedRequest.Request.Items()
	if len(items) != 1 || items[0].Kind() != canonical.ItemKindToolResult {
		t.Fatalf("rebased items = %+v", items)
	}
	result, _ := items[0].ToolResult()
	if result.CallID().String() != "A" {
		t.Fatalf("result ID = %q", result.CallID().String())
	}
}

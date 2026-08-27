package messages

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestEncode_PreservesGenerationControls(t *testing.T) {
	maxTokens := 88
	temperature := 0.3
	topP := 0.75
	controls, err := canonical.NewGenerationControls(canonical.GenerationControlsParams{
		MaxOutputTokens: &maxTokens,
		Temperature:     &temperature,
		TopP:            &topP,
		StopSequences:   []string{"END"},
	})
	if err != nil {
		t.Fatalf("NewGenerationControls returned error: %v", err)
	}
	req := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:    canonical.Specify("claude-3-5"),
		Items:    []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hi")},
		Controls: controls,
	})
	wire, err := EncodeCarrier(req, delivery.BufferedDelivery())
	if err != nil {
		t.Fatalf("EncodeCarrier returned error: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(wire.Raw, &body); err != nil {
		t.Fatalf("json.Unmarshal returned error: %v", err)
	}
	if got, ok := body["max_tokens"].(float64); !ok || got != 88 {
		t.Fatalf("max_tokens = %#v, want 88", body["max_tokens"])
	}
	if got, ok := body["temperature"].(float64); !ok || got != 0.3 {
		t.Fatalf("temperature = %#v, want 0.3", body["temperature"])
	}
	if got, ok := body["top_p"].(float64); !ok || got != 0.75 {
		t.Fatalf("top_p = %#v, want 0.75", body["top_p"])
	}
	stop, ok := body["stop_sequences"].([]any)
	if !ok || len(stop) != 1 {
		t.Fatalf("stop_sequences = %#v, want one sequence", body["stop_sequences"])
	}
}

func TestEncode_PreservesInstructionsAsTopLevelSystem(t *testing.T) {
	req := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("claude-3-5"),
		Items: []canonical.CanonicalItem{canonicaltest.MustInstruction(canonical.MessageRoleSystem, "Use native tools for filesystem work."), canonicaltest.Message(t, canonical.MessageRoleUser, "inspect files")},
	})
	wire, err := EncodeCarrier(req, delivery.BufferedDelivery())
	if err != nil {
		t.Fatalf("EncodeCarrier returned error: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(wire.Raw, &body); err != nil {
		t.Fatalf("json.Unmarshal returned error: %v", err)
	}
	if got := body["system"]; got != "Use native tools for filesystem work." {
		t.Fatalf("system = %#v, want canonical instructions", got)
	}
}

func TestDecodeRequest_PreservesTopLevelSystemAsInstructions(t *testing.T) {
	codec := testClientRequestDecoder{}
	req := []byte(`{"model":"claude-3-5","system":"Use native tools for filesystem work.","messages":[{"role":"user","content":"inspect files"}]}`)
	got, _, err := codec.DecodeClientRequest(carrier.Document{Family: protocolkind.Messages, Raw: req})
	if err != nil {
		t.Fatalf("DecodeClientRequest returned error: %v", err)
	}
	if canonicaltest.DirectiveText(got.Items()) != "Use native tools for filesystem work." {
		t.Fatalf("instructions = %q, want top-level system", canonicaltest.DirectiveText(got.Items()))
	}
	items := got.Items()
	message, _ := items[1].Message()
	text, textOK := message.Content()[0].Text()
	if len(items) != 2 || items[0].Kind() != canonical.ItemKindMessage || !textOK || text.Text() != "inspect files" {
		t.Fatalf("items = %#v, want scoped directive and user request", items)
	}
}

func TestDecodeRequest_DecodesGenerationControls(t *testing.T) {
	codec := testClientRequestDecoder{}
	req := []byte(`{"model":"claude-3-5","messages":[{"role":"user","content":"hi"}],"max_tokens":88,"temperature":0.3,"top_p":0.75,"stop_sequences":["END"]}`)
	got, _, err := codec.DecodeClientRequest(carrier.Document{Family: protocolkind.Messages, Raw: req})
	if err != nil {
		t.Fatalf("DecodeClientRequest returned error: %v", err)
	}
	if max, ok := got.Controls().Limits.MaxOutputTokens.Value(); !ok || max != 88 {
		t.Fatalf("max_tokens = (%d, %v), want (88, true)", max, ok)
	}
	if temp, ok := got.Controls().Sampling.Temperature.Value(); !ok || temp != 0.3 {
		t.Fatalf("temperature = (%v, %v), want (0.3, true)", temp, ok)
	}
	if topP, ok := got.Controls().Sampling.TopP.Value(); !ok || topP != 0.75 {
		t.Fatalf("top_p = (%v, %v), want (0.75, true)", topP, ok)
	}
	if gotStop := got.Controls().Limits.StopSequences; len(gotStop) != 1 || gotStop[0] != "END" {
		t.Fatalf("stop_sequences = %#v, want [END]", gotStop)
	}
}

func TestEncode_PreservesStructuredOutputFormat(t *testing.T) {
	format, err := canonical.NewOutputFormat(canonical.OutputFormatParams{
		Kind:   canonical.OutputFormatJSONSchema,
		Name:   "reply_shape",
		Schema: canonical.NewRawJSONObject(`{"type":"object","properties":{"answer":{"type":"string"}}}`),
	})
	if err != nil {
		t.Fatalf("NewOutputFormat returned error: %v", err)
	}
	wire, err := EncodeCarrier(canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:        canonical.Specify("claude-3-5"),
		Items:        []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hi")},
		OutputFormat: canonical.Specify(format),
	}), delivery.BufferedDelivery())
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(wire.Raw, &body); err != nil {
		t.Fatal(err)
	}
	outputConfig := body["output_config"].(map[string]any)
	bodyFormat := outputConfig["format"].(map[string]any)
	if bodyFormat["type"] != "json_schema" {
		t.Fatalf("output_config.format = %#v", bodyFormat)
	}
}

func TestDecodeRequest_PreservesStructuredOutputFormat(t *testing.T) {
	codec := testClientRequestDecoder{}
	req := []byte(`{"model":"claude-3-5","messages":[{"role":"user","content":"hi"}],"response_format":{"type":"json_schema","json_schema":{"name":"reply_shape","schema":{"type":"object","properties":{"answer":{"type":"string"}}}}}}`)
	got, _, err := codec.DecodeClientRequest(carrier.Document{Family: protocolkind.Messages, Raw: req})
	if err != nil {
		t.Fatal(err)
	}
	if format := got.OutputFormat(); format.Kind != canonical.OutputFormatJSONSchema || format.Name != "reply_shape" {
		t.Fatalf("output format = %#v", format)
	}
}

func TestMessagesJSONObjectIngressRequiresSchemaForProviderProjection(t *testing.T) {
	decoded, err := decodeMessagesOutputFormat(json.RawMessage(`{"type":"json_object"}`), nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Kind != canonical.OutputFormatJSONObject {
		t.Fatalf("decoded format = %#v", decoded)
	}
	var changes []compat.Change
	encoded, err := encodeMessagesOutputFormat(decoded, &changes)
	if err != nil || len(encoded) != 0 {
		t.Fatalf("json_object projection = (%s, %v), want omission", encoded, err)
	}
	want := compat.NewOmission(canonical.RequestOutputFormat, canonical.Occurrence{})
	if len(changes) != 1 || changes[0] != want {
		t.Fatalf("changes = %#v, want %#v", changes, want)
	}
}

func TestMessagesStructuredOutputStrictnessControlsHardness(t *testing.T) {
	for _, tc := range []struct {
		name       string
		strict     bool
		wantChange bool
	}{
		{name: "non-strict", wantChange: true},
		{name: "strict", strict: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			format, err := canonical.NewOutputFormat(canonical.OutputFormatParams{
				Kind: canonical.OutputFormatJSONSchema, Name: "reply", Strict: tc.strict,
				Schema: canonical.NewRawJSONObject(`{"type":"object"}`),
			})
			if err != nil {
				t.Fatal(err)
			}
			var changes []compat.Change
			_, err = encodeMessagesOutputFormat(format, &changes)
			if err != nil {
				t.Fatal(err)
			}
			if !tc.wantChange {
				if len(changes) != 0 {
					t.Fatalf("changes = %#v, want exact strict lowering", changes)
				}
				return
			}
			want := compat.NewApproximation(canonical.RequestOutputFormat, canonical.RequestOutputFormat, canonical.Occurrence{})
			if len(changes) != 1 || changes[0] != want {
				t.Fatalf("changes = %#v, want %#v", changes, want)
			}
		})
	}
}

func TestDecodeRequestPreservesNativeMessagesOutputConfigFormat(t *testing.T) {
	codec := testClientRequestDecoder{}
	req := []byte(`{"model":"claude","messages":[{"role":"user","content":"hi"}],"output_config":{"format":{"type":"json_schema","schema":{"type":"object"}}}}`)
	got, _, err := codec.DecodeClientRequest(carrier.Document{Family: protocolkind.Messages, Raw: req})
	if err != nil {
		t.Fatal(err)
	}
	format := got.OutputFormat()
	if format.Kind != canonical.OutputFormatJSONSchema || format.Schema.RawObject() != `{"type":"object"}` || !format.Strict {
		t.Fatalf("native Messages output format = %#v", format)
	}
}

func TestMessagesUnknownOutputFormatIsBadRequestWithoutDrop(t *testing.T) {
	var changes []compat.Change
	_, err := decodeMessagesOutputFormat(json.RawMessage(`{"type":"future_format"}`), &changes, "ex")
	var canonicalErr canonical.Error
	if !errors.As(err, &canonicalErr) || canonicalErr.Code != canonical.ErrorCodeBadRequest {
		t.Fatalf("error = %v, want BAD_REQUEST", err)
	}
	if len(changes) != 0 {
		t.Fatalf("changes = %#v, want no omission", changes)
	}
}

func TestMessagesUnknownNativeOutputFormatIsBadRequestWithoutDrop(t *testing.T) {
	var changes []compat.Change
	_, err := decodeMessagesNativeOutputFormat(&messagesNativeOutputFormatDTO{
		Type: "future_format",
	}, &changes, "ex")
	var canonicalErr canonical.Error
	if !errors.As(err, &canonicalErr) || canonicalErr.Code != canonical.ErrorCodeBadRequest {
		t.Fatalf("error = %v, want BAD_REQUEST", err)
	}
	if len(changes) != 0 {
		t.Fatalf("changes = %#v, want no omission", changes)
	}
}

func TestMessagesMissingOutputFormatDiscriminatorIsBadRequest(t *testing.T) {
	tests := []struct {
		name string
		run  func() error
	}{
		{
			name: "response format",
			run: func() error {
				_, err := decodeMessagesOutputFormat(json.RawMessage(`{}`), nil, "")
				return err
			},
		},
		{
			name: "native output config",
			run: func() error {
				_, err := decodeMessagesNativeOutputFormat(&messagesNativeOutputFormatDTO{}, nil, "")
				return err
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var canonicalErr canonical.Error
			if err := test.run(); !errors.As(err, &canonicalErr) || canonicalErr.Code != canonical.ErrorCodeBadRequest {
				t.Fatalf("error = %T %v, want BAD_REQUEST", err, err)
			}
		})
	}
}

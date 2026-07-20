package canonical

import "testing"

func TestTranscriptPrefixFingerprintsNormalizeCallIDsAndJSONOrder(t *testing.T) {
	makeRequest := func(callRaw, args string) CanonicalRequest {
		callID, _ := NewToolCallID(callRaw)
		object, _ := ParseJSONObject([]byte(args))
		toolID := testRequestToolKey(ToolKindFunction, "lookup")
		tool := toolID
		call, _ := NewToolCallItem(callID, tool, NewJSONObjectToolInput(object))
		result, _ := NewToolResultItem(callID, []ToolResultPart{NewTextToolResultPart("ok")}, false)
		decl := testFunctionTool(testRequestToolKey(ToolKindFunction, "lookup"), "", testToolSchema(`{"type":"object"}`), Unspecified[bool]())
		tools, _ := NewToolSet([]ToolDeclaration{decl})
		return NewCanonicalRequest(RequestParams{Instructions: Specify(NewSystemInstructionSet("system")), Tools: Specify(tools), Items: []CanonicalItem{call, result}})
	}
	a, err := TranscriptPrefixFingerprints(makeRequest("call_a", `{"b":2,"a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	b, err := TranscriptPrefixFingerprints(makeRequest("different", `{"a":1,"b":2}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 3 || a[2].Sum != b[2].Sum {
		t.Fatal("equivalent correlation topology did not fingerprint equally")
	}
}

func TestTranscriptPrefixFingerprintsPreserveJSONNumberSpelling(t *testing.T) {
	fingerprint := func(args string) [32]byte {
		callID, _ := NewToolCallID("call")
		object, _ := ParseJSONObject([]byte(args))
		tool := testRequestToolKey(ToolKindFunction, "lookup")
		call, _ := NewToolCallItem(callID, tool, NewJSONObjectToolInput(object))
		chain, err := TranscriptPrefixFingerprints(NewCanonicalRequest(RequestParams{Items: []CanonicalItem{call}}))
		if err != nil {
			t.Fatal(err)
		}
		return chain[1].Sum
	}
	if fingerprint(`{"x":1}`) == fingerprint(`{"x":1.0}`) || fingerprint(`{"x":1}`) == fingerprint(`{"x":1e0}`) {
		t.Fatal("lexically distinct JSON numbers fingerprinted equally")
	}
}

func TestTranscriptPrefixFingerprintsRejectBadCorrelationAndMarkURLsUnstable(t *testing.T) {
	callID, _ := NewToolCallID("missing")
	result, _ := NewToolResultItem(callID, []ToolResultPart{NewTextToolResultPart("orphan")}, false)
	if _, err := TranscriptPrefixFingerprints(NewCanonicalRequest(RequestParams{Items: []CanonicalItem{result}})); err == nil {
		t.Fatal("unmatched tool result was accepted")
	}
	image, _ := NewURLImage("https://example.test/image.png", Unspecified[ImageDetail]())
	message, _ := NewMessageItem(MessageRoleUser, []MessagePart{NewImageMessagePart(image)})
	chain, err := TranscriptPrefixFingerprints(NewCanonicalRequest(RequestParams{Items: []CanonicalItem{message}}))
	if err != nil {
		t.Fatal(err)
	}
	if chain[0].Stable != true || chain[1].Stable != false {
		t.Fatal("URL image did not make cumulative prefix unstable")
	}
}

func TestTranscriptPrefixFingerprintsIncludeImageDetail(t *testing.T) {
	fingerprint := func(detail ImageDetail) [32]byte {
		image, err := NewInlineImage(ImageMediaPNG, pngSignature(), Specify(detail))
		if err != nil {
			t.Fatal(err)
		}
		message, err := NewMessageItem(MessageRoleUser, []MessagePart{NewImageMessagePart(image)})
		if err != nil {
			t.Fatal(err)
		}
		chain, err := TranscriptPrefixFingerprints(NewCanonicalRequest(RequestParams{Items: []CanonicalItem{message}}))
		if err != nil {
			t.Fatal(err)
		}
		return chain[len(chain)-1].Sum
	}
	if fingerprint(ImageDetailLow) == fingerprint(ImageDetailHigh) {
		t.Fatal("different image details produced the same transcript fingerprint")
	}
}

func TestTranscriptAndInvocationFingerprintsSeparateCurrentBands(t *testing.T) {
	message, _ := NewMessageItem(MessageRoleUser, []MessagePart{NewTextMessagePart("same")})
	schema := testToolSchema(`{"type":"object"}`)
	first := testFunctionTool(testRequestToolKey(ToolKindFunction, "first"), "", schema, Unspecified[bool]())
	second := testFunctionTool(testRequestToolKey(ToolKindFunction, "second"), "", schema, Unspecified[bool]())
	toolsAB, _ := NewToolSet([]ToolDeclaration{first, second})
	toolsBA, _ := NewToolSet([]ToolDeclaration{second, first})
	a := NewCanonicalRequest(RequestParams{Instructions: Specify(NewSystemInstructionSet("a")), Tools: Specify(toolsAB), Items: []CanonicalItem{message}})
	b := NewCanonicalRequest(RequestParams{Instructions: Specify(NewSystemInstructionSet("b")), Tools: Specify(toolsBA), Items: []CanonicalItem{message}})
	pa, _ := TranscriptPrefixFingerprints(a)
	pb, _ := TranscriptPrefixFingerprints(b)
	if pa[1].Sum != pb[1].Sum {
		t.Fatal("current instructions affected transcript prefix")
	}
	ia, _ := FingerprintInvocation(a)
	ib, _ := FingerprintInvocation(b)
	if ia.Sum == ib.Sum {
		t.Fatal("current instructions and ordered tools did not affect invocation fingerprint")
	}
}

func TestTranscriptPrefixFingerprintsPermitCallIDReuseAfterResult(t *testing.T) {
	callID, _ := NewToolCallID("call_1")
	tool := testRequestToolKey(ToolKindFunction, "lookup")
	object, _ := ParseJSONObject([]byte(`{}`))
	call, _ := NewToolCallItem(callID, tool, NewJSONObjectToolInput(object))
	result, _ := NewToolResultItem(callID, []ToolResultPart{NewTextToolResultPart("ok")}, false)
	request := NewCanonicalRequest(RequestParams{Items: []CanonicalItem{call, result, call, result}})
	if _, err := TranscriptPrefixFingerprints(request); err != nil {
		t.Fatalf("completed call ID reuse rejected: %v", err)
	}
	if _, err := TranscriptPrefixFingerprints(NewCanonicalRequest(RequestParams{Items: []CanonicalItem{call, call}})); err == nil {
		t.Fatal("overlapping call ID reuse was accepted")
	}
}

func TestInvocationFingerprintRejectsUnmaterializedPreviousResponse(t *testing.T) {
	ref := ResponseRef{SwobuID: NewSwobuResponseID("resp_1")}
	_, err := FingerprintInvocation(NewCanonicalRequest(RequestParams{PreviousResponse: &ref}))
	if err == nil {
		t.Fatal("invocation fingerprint ignored context-bearing previous response")
	}
}

func TestInvocationFingerprintDistinguishesToolOrderAndToolOmission(t *testing.T) {
	schema := testToolSchema(`{"type":"object"}`)
	first := testFunctionTool(testRequestToolKey(ToolKindFunction, "first"), "", schema, Unspecified[bool]())
	second := testFunctionTool(testRequestToolKey(ToolKindFunction, "second"), "", schema, Unspecified[bool]())
	ab, _ := NewToolSet([]ToolDeclaration{first, second})
	ba, _ := NewToolSet([]ToolDeclaration{second, first})
	empty, _ := NewToolSet(nil)

	fingerprint := func(tools Specified[ToolSet]) InvocationFingerprint {
		t.Helper()
		got, err := FingerprintInvocation(NewCanonicalRequest(RequestParams{Tools: tools}))
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	if fingerprint(Specify(ab)).Sum == fingerprint(Specify(ba)).Sum {
		t.Fatal("tool declaration order did not affect invocation fingerprint")
	}
	if fingerprint(Unspecified[ToolSet]()).Sum == fingerprint(Specify(empty)).Sum {
		t.Fatal("omitted and explicitly empty tools fingerprinted equally")
	}
}

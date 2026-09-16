package canonical

import "testing"

func TestParseClientOperationGenerateContent(t *testing.T) {
	tests := []struct {
		path      string
		streaming bool
	}{
		{"/v1beta/models/gemini-3:generateContent", false},
		{"/v1/models/gemini-3:streamGenerateContent?alt=sse&key=dummy", true},
	}
	for _, test := range tests {
		op, err := ParseClientOperation("POST", test.path, false, false)
		if err != nil {
			t.Fatalf("ParseClientOperation(%q): %v", test.path, err)
		}
		if op.Family != ClientFamilyGenerateContent {
			t.Fatalf("family = %q", op.Family)
		}
		model, ok := op.Model.Get()
		if !ok || model != "gemini-3" {
			t.Fatalf("model = %q, %v", model, ok)
		}
		streaming, ok := op.Streaming.Get()
		if !ok || streaming != test.streaming {
			t.Fatalf("streaming = %v, %v", streaming, ok)
		}
	}
}

func TestParseClientOperationUsesDecodedGenerateContentModel(t *testing.T) {
	op, err := ParseClientOperation("POST", "/v1beta/models/gemini%2Dfoo:generateContent", false, false)
	if err != nil {
		t.Fatal(err)
	}
	model, ok := op.Model.Get()
	if !ok || model != "gemini-foo" {
		t.Fatalf("model=%q specified=%v", model, ok)
	}
}

func TestParseClientOperationRetainsRecognizedGenerateContentFamilyOnError(t *testing.T) {
	for _, test := range []struct {
		method string
		path   string
	}{
		{"GET", "/v1beta/models/x:generateContent"},
		{"POST", "/v1beta/models/x:wrongAction"},
		{"POST", "/v1beta/models/:generateContent"},
	} {
		op, err := ParseClientOperation(test.method, test.path, false, false)
		if err == nil || op.Family != ClientFamilyGenerateContent {
			t.Fatalf("ParseClientOperation(%q, %q) = %#v, %v", test.method, test.path, op, err)
		}
	}
}

func TestParseClientOperationRejectsInvalidGenerateContentPaths(t *testing.T) {
	for _, path := range []string{
		"/v1beta/models/:generateContent",
		"/v1beta/models/a/b:generateContent",
		"/v1beta/models/a%2Fb:generateContent",
		"/v1beta/models/../a:generateContent",
		"/v1beta/models/%2e%2e:generateContent",
		"/v1beta/models/a:unknown",
		"/v1beta/models/a:streamGenerateContent?alt=json",
		"/v1beta/models/a:streamGenerateContent?alt=sse&alt=json",
		"/v1beta/models/a:generateContent?future=value",
	} {
		if _, err := ParseClientOperation("POST", path, false, false); err == nil {
			t.Fatalf("ParseClientOperation(%q) succeeded", path)
		}
	}
}

package canonical

import "testing"

func TestParseJSONObjectDeterministicOrderingAndNumberLexemes(t *testing.T) {
	left, err := ParseJSONObject([]byte(`{"z":1.2300,"a":{"y":2,"x":1},"items":[{"b":2,"a":1},3]}`))
	if err != nil {
		t.Fatalf("ParseJSONObject(left): %v", err)
	}
	right, err := ParseJSONObject([]byte(` { "items" : [ { "a":1, "b":2 }, 3 ], "a":{"x":1,"y":2}, "z":1.2300 } `))
	if err != nil {
		t.Fatalf("ParseJSONObject(right): %v", err)
	}
	const want = `{"a":{"x":1,"y":2},"items":[{"a":1,"b":2},3],"z":1.2300}`
	if left.String() != want || right.String() != want {
		t.Fatalf("canonical objects = %q and %q, want %q", left.String(), right.String(), want)
	}
}

func TestParseJSONObjectRejectsInvalidObjectShapes(t *testing.T) {
	tests := []string{
		`[]`,
		`{"a":1} {"b":2}`,
		`{"a":1,"a":2}`,
		`{"nested":{"a":1,"a":2}}`,
		`{"items":[{"a":1,"a":2}]}`,
	}
	for _, raw := range tests {
		t.Run(raw, func(t *testing.T) {
			if _, err := ParseJSONObject([]byte(raw)); err == nil {
				t.Fatalf("ParseJSONObject(%s) unexpectedly succeeded", raw)
			}
		})
	}
}

func TestJSONObjectCloneDoesNotAliasBytes(t *testing.T) {
	original, err := ParseJSONObject([]byte(`{"b":2,"a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	cloned := original.Clone()
	bytes := cloned.Bytes()
	bytes[0] = '['
	if original.String() != `{"a":1,"b":2}` || cloned.String() != original.String() {
		t.Fatalf("mutation escaped copy: original=%q clone=%q", original.String(), cloned.String())
	}
}

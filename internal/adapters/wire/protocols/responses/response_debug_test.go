package responses

import "testing"

func TestCompactAndTruncateJSONCompacts(t *testing.T) {
	got, truncated := compactAndTruncateJSON([]byte("{\n  \"a\": 1,\n  \"b\": \"x\"\n}\n"), 1024)
	if truncated {
		t.Fatalf("truncated = true, want false")
	}
	want := "{\"a\":1,\"b\":\"x\"}"
	if got != want {
		t.Fatalf("compactAndTruncateJSON = %q, want %q", got, want)
	}
}

func TestCompactAndTruncateJSONTruncates(t *testing.T) {
	got, truncated := compactAndTruncateJSON([]byte("{\"a\":\"1234567890\"}"), 8)
	if !truncated {
		t.Fatalf("truncated = false, want true")
	}
	if len(got) != 8 {
		t.Fatalf("len(got) = %d, want 8", len(got))
	}
}

func TestCompactAndTruncateJSONEmptyAsNull(t *testing.T) {
	got, truncated := compactAndTruncateJSON([]byte(" \n\t "), 16)
	if truncated {
		t.Fatalf("truncated = true, want false")
	}
	if got != "null" {
		t.Fatalf("compactAndTruncateJSON = %q, want null", got)
	}
}

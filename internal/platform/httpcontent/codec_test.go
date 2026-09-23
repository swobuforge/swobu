package httpcontent

import (
	"bytes"
	"testing"
)

func TestIdentityDecodeReturnsOwnedInputWithoutCopy(t *testing.T) {
	raw := []byte("request body")
	decoded, err := DecodeBytesLimited("identity", raw, int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded) == 0 || &decoded[0] != &raw[0] {
		t.Fatal("identity decode copied the body")
	}
	decoded[0] = 'R'
	if raw[0] != 'R' {
		t.Fatal("identity decode did not return the input allocation")
	}
}

func TestIdentityDecodePreservesDecodedSizeLimit(t *testing.T) {
	if _, err := DecodeBytesLimited("", bytes.Repeat([]byte("x"), 5), 4); err == nil || err.Error() != "decoded body exceeds limit" {
		t.Fatalf("limit error=%v", err)
	}
}

package carrier

import (
	"net/http"
	"testing"
)

func TestDocumentCloneAndRawBytes_DefensiveCopies(t *testing.T) {
	in := NewDocument("responses", "application/json", nil, []byte(`{"a":1}`), Meta{})

	cloned := in.Clone()
	cloned.Raw[0] = 'X'
	if in.Raw[0] == 'X' {
		t.Fatalf("clone mutated source raw")
	}

	raw := in.RawBytes()
	raw[0] = 'Y'
	if in.Raw[0] == 'Y' {
		t.Fatalf("raw bytes leaked source storage")
	}
}

func TestNewDocument_ClonesHeaderAndMeta(t *testing.T) {
	header := http.Header{}
	header.Set("X-Id", "a")
	meta := Meta{Opaque: map[string]string{"k": "v"}}
	doc := NewDocument("responses", "application/json", header, []byte(`{}`), meta)

	header.Set("X-Id", "b")
	meta.Opaque["k"] = "x"
	if doc.Header.Get("X-Id") != "a" {
		t.Fatalf("header leaked mutation")
	}
	if doc.Meta.Opaque["k"] != "v" {
		t.Fatalf("meta leaked mutation")
	}
}

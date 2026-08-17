package cacheintent

import "testing"

func TestExplicitPreservesClientKey(t *testing.T) {
	if got := Explicit(" Repo Key ").Key(); got != " Repo Key " {
		t.Fatalf("key = %q", got)
	}
}

func TestDerivedIsStableSeparatedAndOpaque(t *testing.T) {
	first := Derived("alpha", "resp_1")
	if first.IsZero() || first != Derived("alpha", "resp_1") {
		t.Fatal("derived affinity is not stable and non-zero")
	}
	if first == Derived("beta", "resp_1") || first == Derived("alpha", "resp_2") {
		t.Fatal("workspace and lineage must both separate derived affinity")
	}
	if len(first.Key()) != len("swobu_")+sha256HexLength {
		t.Fatalf("derived key length = %d", len(first.Key()))
	}
	if first.Key() == "resp_1" {
		t.Fatal("lineage identity leaked")
	}
}

const sha256HexLength = 64

package cachelocality

import "testing"

func TestExplicitPreservesClientKey(t *testing.T) {
	if got := Explicit(" Repo Key ").Key(); got != " Repo Key " {
		t.Fatalf("key = %q", got)
	}
}

func TestDerivedIsStableSeparatedAndOpaque(t *testing.T) {
	first := Derived("alpha", "resp_1")
	if first.IsZero() || first != Derived("alpha", "resp_1") {
		t.Fatal("derived locality is not stable and non-zero")
	}
	if first == Derived("beta", "resp_1") || first == Derived("alpha", "resp_2") {
		t.Fatal("workspace and lineage must both separate derived locality")
	}
	if len(first.Key()) != len("swobu_")+sha256HexLength {
		t.Fatalf("derived key length = %d", len(first.Key()))
	}
	if first.Key() == "resp_1" {
		t.Fatal("lineage identity leaked")
	}
}

func TestDerivedPreservesCacheAffinityV1CompatibilityKey(t *testing.T) {
	const want = "swobu_bbb8a13e233ec1671ef7b595ae15791fde6e4c8f71eb5ffe110ea608619c1d7d"
	if got := Derived("alpha", "resp_1").Key(); got != want {
		t.Fatalf("derived compatibility key = %q, want %q", got, want)
	}
}

const sha256HexLength = 64

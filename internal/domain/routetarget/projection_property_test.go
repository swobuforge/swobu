package routetarget

import (
	"math/rand"
	"testing"
)

func TestResolveSelectedTarget_IsDeterministicForIdenticalConfig(t *testing.T) {
	rng := rand.New(rand.NewSource(1))

	specs := []providerConfigSpec{
		{ref: "third"},
		{ref: "first"},
		{ref: "second"},
	}

	const iterations = 25
	var want string
	for i := 0; i < iterations; i++ {
		shuffled := append([]providerConfigSpec(nil), specs...)
		rng.Shuffle(len(shuffled), func(i, j int) {
			shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
		})

		endpoint := testEndpoint(t, shuffled, "first")
		resolved, err := ResolveRoutableTarget(endpoint)
		if err != nil {
			t.Fatalf("ResolveRoutableTarget returned error: %v", err)
		}

		got := resolved.ProviderConfig.Ref().String()
		if i == 0 {
			want = got
			continue
		}
		if got != want {
			t.Fatalf("iteration %d selected %q, want deterministic result %q", i, got, want)
		}
	}
}

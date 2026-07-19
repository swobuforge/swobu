package codecresolver_test

import (
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/exchange/codecresolver"
)

func TestCodecResolver_ComposesAllClientFamilies(t *testing.T) {
	resolver := codecresolver.NewRuntimeCodecResolver()
	for _, family := range []canonical.ClientFamily{
		canonical.ClientFamilyChatCompletions,
		canonical.ClientFamilyResponses,
		canonical.ClientFamilyMessages,
	} {
		if resolver.ClientCodec(family) == nil {
			t.Fatalf("client codec missing for family %s", family)
		}
	}
}

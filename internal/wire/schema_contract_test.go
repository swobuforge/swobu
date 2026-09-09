package wire

import (
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"testing"
)

func TestSchemaContractExactMatrix(t *testing.T) {
	profiles := []canonical.SchemaProfile{canonical.SchemaProfileAnthropic, canonical.SchemaProfileOpenAI}
	for _, source := range profiles {
		for _, target := range profiles {
			want := source == target
			if got := SchemaContractExact(canonical.SchemaContract{Profile: source}, target); got != want {
				t.Fatalf("source=%v target=%v exact=%t want=%t", source, target, got, want)
			}
		}
	}
	for _, target := range profiles {
		if SchemaContractExact(canonical.SchemaContract{Profile: canonical.SchemaProfileUnprofiled}, target) || SchemaContractExact(canonical.SchemaContract{}, target) {
			t.Fatalf("unprofiled/unspecified exact for target=%v", target)
		}
	}
}
